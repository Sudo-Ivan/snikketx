package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type settings struct {
	IntervalHours int  `json:"interval_hours"`
	AutoUpdate    bool `json:"auto_update"`
}

type serviceState struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	Digest          string `json:"digest,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	SignatureOK     *bool  `json:"signature_ok,omitempty"`
	SignatureDetail string `json:"signature_detail,omitempty"`
	Registry        string `json:"registry,omitempty"`
}

type statusResponse struct {
	Status        string         `json:"status"`
	Available     bool           `json:"available"`
	LastCheck     string         `json:"last_check,omitempty"`
	LastApply     string         `json:"last_apply,omitempty"`
	IntervalHours int            `json:"interval_hours"`
	AutoUpdate    bool           `json:"auto_update"`
	Services      []serviceState `json:"services"`
	Detail        string         `json:"detail,omitempty"`
	Configured    bool           `json:"configured"`
	ImagePrefix   string         `json:"image_prefix,omitempty"`
	VerifyEnabled bool           `json:"verify_enabled"`
}

type server struct {
	token            string
	composeDir       string
	statePath        string
	imagePrefix      string
	verifyEnabled    bool
	requireVerify    bool
	cosignIdentity   string
	cosignOIDCIssuer string
	cosignBin        string

	mu        sync.Mutex
	settings  settings
	lastCheck time.Time
	lastApply time.Time
	services  []serviceState
	available bool
	detail    string
	busy      bool
}

func main() {
	addr := envOr("SNIKKET_UPDATER_LISTEN", "0.0.0.0:9191")
	token := strings.TrimSpace(os.Getenv("SNIKKET_UPDATER_TOKEN"))
	if token == "" {
		token = "snikket-updater-local"
		log.Printf("SNIKKET_UPDATER_TOKEN unset, using built-in local default")
	}
	composeDir := envOr("SNIKKET_UPDATER_COMPOSE_DIR", "/work")
	stateDir := envOr("SNIKKET_UPDATER_STATE_DIR", "/var/lib/snikket-updater")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		log.Fatal(err)
	}

	verifyEnabled := envBool("SNIKKET_UPDATER_VERIFY_SIGNATURES", true)
	requireVerify := envBool("SNIKKET_UPDATER_REQUIRE_SIGNATURES", false)

	s := &server{
		token:            token,
		composeDir:       composeDir,
		statePath:        filepath.Join(stateDir, "settings.json"),
		imagePrefix:      strings.TrimRight(envOr("SNIKKET_UPDATER_IMAGE_PREFIX", "ghcr.io/sudo-ivan/snikketx"), "/"),
		verifyEnabled:    verifyEnabled,
		requireVerify:    requireVerify,
		cosignIdentity:   envOr("SNIKKET_UPDATER_COSIGN_IDENTITY_REGEXP", "https://github.com/Sudo-Ivan/snikketx/.*"),
		cosignOIDCIssuer: envOr("SNIKKET_UPDATER_COSIGN_OIDC_ISSUER", "https://token.actions.githubusercontent.com"),
		cosignBin:        envOr("SNIKKET_UPDATER_COSIGN_BIN", "cosign"),
		settings: settings{
			IntervalHours: 24,
			AutoUpdate:    false,
		},
	}
	s.loadSettings()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.auth(s.handleStatus))
	mux.HandleFunc("POST /v1/check", s.auth(s.handleCheck))
	mux.HandleFunc("POST /v1/apply", s.auth(s.handleApply))
	mux.HandleFunc("GET /v1/settings", s.auth(s.handleGetSettings))
	mux.HandleFunc("PUT /v1/settings", s.auth(s.handlePutSettings))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	go s.loop()
	log.Printf("snikket updater listening on %s prefix=%s verify=%v", addr, s.imagePrefix, s.verifyEnabled)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.snapshot())
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, s.settings)
}

func (s *server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var next settings
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if next.IntervalHours < 1 {
		next.IntervalHours = 1
	}
	if next.IntervalHours > 168 {
		next.IntervalHours = 168
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	if err := s.saveSettings(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, next)
}

func (s *server) handleCheck(w http.ResponseWriter, r *http.Request) {
	if err := s.runCheck(r.Context(), false); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, s.snapshot())
}

func (s *server) handleApply(w http.ResponseWriter, r *http.Request) {
	if err := s.runApply(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, s.snapshot())
}

func (s *server) loop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		interval := time.Duration(s.settings.IntervalHours) * time.Hour
		auto := s.settings.AutoUpdate
		due := s.lastCheck.IsZero() || time.Since(s.lastCheck) >= interval
		s.mu.Unlock()
		if !due {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		_ = s.runCheck(ctx, auto)
		cancel()
	}
}

func (s *server) acquire() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return errors.New("updater is busy")
	}
	s.busy = true
	return nil
}

func (s *server) release() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func (s *server) runCheck(ctx context.Context, autoApply bool) error {
	if err := s.acquire(); err != nil {
		return err
	}
	defer s.release()

	before, err := s.imageInventory(ctx)
	if err != nil {
		s.setDetail("image inventory failed: " + err.Error())
		return err
	}
	if out, err := s.compose(ctx, "pull"); err != nil {
		s.setDetail(strings.TrimSpace(out) + " " + err.Error())
		return err
	}
	after, err := s.imageInventory(ctx)
	if err != nil {
		s.setDetail("image inventory failed: " + err.Error())
		return err
	}

	services := make([]serviceState, 0, len(after))
	available := false
	verifyFailed := 0
	names := make([]string, 0, len(after))
	for name := range after {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		img := after[name]
		prev := before[name]
		changed := prev.ID != "" && prev.ID != img.ID
		if changed {
			available = true
		}
		row := serviceState{
			Name:            name,
			Image:           img.Ref,
			Digest:          img.Digest,
			UpdateAvailable: changed,
			Registry:        img.Registry,
		}
		if s.verifyEnabled && strings.HasPrefix(img.Ref, s.imagePrefix+"/") {
			ok, detail := s.verifyImage(ctx, img)
			row.SignatureOK = &ok
			row.SignatureDetail = detail
			if !ok {
				verifyFailed++
			}
		}
		services = append(services, row)
	}

	detail := "all tracked images are current"
	if available {
		detail = "newer container images are ready"
	}
	if verifyFailed > 0 {
		detail = fmt.Sprintf("%s (%d signature check(s) failed)", detail, verifyFailed)
		if s.requireVerify {
			s.mu.Lock()
			s.services = services
			s.available = false
			s.lastCheck = time.Now().UTC()
			s.detail = detail
			s.mu.Unlock()
			return fmt.Errorf("signature verification failed for %d image(s)", verifyFailed)
		}
	}

	s.mu.Lock()
	s.services = services
	s.available = available
	s.lastCheck = time.Now().UTC()
	s.detail = detail
	s.mu.Unlock()

	if autoApply && available {
		s.release()
		err := s.runApply(ctx)
		_ = s.acquire()
		return err
	}
	return nil
}

func (s *server) runApply(ctx context.Context) error {
	if err := s.acquire(); err != nil {
		return err
	}
	defer s.release()

	if s.requireVerify {
		for _, svc := range s.snapshot().Services {
			if svc.SignatureOK != nil && !*svc.SignatureOK {
				return fmt.Errorf("refusing to apply unsigned or unverified image %s", svc.Name)
			}
		}
	}

	if out, err := s.compose(ctx, "up", "-d"); err != nil {
		s.setDetail(strings.TrimSpace(out) + " " + err.Error())
		return err
	}
	s.mu.Lock()
	s.lastApply = time.Now().UTC()
	s.available = false
	for i := range s.services {
		s.services[i].UpdateAvailable = false
	}
	s.detail = "containers updated"
	s.mu.Unlock()
	return nil
}

func (s *server) compose(ctx context.Context, args ...string) (string, error) {
	cmdArgs := append([]string{"compose"}, args...)
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	cmd.Dir = s.composeDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type imageInfo struct {
	ID       string
	Ref      string
	Digest   string
	Registry string
}

func (s *server) imageInventory(ctx context.Context) (map[string]imageInfo, error) {
	out, err := s.compose(ctx, "images", "--format", "json")
	if err != nil {
		return map[string]imageInfo{}, nil
	}
	result := map[string]imageInfo{}
	dec := json.NewDecoder(strings.NewReader(out))
	for {
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			break
		}
		name, _ := row["Service"].(string)
		if name == "" {
			name, _ = row["ContainerName"].(string)
		}
		id, _ := row["ID"].(string)
		repo, _ := row["Repository"].(string)
		tag, _ := row["Tag"].(string)
		ref := strings.TrimSpace(repo)
		if ref != "" && tag != "" && tag != "<none>" {
			ref = ref + ":" + tag
		}
		if ref == "" {
			ref = id
		}
		digest := ""
		if id != "" {
			digest = s.imageDigest(ctx, id)
			if digest != "" && !strings.Contains(ref, "@") {
				base := repo
				if base == "" {
					base = strings.Split(ref, ":")[0]
				}
				ref = base + "@" + digest
			}
		}
		registry := ""
		if i := strings.IndexByte(ref, '/'); i > 0 {
			registry = ref[:i]
		}
		if name != "" && (id != "" || ref != "") {
			result[name] = imageInfo{ID: id, Ref: ref, Digest: digest, Registry: registry}
		}
	}
	return result, nil
}

func (s *server) imageDigest(ctx context.Context, id string) string {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{index .RepoDigests 0}}", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if i := strings.LastIndex(line, "@"); i >= 0 {
		return line[i+1:]
	}
	return ""
}

func (s *server) verifyImage(ctx context.Context, img imageInfo) (bool, string) {
	target := img.Ref
	if img.Digest != "" && !strings.Contains(target, "@") {
		base := strings.Split(target, ":")[0]
		target = base + "@" + img.Digest
	}
	if target == "" {
		return false, "missing image reference"
	}
	args := []string{
		"verify",
		"--certificate-identity-regexp", s.cosignIdentity,
		"--certificate-oidc-issuer", s.cosignOIDCIssuer,
		target,
	}
	cmd := exec.CommandContext(ctx, s.cosignBin, args...)
	out, err := cmd.CombinedOutput()
	detail := strings.TrimSpace(string(out))
	if err != nil {
		if detail == "" {
			detail = err.Error()
		}
		return false, detail
	}
	if detail == "" {
		detail = "cosign keyless signature verified"
	}
	attOK, attDetail := s.verifyAttestation(ctx, target)
	if !attOK {
		return true, detail + " (sbom attestation: " + attDetail + ")"
	}
	return true, detail + "; sbom attestation present"
}

func (s *server) verifyAttestation(ctx context.Context, target string) (bool, string) {
	args := []string{
		"verify-attestation",
		"--type", "spdxjson",
		"--certificate-identity-regexp", s.cosignIdentity,
		"--certificate-oidc-issuer", s.cosignOIDCIssuer,
		target,
	}
	cmd := exec.CommandContext(ctx, s.cosignBin, args...)
	out, err := cmd.CombinedOutput()
	detail := strings.TrimSpace(string(out))
	if err != nil {
		if detail == "" {
			detail = err.Error()
		}
		return false, detail
	}
	return true, "ok"
}

func (s *server) snapshot() statusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := "idle"
	if s.busy {
		status = "busy"
	}
	resp := statusResponse{
		Status:        status,
		Available:     s.available,
		IntervalHours: s.settings.IntervalHours,
		AutoUpdate:    s.settings.AutoUpdate,
		Services:      append([]serviceState(nil), s.services...),
		Detail:        s.detail,
		Configured:    true,
		ImagePrefix:   s.imagePrefix,
		VerifyEnabled: s.verifyEnabled,
	}
	if !s.lastCheck.IsZero() {
		resp.LastCheck = s.lastCheck.Format(time.RFC3339)
	}
	if !s.lastApply.IsZero() {
		resp.LastApply = s.lastApply.Format(time.RFC3339)
	}
	return resp
}

func (s *server) setDetail(detail string) {
	s.mu.Lock()
	s.detail = strings.TrimSpace(detail)
	s.mu.Unlock()
}

func (s *server) loadSettings() {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		return
	}
	var next settings
	if json.Unmarshal(data, &next) != nil {
		return
	}
	if next.IntervalHours > 0 {
		s.settings = next
	}
}

func (s *server) saveSettings() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.statePath, data, 0o600)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
