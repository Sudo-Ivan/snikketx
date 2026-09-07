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
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/config"
	"github.com/sudo-ivan/snikketx/web-portal/internal/csrf"
	"github.com/sudo-ivan/snikketx/web-portal/internal/health"
	"github.com/sudo-ivan/snikketx/web-portal/internal/metrics"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

const (
	// minPasswordLength is the shortest password the portal accepts.
	minPasswordLength = 10
	// maxRequestBody caps the size of any request body the portal parses.
	maxRequestBody = 16 << 20
	// maxImportSize caps an uploaded XEP-0227 account data document.
	maxImportSize = 5 << 20
	// resetInviteTTL is the lifetime of an administrator issued password
	// reset link.
	resetInviteTTL = 86400
)

// App carries the dependencies shared by every handler.
type App struct {
	Cfg       *config.Config
	Prosody   *prosody.Client
	Sessions  *session.Store
	Templates *webui.Renderer
	Errors    *health.Ring
	Metrics   *metrics.Registry
	LoginGate *authlimit.Limiter
	Started   time.Time
	Static    http.Handler
}

// ctxKey is the private key type used for request scoped values.
type ctxKey int

// ctxRequestID carries the generated request id.
const ctxRequestID ctxKey = iota

// Routes builds the router with the full middleware chain applied.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	a.mountMain(mux)
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

// csrfExempt reports whether a path is excluded from CSRF validation.
func csrfExempt(path string) bool {
	return path == "/metrics" || strings.HasPrefix(path, "/_health")
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
	case strings.HasPrefix(path, "/_health"):
		return "health"
	case path == "/metrics":
		return "metrics"
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

// requireSession returns the caller's session, redirecting to the login page
// when there is none.
func (a *App) requireSession(w http.ResponseWriter, r *http.Request) (session.Data, bool) {
	sess := a.Sessions.Get(r)
	if !sess.HasSession() {
		a.redirectToLogin(w, r)
		return nil, false
	}
	return sess, true
}

// requireAdmin returns the caller's session when it carries the admin scope.
func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) (session.Data, bool) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return nil, false
	}
	if !sess.IsAdmin() {
		a.renderError(w, r, http.StatusForbidden, "Administrator access required",
			"This page is only available to administrators of this service.")
		return nil, false
	}
	return sess, true
}

// redirectToLogin sends the caller to the login page.
func (a *App) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// newPage assembles the base page data, refreshes the CSRF token, consumes any
// pending flash message and persists the session cookie. It must be called
// before the response headers are written.
func (a *App) newPage(w http.ResponseWriter, r *http.Request, sess session.Data, title, nav, shell string) webui.PageData {
	if sess == nil {
		sess = session.Data{}
	}

	token := csrf.Ensure(sess)
	flash := sess.PopFlash()
	_ = a.Sessions.Save(w, sess)

	page := webui.PageData{
		Title:         title,
		SiteName:      a.Cfg.SiteName,
		Domain:        a.Cfg.Domain,
		Lang:          "en",
		Shell:         shell,
		Nav:           nav,
		Theme:         themeFromRequest(r),
		CSRF:          token,
		RequestID:     requestID(r),
		Version:       a.Cfg.Version,
		HasSession:    sess.HasSession(),
		IsAdmin:       sess.IsAdmin(),
		ShowMetrics:   a.Cfg.ShowMetrics,
		TOSURI:        a.Cfg.TOSURI,
		PrivacyURI:    a.Cfg.PrivacyURI,
		AbuseEmail:    a.Cfg.AbuseEmail,
		SecurityEmail: a.Cfg.SecurityEmail,
		AppleStoreURL: a.Cfg.AppleStoreURL,
		Now:           time.Now().UTC(),
	}

	if flash != nil {
		page.Flash = &webui.Flash{Message: flash.Message, Category: flash.Category}
	}

	if jid := sess.JID(); jid != "" {
		localpart, _, _ := xmpp.SplitJID(jid)
		if localpart == "" {
			localpart = jid
		}
		page.User = &webui.UserSummary{
			Address:     jid,
			Username:    localpart,
			DisplayName: localpart,
			IsAdmin:     sess.IsAdmin(),
		}
	}
	return page
}

// themeFromRequest reads the theme preference set by the toggle.
func themeFromRequest(r *http.Request) string {
	cookie, err := r.Cookie("theme")
	if err != nil {
		return "light"
	}
	switch cookie.Value {
	case "dark", "light":
		return cookie.Value
	default:
		return "light"
	}
}

// applyUserInfo copies a fetched profile summary into the page data.
func applyUserInfo(page *webui.PageData, info *prosody.UserInfo) {
	if info == nil {
		return
	}
	page.User = &webui.UserSummary{
		Address:     info.Address,
		Username:    info.Username,
		Nickname:    info.Nickname,
		DisplayName: info.DisplayName,
		AvatarHash:  info.AvatarHash,
		IsAdmin:     info.IsAdmin,
	}
}

