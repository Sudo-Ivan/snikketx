package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	addr := envOr(envListenAddr, defaultListenAddr)
	token := strings.TrimSpace(os.Getenv(envToken))
	if token == "" {
		token = defaultToken
		log.Printf("%s unset, using built-in local default", envToken)
	}
	if token == defaultToken {
		slog.Warn(envToken + " is the well-known default; set a random token in .env")
	}
	stateDir := envOr(envStateDir, defaultStateDir)
	archiveDir := envOr(envArchiveDir, filepath.Join(stateDir, archivesDirName))
	if err := os.MkdirAll(archiveDir, dirMode); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(stateDir, dirMode); err != nil {
		log.Fatal(err)
	}

	s := &server{
		token:        token,
		tokenBytes:   []byte(token),
		composeDir:   envOr(envComposeDir, defaultComposeDir),
		archiveDir:   archiveDir,
		statePath:    filepath.Join(stateDir, stateFileName),
		settingsPath: filepath.Join(stateDir, settingsFileName),
		alpineImage:  envOr(envAlpineImage, defaultAlpineImage),
		resticBin:    envOr(envResticBin, defaultResticBin),
		resticRepo:   strings.TrimSpace(os.Getenv(envResticRepo)),
		resticPass:   strings.TrimSpace(firstNonEmpty(os.Getenv(envResticPass), readTrimmed(os.Getenv(envResticPassFile)))),
		settings: settings{
			Enabled:         false,
			IntervalHours:   defaultIntervalHours,
			Scope:           defaultScope,
			KeepCount:       defaultKeepCount,
			KeepDays:        defaultKeepDays,
			ResticKeepLast:  defaultResticKeepLast,
			ResticKeepDaily: defaultResticKeepDaily,
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
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", "2")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(okBody)
	})

	go s.loop()
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}
	log.Printf("snikket backup listening on %s archives=%s", addr, s.archiveDir)
	log.Fatal(srv.ListenAndServe())
}

var okBody = []byte("ok")
