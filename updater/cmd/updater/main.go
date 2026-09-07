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
	PinDigests    bool `json:"pin_digests"`
}

type serviceState struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	Digest          string `json:"digest,omitempty"`
	PinnedDigest    string `json:"pinned_digest,omitempty"`
	PinnedImage     string `json:"pinned_image,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	SignatureOK     *bool  `json:"signature_ok,omitempty"`
	SignatureDetail string `json:"signature_detail,omitempty"`
	Registry        string `json:"registry,omitempty"`
}

type jobStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	At     string `json:"at,omitempty"`
}

type jobState struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Status  string    `json:"status"`
	Phase   string    `json:"phase"`
	Steps   []jobStep `json:"steps"`
	Log     []string  `json:"log"`
	Error   string    `json:"error,omitempty"`
	Started string    `json:"started,omitempty"`
	Ended   string    `json:"ended,omitempty"`
}

type statusResponse struct {
	Status        string         `json:"status"`
	Phase         string         `json:"phase,omitempty"`
	Available     bool           `json:"available"`
	LastCheck     string         `json:"last_check,omitempty"`
	LastApply     string         `json:"last_apply,omitempty"`
	IntervalHours int            `json:"interval_hours"`
	AutoUpdate    bool           `json:"auto_update"`
	PinDigests    bool           `json:"pin_digests"`
	Services      []serviceState `json:"services"`
	Detail        string         `json:"detail,omitempty"`
	Configured    bool           `json:"configured"`
	ImagePrefix   string         `json:"image_prefix,omitempty"`
	VerifyEnabled bool           `json:"verify_enabled"`
	Job           *jobState      `json:"job,omitempty"`
}

type pinsFile struct {
	Services map[string]string `json:"services"`
}

type server struct {
	token            string
	composeDir       string
	statePath        string
	pinsPath         string
	overridePath     string
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
	phase     string
	job       *jobState
	pins      map[string]string
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

	s := &server{
		token:            token,
		composeDir:       composeDir,
		statePath:        filepath.Join(stateDir, "settings.json"),
		pinsPath:         filepath.Join(stateDir, "pins.json"),
		overridePath:     filepath.Join(composeDir, "docker-compose.pins.yml"),
		imagePrefix:      strings.TrimRight(envOr("SNIKKET_UPDATER_IMAGE_PREFIX", "ghcr.io/sudo-ivan/snikketx"), "/"),
		verifyEnabled:    envBool("SNIKKET_UPDATER_VERIFY_SIGNATURES", true),
		requireVerify:    envBool("SNIKKET_UPDATER_REQUIRE_SIGNATURES", false),
		cosignIdentity:   envOr("SNIKKET_UPDATER_COSIGN_IDENTITY_REGEXP", "https://github.com/Sudo-Ivan/snikketx/.*"),
		cosignOIDCIssuer: envOr("SNIKKET_UPDATER_COSIGN_OIDC_ISSUER", "https://token.actions.githubusercontent.com"),
		cosignBin:        envOr("SNIKKET_UPDATER_COSIGN_BIN", "cosign"),
		settings: settings{
			IntervalHours: 24,
			AutoUpdate:    false,
			PinDigests:    true,
		},
		pins: map[string]string{},
	}
	s.loadSettings()
	s.loadPins()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.auth(s.handleStatus))
	mux.HandleFunc("POST /v1/check", s.auth(s.handleCheck))
	mux.HandleFunc("POST /v1/apply", s.auth(s.handleApply))
	mux.HandleFunc("GET /v1/settings", s.auth(s.handleGetSettings))
	mux.HandleFunc("PUT /v1/settings", s.auth(s.handlePutSettings))
	mux.HandleFunc("GET /v1/pins", s.auth(s.handleGetPins))
	mux.HandleFunc("PUT /v1/pins", s.auth(s.handlePutPins))
	mux.HandleFunc("DELETE /v1/pins", s.auth(s.handleClearPins))
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
	if !next.PinDigests {
		_ = s.writePinOverride(map[string]string{})
	} else {
		s.mu.Lock()
		pins := copyMap(s.pins)
		s.mu.Unlock()
		_ = s.writePinOverride(pins)
	}
	writeJSON(w, next)
}

func (s *server) handleGetPins(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, pinsFile{Services: copyMap(s.pins)})
}

func (s *server) handlePutPins(w http.ResponseWriter, r *http.Request) {
	var body pinsFile
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Services == nil {
		body.Services = map[string]string{}
	}
	s.mu.Lock()
	s.pins = body.Services
	pinMode := s.settings.PinDigests
	s.mu.Unlock()
	if err := s.savePins(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pinMode {
		_ = s.writePinOverride(body.Services)
	}
	writeJSON(w, body)
}

func (s *server) handleClearPins(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.pins = map[string]string{}
	s.mu.Unlock()
	_ = s.savePins()
	_ = s.writePinOverride(map[string]string{})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleCheck(w http.ResponseWriter, r *http.Request) {
	if err := s.startJob("check", s.runCheckJob); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, s.snapshot())
}

func (s *server) handleApply(w http.ResponseWriter, r *http.Request) {
	if err := s.startJob("apply", s.runApplyJob); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
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
		busy := s.busy
		s.mu.Unlock()
		if !due || busy {
			continue
		}
		_ = s.startJob("check", func(ctx context.Context) error {
			if err := s.runCheckJob(ctx); err != nil {
				return err
			}
			if auto {
				s.mu.Lock()
				available := s.available
				s.mu.Unlock()
				if available {
					return s.runApplyJob(ctx)
				}
			}
			return nil
		})
	}
}

func (s *server) startJob(kind string, fn func(context.Context) error) error {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return errors.New("updater is busy")
	}
	s.busy = true
	s.phase = kind + "ing"
	if kind == "apply" {
		s.phase = "applying"
	}
	if kind == "check" {
		s.phase = "checking"
	}
	job := &jobState{
		ID:      fmt.Sprintf("%s-%d", kind, time.Now().UTC().Unix()),
		Kind:    kind,
		Status:  "running",
		Phase:   s.phase,
		Steps:   []jobStep{},
		Log:     []string{},
		Started: time.Now().UTC().Format(time.RFC3339),
	}
	s.job = job
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err := fn(ctx)
		s.mu.Lock()
		s.busy = false
		s.phase = "idle"
		if s.job != nil {
			s.job.Ended = time.Now().UTC().Format(time.RFC3339)
			if err != nil {
				s.job.Status = "failed"
				s.job.Error = err.Error()
				s.job.Phase = "failed"
				s.detail = err.Error()
			} else {
				s.job.Status = "succeeded"
				s.job.Phase = "idle"
			}
		}
		s.mu.Unlock()
	}()
	return nil
}

func (s *server) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil {
		s.job.Log = append(s.job.Log, time.Now().UTC().Format(time.RFC3339)+" "+line)
		if len(s.job.Log) > 200 {
			s.job.Log = s.job.Log[len(s.job.Log)-200:]
		}
	}
	s.detail = line
}

func (s *server) setStep(name, status, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == nil {
		return
	}
	at := time.Now().UTC().Format(time.RFC3339)
	for i := range s.job.Steps {
		if s.job.Steps[i].Name == name {
			s.job.Steps[i].Status = status
			s.job.Steps[i].Detail = detail
			s.job.Steps[i].At = at
			return
		}
	}
	s.job.Steps = append(s.job.Steps, jobStep{Name: name, Status: status, Detail: detail, At: at})
}

func (s *server) runCheckJob(ctx context.Context) error {
	s.setStep("inventory", "running", "reading compose images")
	s.logf("inventory: reading local compose images")
	before, err := s.imageInventory(ctx)
	if err != nil {
		s.setStep("inventory", "failed", err.Error())
		return err
	}
	s.setStep("inventory", "done", fmt.Sprintf("%d services", len(before)))

	s.setStep("pull", "running", "docker compose pull")
	s.logf("pull: starting docker compose pull")
	if out, err := s.compose(ctx, "pull"); err != nil {
		s.setStep("pull", "failed", strings.TrimSpace(out))
		s.logf("pull failed: %s", strings.TrimSpace(out))
		return err
	}
	s.setStep("pull", "done", "images pulled")
	s.logf("pull: complete")

	s.setStep("inventory", "running", "re-reading images")
	after, err := s.imageInventory(ctx)
	if err != nil {
		s.setStep("inventory", "failed", err.Error())
		return err
	}

	s.setStep("verify", "running", "cosign keyless verification")
	services, available, verifyFailed := s.buildServiceStates(ctx, before, after)
	if verifyFailed > 0 {
		s.setStep("verify", "failed", fmt.Sprintf("%d signature check(s) failed", verifyFailed))
		s.logf("verify: %d failed", verifyFailed)
		if s.requireVerify {
			s.mu.Lock()
			s.services = services
			s.available = false
			s.lastCheck = time.Now().UTC()
			s.mu.Unlock()
			return fmt.Errorf("signature verification failed for %d image(s)", verifyFailed)
		}
	} else {
		s.setStep("verify", "done", "signatures checked")
		s.logf("verify: complete")
	}

	detail := "all tracked images are current"
	if available {
		detail = "newer container images are ready"
	}
	s.mu.Lock()
	s.services = services
	s.available = available
	s.lastCheck = time.Now().UTC()
	s.detail = detail
	s.mu.Unlock()
	s.logf("check finished: %s", detail)
	return nil
}

func (s *server) runApplyJob(ctx context.Context) error {
	s.mu.Lock()
	pinMode := s.settings.PinDigests
	pins := copyMap(s.pins)
	services := append([]serviceState(nil), s.services...)
	s.mu.Unlock()

	if s.requireVerify {
		for _, svc := range services {
			if svc.SignatureOK != nil && !*svc.SignatureOK {
				return fmt.Errorf("refusing to apply unsigned or unverified image %s", svc.Name)
			}
		}
	}

	s.setStep("apply", "running", "docker compose up -d")
	s.logf("apply: starting compose up")
	if pinMode && len(pins) > 0 {
		if err := s.writePinOverride(pins); err != nil {
			s.setStep("apply", "failed", err.Error())
			return err
		}
		s.logf("apply: using pinned digests for %d services", len(pins))
	}
	if out, err := s.compose(ctx, "up", "-d"); err != nil {
		s.setStep("apply", "failed", strings.TrimSpace(out))
		s.logf("apply failed: %s", strings.TrimSpace(out))
		return err
	}
	s.setStep("apply", "done", "containers updated")
	s.logf("apply: containers updated")

	s.setStep("pin", "running", "recording digests")
	after, err := s.imageInventory(ctx)
	if err != nil {
		s.setStep("pin", "failed", err.Error())
		return err
	}
	if pinMode {
		nextPins := map[string]string{}
		for name, img := range after {
			if img.Digest == "" || !strings.HasPrefix(img.Ref, s.imagePrefix+"/") {
				continue
			}
			base := strings.Split(img.Ref, ":")[0]
			base = strings.Split(base, "@")[0]
			nextPins[name] = base + "@" + img.Digest
		}
		s.mu.Lock()
		s.pins = nextPins
		s.mu.Unlock()
		_ = s.savePins()
		_ = s.writePinOverride(nextPins)
		s.logf("pin: locked %d digests", len(nextPins))
	}
	s.setStep("pin", "done", "digest pins saved")

	s.mu.Lock()
	s.lastApply = time.Now().UTC()
	s.available = false
	for i := range s.services {
		s.services[i].UpdateAvailable = false
		if ref, ok := s.pins[s.services[i].Name]; ok {
			s.services[i].PinnedImage = ref
			if at := strings.Index(ref, "@"); at >= 0 {
				s.services[i].PinnedDigest = ref[at+1:]
			}
		}
	}
	s.detail = "containers updated"
	s.mu.Unlock()
	return nil
}

func (s *server) buildServiceStates(ctx context.Context, before, after map[string]imageInfo) ([]serviceState, bool, int) {
	s.mu.Lock()
	pins := copyMap(s.pins)
	s.mu.Unlock()

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
		pinned := pins[name]
		if pinned != "" && img.Digest != "" && !strings.Contains(pinned, img.Digest) {
			changed = true
		}
		if changed {
			available = true
		}
		row := serviceState{
			Name:            name,
			Image:           img.Ref,
			Digest:          img.Digest,
			UpdateAvailable: changed,
			Registry:        img.Registry,
			PinnedImage:     pinned,
		}
		if pinned != "" {
			if at := strings.Index(pinned, "@"); at >= 0 {
				row.PinnedDigest = pinned[at+1:]
			}
		}
		if s.verifyEnabled && strings.HasPrefix(img.Ref, s.imagePrefix+"/") {
			ok, detail := s.verifyImage(ctx, img)
			row.SignatureOK = &ok
			row.SignatureDetail = detail
			s.logf("verify %s: %v", name, ok)
			if !ok {
				verifyFailed++
			}
		}
		services = append(services, row)
	}
	return services, available, verifyFailed
}

func (s *server) compose(ctx context.Context, args ...string) (string, error) {
	cmdArgs := append([]string{"compose"}, args...)
	s.mu.Lock()
	pinMode := s.settings.PinDigests
	hasPins := len(s.pins) > 0
	s.mu.Unlock()
	if pinMode && hasPins && fileExists(s.overridePath) && (len(args) > 0 && (args[0] == "up" || args[0] == "images")) {
		cmdArgs = append([]string{"compose", "-f", "docker-compose.yml", "-f", "docker-compose.pins.yml"}, args...)
	}
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
		return false, truncate(detail, 240)
	}
	attOK, attDetail := s.verifyAttestation(ctx, target)
	if !attOK {
		return true, "signature ok; sbom attestation: " + truncate(attDetail, 120)
	}
	return true, "signature and sbom attestation verified"
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

func (s *server) writePinOverride(pins map[string]string) error {
	if len(pins) == 0 {
		_ = os.Remove(s.overridePath)
		return nil
	}
	var b strings.Builder
	b.WriteString("# Generated by snikket-updater. Do not edit by hand.\n")
	b.WriteString("services:\n")
	names := make([]string, 0, len(pins))
	for name := range pins {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, "  %s:\n    image: %s\n", name, pins[name])
	}
	return os.WriteFile(s.overridePath, []byte(b.String()), 0o644)
}

func (s *server) snapshot() statusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := "idle"
	phase := s.phase
	if phase == "" {
		phase = "idle"
	}
	if s.busy {
		status = "busy"
	}
	resp := statusResponse{
		Status:        status,
		Phase:         phase,
		Available:     s.available,
		IntervalHours: s.settings.IntervalHours,
		AutoUpdate:    s.settings.AutoUpdate,
		PinDigests:    s.settings.PinDigests,
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
	if s.job != nil {
		jobCopy := *s.job
		jobCopy.Steps = append([]jobStep(nil), s.job.Steps...)
		jobCopy.Log = append([]string(nil), s.job.Log...)
		resp.Job = &jobCopy
	}
	return resp
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

func (s *server) loadPins() {
	data, err := os.ReadFile(s.pinsPath)
	if err != nil {
		return
	}
	var body pinsFile
	if json.Unmarshal(data, &body) != nil || body.Services == nil {
		return
	}
	s.pins = body.Services
}

func (s *server) savePins() error {
	s.mu.Lock()
	body := pinsFile{Services: copyMap(s.pins)}
	s.mu.Unlock()
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.pinsPath, data, 0o600)
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

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
