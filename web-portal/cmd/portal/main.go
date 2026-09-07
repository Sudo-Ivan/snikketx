// Command portal serves the SnikketX web portal.
package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/audit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/config"
	"github.com/sudo-ivan/snikketx/web-portal/internal/handlers"
	"github.com/sudo-ivan/snikketx/web-portal/internal/health"
	"github.com/sudo-ivan/snikketx/web-portal/internal/metrics"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/updater"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
	"github.com/sudo-ivan/snikketx/web-portal/web"
)

// version is set through the linker at build time.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

const (
	// errorRingSize is how many recent errors the health page keeps.
	errorRingSize = 64
	// shutdownGrace is how long in flight requests may finish during a
	// graceful shutdown.
	shutdownGrace = 15 * time.Second
	// readHeaderTimeout bounds how long a client may take to send headers.
	readHeaderTimeout = 15 * time.Second
	// writeTimeout bounds how long a response may take to be written.
	writeTimeout = 60 * time.Second
	// idleTimeout bounds how long a keep alive connection stays open.
	idleTimeout = 120 * time.Second
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel(),
	})))

	if err := run(); err != nil {
		slog.Error("startup failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// logLevel reads the requested log level, defaulting to info.
func logLevel() slog.Level {
	switch strings.ToLower(os.Getenv("SNIKKET_WEB_LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// run wires the application together and serves until a signal arrives.
func run() error {
	cfg, err := config.Load(version, commit, buildDate)
	if err != nil {
		return err
	}

	templateFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		return err
	}
	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		return err
	}

	stateDir := envOr("SNIKKET_WEB_STATE_DIR", "/var/lib/snikket-web-portal")
	auditStore, err := audit.Open(stateDir, 512)
	if err != nil {
		slog.Warn("audit log unavailable", slog.String("error", err.Error()))
		auditStore = nil
	}

	sprite, err := fs.ReadFile(staticFS, "img/icons.svg")
	if err != nil {
		slog.Warn("icon sprite missing, icons will not render",
			slog.String("error", err.Error()))
		sprite = nil
	}

	renderer, err := webui.New(templateFS, webui.Options{Sprite: sprite})
	if err != nil {
		return err
	}

	sessStore, err := session.New(cfg.SecretKey, secureCookies())
	if err != nil {
		return err
	}

	prosodyClient := prosody.New(cfg.ProsodyEndpoint, cfg.Domain, version)
	prosodyClient.SetCredentialsPath(prosody.DefaultOAuthCredentialsPath(stateDir))
	if err := prosodyClient.LoadStoredCredentials(); err != nil {
		slog.Warn("oauth credentials not loaded", slog.String("error", err.Error()))
	}

	app := &handlers.App{
		Cfg:       cfg,
		Prosody:   prosodyClient,
		Sessions:  sessStore,
		Templates: renderer,
		Errors:    health.NewRing(errorRingSize),
		Audit:     auditStore,
		Metrics:   metrics.New(),
		LoginGate: authlimit.New(),
		Updater: &updater.Client{
			Endpoint: cfg.UpdaterEndpoint,
			Token:    cfg.UpdaterToken,
		},
		Started: time.Now(),
		Static:  staticHandler(staticFS),
	}

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening",
			slog.String("addr", cfg.ListenAddr),
			slog.String("domain", cfg.Domain),
			slog.String("version", version),
		)
		errc <- server.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

// secureCookies reports whether session cookies must carry the Secure
// attribute. It is on unless the operator turns it off for a plain HTTP
// development setup.
func secureCookies() bool {
	switch strings.ToLower(os.Getenv("SNIKKET_WEB_INSECURE_COOKIES")) {
	case "1", "true", "yes":
		return false
	default:
		return true
	}
}

// staticHandler serves the embedded assets with a long cache lifetime, since
// every asset is versioned with the container image.
func staticHandler(staticFS fs.FS) http.Handler {
	fileServer := http.StripPrefix("/static/", http.FileServerFS(staticFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fileServer.ServeHTTP(w, r)
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
