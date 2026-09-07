package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

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
