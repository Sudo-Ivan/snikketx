// Package handlers implements the HTTP surface of the SnikketX web portal.
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/appcache"
	"github.com/sudo-ivan/snikketx/web-portal/internal/audit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	backupclient "github.com/sudo-ivan/snikketx/web-portal/internal/backupclient"
	"github.com/sudo-ivan/snikketx/web-portal/internal/config"
	"github.com/sudo-ivan/snikketx/web-portal/internal/credstore"
	"github.com/sudo-ivan/snikketx/web-portal/internal/csrf"
	"github.com/sudo-ivan/snikketx/web-portal/internal/health"
	"github.com/sudo-ivan/snikketx/web-portal/internal/linkpreview"
	"github.com/sudo-ivan/snikketx/web-portal/internal/metrics"
	"github.com/sudo-ivan/snikketx/web-portal/internal/oidc"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/updater"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

const (
	// minPasswordLength is the shortest password the portal accepts.
	minPasswordLength = 10
	// maxRequestBody caps the size of any request body the portal parses.
	// Sized to allow admin Android APK uploads under the appcache ceiling.
	maxRequestBody = 64 << 20
	// maxImportSize caps an uploaded XEP-0227 account data document.
	maxImportSize = 5 << 20
	// resetInviteTTL is the lifetime of an administrator issued password
	// reset link.
	resetInviteTTL = 86400
)

// App carries the dependencies shared by every handler.
type App struct {
	Cfg       *config.Config
	Prosody   ProsodyClient
	Sessions  *session.Store
	Templates *webui.Renderer
	Errors    *health.Ring
	Audit     *audit.Store
	Metrics   *metrics.Registry
	LoginGate *authlimit.Limiter
	// Credentials stores passkeys and TOTP secrets. Nil disables the
	// second-factor features.
	Credentials *credstore.Store
	// OIDC is the configured external identity provider. Nil disables
	// single sign-on.
	OIDC *oidc.Provider
	// Service is the cached login of the portal service account used for
	// operator level calls like single sign-on account provisioning. Nil
	// disables those calls.
	Service  *serviceAuth
	Updater  *updater.Client
	Backup   *backupclient.Client
	AppCache *appcache.Cache
	APKGate  *appcache.DownloadLimiter
	Started  time.Time
	Static   http.Handler
	// LinkPreview fetches OpenGraph metadata for the API endpoints.
	// LinkPreviewGate caps lookups per authenticated account.
	LinkPreview     LinkPreviewer
	LinkPreviewGate *linkpreview.Limiter

	healthMu     sync.Mutex
	healthAt     time.Time
	healthStatus string

	logSourcesMu    sync.Mutex
	logSourcesAt    time.Time
	logSourcesCache []logSource
}

// ctxKey is the private key type used for request scoped values.
type ctxKey int

// ctxRequestID carries the generated request id.
const ctxRequestID ctxKey = iota

// Routes builds the router with the full middleware chain applied.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	// The anonymous XMPP protocol endpoints are mounted first. They carry
	// their own authentication and skip session, CSRF and browser security
	// header handling.
	a.mountXMPPProxy(mux)
	a.mountLinkPreview(mux)
	a.mountMain(mux)
	a.mountOIDC(mux)
	a.mountSecurity(mux)
	a.mountUser(mux)
	a.mountAdmin(mux)
	a.mountInvite(mux)
	a.mountMetrics(mux)

	if a.Static != nil {
		mux.Handle("GET /static/", a.Static)
	}

	var handler http.Handler = mux
	handler = a.csrfMiddleware(handler)
	handler = a.observeMiddleware(handler)
	handler = a.recoverMiddleware(handler)
	handler = secureHeaders(handler)
	handler = a.accessLog(handler)
	handler = requestIDMiddleware(handler)
	return handler
}

// statusWriter records the status and the size of a response so the access log
// and the metrics registry can report them.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

// WriteHeader records the status before passing it on.
func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

// Write records the response size, defaulting the status to 200.
func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

