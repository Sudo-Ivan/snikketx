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
	composeDir := envOr(envComposeDir, defaultComposeDir)
	stateDir := envOr(envStateDir, defaultStateDir)
	if err := os.MkdirAll(stateDir, stateDirMode); err != nil {
		log.Fatal(err)
	}

	s := &server{
		token:            token,
		tokenBytes:       []byte(token),
		composeDir:       composeDir,
		statePath:        filepath.Join(stateDir, settingsFileName),
		pinsPath:         filepath.Join(stateDir, pinsFileName),
		overridePath:     filepath.Join(composeDir, overrideFileName),
		imagePrefix:      strings.TrimRight(envOr(envImagePrefix, defaultImagePrefix), "/"),
		verifyEnabled:    envBool(envVerifySignatures, defaultVerifySignatures),
		requireVerify:    envBool(envRequireSignatures, defaultRequireSignatures),
		cosignIdentity:   envOr(envCosignIdentity, defaultCosignIdentity),
		cosignOIDCIssuer: envOr(envCosignOIDCIssuer, defaultCosignOIDCIssuer),
		cosignBin:        envOr(envCosignBin, defaultCosignBin),
		settings: settings{
			IntervalHours: defaultIntervalHours,
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
	mux.HandleFunc("GET /v1/logs", s.auth(s.handleLogs))
	mux.HandleFunc("GET /services", s.auth(s.handleServices))
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
	log.Printf("snikket updater listening on %s prefix=%s verify=%v", addr, s.imagePrefix, s.verifyEnabled)
	log.Fatal(srv.ListenAndServe())
}

var okBody = []byte("ok")