// render writes a page, falling back to a plain 500 when the template itself
// fails so the caller never sees an empty response.
func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	if err := a.Templates.RenderStatus(w, status, name, data); err != nil {
		id := a.recordError(r, err)
		slog.Error("render failed",
			slog.String("request_id", requestID(r)),
			slog.String("error_id", id),
			slog.String("template", name),
			slog.String("error", err.Error()),
		)
	}
}

// renderError writes the generic status page.
func (a *App) renderError(w http.ResponseWriter, r *http.Request, status int, title, detail string) {
	sess := a.Sessions.Get(r)
	page := a.newPage(w, r, sess, title, "", webui.ShellBare)

	data := struct {
		webui.PageData
		Status int
		Detail string
	}{PageData: page, Status: status, Detail: detail}

	if err := a.Templates.RenderStatus(w, status, "error.html", data); err != nil {
		http.Error(w, title, status)
	}
}

// backendError writes the 503 status page shown when Prosody is unreachable.
func (a *App) backendError(w http.ResponseWriter, r *http.Request, cause error) {
	id := a.recordError(r, cause)
	slog.Error("backend unreachable",
		slog.String("request_id", requestID(r)),
		slog.String("error_id", id),
		slog.String("path", r.URL.Path),
		slog.String("error", cause.Error()),
	)

	sess := a.Sessions.Get(r)
	page := a.newPage(w, r, sess, "Service unavailable", "", webui.ShellBare)

	data := struct {
		webui.PageData
		ErrorID string
	}{PageData: page, ErrorID: id}

	if err := a.Templates.RenderStatus(w, http.StatusServiceUnavailable, "backend_error.html", data); err != nil {
		http.Error(w, "backend unavailable", http.StatusServiceUnavailable)
	}
}

// failAPI turns a failed Prosody call into the appropriate response. An
// expired or rejected token clears the session and returns to the login page,
// a transport failure yields the backend status page and anything else yields
// a bad gateway status page.
func (a *App) failAPI(w http.ResponseWriter, r *http.Request, err error) {
	switch prosody.StatusOf(err) {
	case http.StatusUnauthorized, http.StatusForbidden:
		sess := a.Sessions.Get(r)
		sess.ClearAuth()
		sess.PushFlash("Your session expired, please sign in again.", "alert")
		_ = a.Sessions.Save(w, sess)
		a.redirectToLogin(w, r)
	case 0:
		a.backendError(w, r, err)
	default:
		id := a.recordError(r, err)
		slog.Error("backend call failed",
			slog.String("request_id", requestID(r)),
			slog.String("error_id", id),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		a.renderError(w, r, http.StatusBadGateway, "The chat server refused the request",
			"The chat server answered with an error. The incident was recorded as "+id+".")
	}
}

// recordError stores an error in the ring buffer shown on the health page and
// returns the identifier assigned to it.
func (a *App) recordError(r *http.Request, cause error) string {
	id := newID()
	if a.Errors == nil {
		return id
	}
	message := "unknown error"
	if cause != nil {
		message = cause.Error()
	}
	a.Errors.Add(health.ErrorEntry{
		ID:        id,
		Message:   r.Method + " " + r.URL.Path + ": " + message,
		When:      time.Now().UTC(),
		RequestID: requestID(r),
	})
	return id
}

// apiErrorMessage maps a structured backend error onto the wording the portal
// shows, falling back to a generic message.
func apiErrorMessage(err error) string {
	if apiErr, ok := errors.AsType[*prosody.APIError](err); ok {
		if apiErr.Extra != nil {
			switch apiErr.Extra["condition"] {
			case "cannot-remove-only-admin":
				return "Cannot remove the only administrator."
			case "user-not-found":
				return "User account not found."
			case "group-not-found":
				return "Circle not found."
			case "group-name-required":
				return "A circle name is required."
			case "conflict":
				return "That already exists."
			}
		}
		switch apiErr.Condition {
		case "conflict":
			return "That already exists."
		case "item-not-found":
			return "Not found."
		case "feature-not-implemented":
			return "The chat server does not support this action."
		case "service-unavailable":
			return "Unable to perform the requested action."
		}
		if apiErr.Text != "" {
			return apiErr.Text
		}
	}
	if status := prosody.StatusOf(err); status == http.StatusConflict {
		return "That already exists."
	}
	return "The chat server refused the request."
}

// flashRedirect stores a flash message and redirects.
func (a *App) flashRedirect(w http.ResponseWriter, r *http.Request, sess session.Data, message, category, target string) {
	sess.PushFlash(message, category)
	_ = a.Sessions.Save(w, sess)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// notFound writes the generic 404 status page.
func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.renderError(w, r, http.StatusNotFound, "Page not found",
		"The page you asked for does not exist on this service.")
}