// Unwrap exposes the wrapped writer so http.ResponseController can reach
// Hijack and Flush for proxied WebSocket and streaming responses.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush relays a flush to the underlying writer when it supports streaming.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Status returns the recorded status, defaulting to 200 for handlers that
// never wrote a header.
func (w *statusWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// requestIDMiddleware attaches a random request id to the context and echoes
// it back so a report can be traced in the logs.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newID()
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxRequestID, id)))
	})
}

// accessLog writes one structured JSON line per request.
func (a *App) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)

		slog.Info("request",
			slog.String("request_id", requestID(r)),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("route_group", routeGroup(r.URL.Path)),
			slog.Int("status", recorder.Status()),
			slog.Int("bytes", recorder.bytes),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.String("user_agent", r.UserAgent()),
		)
	})
}

// secureHeaders applies the response headers that harden the portal in the
// browser. Inline style attributes stay allowed because avatar placeholders
// carry a per address colour.
func secureHeaders(next http.Handler) http.Handler {
	const policy = "default-src 'none'; base-uri 'none'; form-action 'self'; " +
		"frame-ancestors 'none'; img-src 'self' data:; font-src 'self'; " +
		"style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; " +
		"manifest-src 'self'"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Proxied XMPP protocol endpoints keep the headers Prosody sends so
		// its CORS and discovery responses reach external clients unchanged.
		if xmppProxyPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		header := w.Header()
		header.Set("Content-Security-Policy", policy)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "same-origin")
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		header.Set("Cross-Origin-Resource-Policy", "same-origin")
		header.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		next.ServeHTTP(w, r)
	})
}

// recoverMiddleware turns a panic into a logged 500 status page.
func (a *App) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(recovered)
			}

			id := a.recordError(r, fmt.Errorf("panic: %v", recovered))
			slog.Error("panic",
				slog.String("request_id", requestID(r)),
				slog.String("error_id", id),
				slog.String("path", r.URL.Path),
				slog.Any("value", recovered),
				slog.String("stack", string(debug.Stack())),
			)
			a.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
				"The portal could not finish this request. The incident was recorded as "+id+".")
		}()
		next.ServeHTTP(w, r)
	})
}

// observeMiddleware records request counts and durations per route group.
func (a *App) observeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Metrics == nil {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		a.Metrics.Observe(routeGroup(r.URL.Path), time.Since(start))
	})
}

// csrfMiddleware rejects unsafe requests that carry no valid CSRF token. The
// health and metrics endpoints are exempt because they take no form input.
func (a *App) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safeMethod(r.Method) || csrfExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		if !csrf.Valid(r, a.Sessions.Get(r)) {
			a.renderError(w, r, http.StatusForbidden, "Request could not be verified",
				"The form you submitted has expired. Go back, reload the page and try again.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// safeMethod reports whether the method is read only.
func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// csrfExempt reports whether a path is excluded from CSRF validation. The
// proxied XMPP endpoints are exempt because external XMPP clients have no
// portal session and carry no CSRF token.
func csrfExempt(path string) bool {
	return path == "/metrics" || strings.HasPrefix(path, "/_health") || xmppProxyPath(path)
}

// routeGroup buckets a path for the metrics registry.
func routeGroup(path string) string {
	switch {
	case strings.HasPrefix(path, "/static/"):
		return "static"
	case strings.HasPrefix(path, "/admin"):
		return "admin"
	case strings.HasPrefix(path, "/user"):
		return "user"
	case strings.HasPrefix(path, "/invite"):
		return "invite"
	case strings.HasPrefix(path, "/avatar/"):
		return "avatar"
	case strings.HasPrefix(path, "/api/"):
		return "api"
	case strings.HasPrefix(path, "/_health"):
		return "health"
	case path == "/metrics":
		return "metrics"
	case xmppProxyPath(path):
		return "xmpp"
	default:
		return "main"
	}
}

// requestID returns the request id attached by the middleware.
func requestID(r *http.Request) string {
	if id, ok := r.Context().Value(ctxRequestID).(string); ok {
		return id
	}
	return ""
}

// newID returns a short random hexadecimal identifier.
func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
