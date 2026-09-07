package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	addr := envOr("SNIKKET_BACKUP_LISTEN", "0.0.0.0:9292")
	token := strings.TrimSpace(os.Getenv("SNIKKET_BACKUP_TOKEN"))
	if token == "" {
		token = "snikket-backup-local"
		log.Printf("SNIKKET_BACKUP_TOKEN unset, using built-in local default")
	}
	stateDir := envOr("SNIKKET_BACKUP_STATE_DIR", "/var/lib/snikket-backup")
	archiveDir := envOr("SNIKKET_BACKUP_ARCHIVE_DIR", filepath.Join(stateDir, "archives"))
	if err := os.MkdirAll(archiveDir, 0o750); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		log.Fatal(err)
	}

	s := &server{
		token:        token,
		composeDir:   envOr("SNIKKET_BACKUP_COMPOSE_DIR", "/work"),
		archiveDir:   archiveDir,
		statePath:    filepath.Join(stateDir, "state.json"),
		settingsPath: filepath.Join(stateDir, "settings.json"),
		alpineImage:  envOr("SNIKKET_BACKUP_ALPINE_IMAGE", "alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"),
		resticBin:    envOr("SNIKKET_BACKUP_RESTIC_BIN", "restic"),
		resticRepo:   strings.TrimSpace(os.Getenv("RESTIC_REPOSITORY")),
		resticPass:   strings.TrimSpace(firstNonEmpty(os.Getenv("RESTIC_PASSWORD"), readTrimmed(os.Getenv("RESTIC_PASSWORD_FILE")))),
		settings: settings{
			Enabled:         false,
			IntervalHours:   24,
			Scope:           "full",
			KeepCount:       7,
			KeepDays:        30,
			ResticKeepLast:  7,
			ResticKeepDaily: 14,
		},
	}
	s.loadSettings()
	s.loadState()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.auth(s.handleStatus))
	mux.HandleFunc("GET /v1/settings", s.auth(s.handleGetSettings))
	mux.HandleFunc("PUT /v1/settings", s.auth(s.handlePutSettings))
	mux.HandleFunc("POST /v1/backup", s.auth(s.handleBackup))
	mux.HandleFunc("GET /v1/archives", s.auth(s.handleArchives))
	mux.HandleFunc("POST /v1/restore/dry-run", s.auth(s.handleDryRun))
	mux.HandleFunc("POST /v1/restic/check", s.auth(s.handleResticCheck))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	go s.loop()
	log.Printf("snikket backup listening on %s archives=%s", addr, s.archiveDir)
	log.Fatal(http.ListenAndServe(addr, mux))
}
