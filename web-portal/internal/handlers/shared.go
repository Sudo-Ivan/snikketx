package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/audit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/csrf"
	"github.com/sudo-ivan/snikketx/web-portal/internal/health"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

// Redirect targets used by more than one handler.
const (
	pathLogin            = "/login"
	pathUserHome         = "/user/"
	pathUserPasswd       = "/user/passwd"
	pathUserProfile      = "/user/profile"
	pathAdminUsers       = "/admin/users"
	pathAdminInvitations = "/admin/invitations"
	pathAdminCircles     = "/admin/circles"
	pathAdminMUCs        = "/admin/mucs"
	pathAdminSystem      = "/admin/system/"
	pathAdminHealth      = "/admin/health/"
	pathAdminDevices     = "/admin/devices"
	pathAdminStorage     = "/admin/storage"
	pathAdminUpdates     = "/admin/updates"
	pathAdminLimits      = "/admin/limits/"
	pathAdminBackup      = "/admin/backup/"
	pathAdminApps        = "/admin/apps"
	pathAdminCircleNew   = "/admin/circle/-/new"
	pathAdminInviteNew   = "/admin/invitation/-/new"
	pathAdminMUCNew      = "/admin/muc/-/new"
)

// Form field names shared between the handlers and the templates.
const (
	fieldAction    = "action"
	fieldName      = "name"
	fieldUser      = "user"
	fieldRole      = "role"
	fieldPassword  = "password"
	fieldLocalpart = "localpart"
)

// Path parameter names declared in the route patterns.
const (
	paramID        = "id"
	paramLocalpart = "localpart"
)

// Android store links used when the APK cache is absent.
const (
	// defaultAndroidPackageID is the upstream Snikket app id used when
	// neither the cache settings nor the environment name a fork.
	defaultAndroidPackageID = "org.snikket.android"
	// androidPlayStoreBase is the Play Store listing the package id is
	// appended to as the id query parameter.
	androidPlayStoreBase = "https://play.google.com/store/apps/details?id="
	// androidFDroidPackages is the F-Droid package page prefix.
	androidFDroidPackages = "https://f-droid.org/packages/"
)

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

// requireTokenSession returns the caller's session when it carries a Prosody
// token. Single sign-on sessions have a JID but no token, so pages that need
// the backend send them to the app bootstrap page instead.
func (a *App) requireTokenSession(w http.ResponseWriter, r *http.Request) (session.Data, bool) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return nil, false
	}
	if sess.IsOIDC() {
		a.flashRedirect(w, r, sess,
			"This area needs a chat account password. Single sign-on accounts can connect an app instead.",
			"info", pathUserApp)
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
	http.Redirect(w, r, pathLogin, http.StatusSeeOther)
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
		BuildCommit:   shortCommit(a.Cfg.BuildCommit),
		BuildDate:     displayBuildDate(a.Cfg.BuildDate),
		Uptime:        formatUptime(time.Since(a.Started)),
		HasSession:    sess.HasSession(),
		IsAdmin:       sess.IsAdmin(),
		IsOIDC:        sess.IsOIDC(),
		OIDCEnabled:   a.OIDC != nil,
		ShowMetrics:   a.Cfg.ShowMetrics,
		TOSURI:        a.Cfg.TOSURI,
		PrivacyURI:    a.Cfg.PrivacyURI,
		AbuseEmail:    a.Cfg.AbuseEmail,
		SecurityEmail: a.Cfg.SecurityEmail,
		AppleStoreURL: a.Cfg.AppleStoreURL,
		Now:           time.Now().UTC(),
	}
	page.HealthStatus, page.HealthLabel = a.footerHealth(r.Context())
	a.applyAppLinks(&page)

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
				return msgCircleNameRequired
			case "conflict":
				return msgAlreadyExists
			}
		}
		switch apiErr.Condition {
		case "conflict":
			return msgAlreadyExists
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
		return msgAlreadyExists
	}
	switch status := prosody.StatusOf(err); status {
	case http.StatusUnauthorized:
		return "Chat server authentication failed."
	case http.StatusForbidden:
		return "Administrator access was denied by the chat server."
	case http.StatusNotFound:
		return "That chat server feature is not available."
	case http.StatusServiceUnavailable:
		return "That chat server feature is unavailable right now."
	case http.StatusBadGateway, http.StatusGatewayTimeout:
		return "The chat server did not respond in time."
	}
	return "The chat server refused the request."
}

// flashRedirect stores a flash message and redirects.
func (a *App) flashRedirect(w http.ResponseWriter, r *http.Request, sess session.Data, message, category, target string) {
	sess.PushFlash(message, category)
	_ = a.Sessions.Save(w, sess)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// recordAudit appends an administrator action to the portal audit log.
func (a *App) recordAudit(r *http.Request, sess session.Data, action, target, detail string) {
	if a.Audit == nil {
		return
	}
	actor := ""
	if sess != nil {
		actor = sess.JID()
	}
	a.Audit.Record(audit.Event{
		Actor:     actor,
		Action:    action,
		Target:    target,
		Detail:    detail,
		IP:        authlimit.ClientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: requestID(r),
	})
}

// notFound writes the generic 404 status page.
func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.renderError(w, r, http.StatusNotFound, "Page not found",
		"The page you asked for does not exist on this service.")
}

const footerHealthTTL = 15 * time.Second

// footerHealth returns a cached Healthy / Degraded / Down label for the footer.
func (a *App) footerHealth(ctx context.Context) (status, label string) {
	a.healthMu.Lock()
	defer a.healthMu.Unlock()
	if a.healthStatus != "" && time.Since(a.healthAt) < footerHealthTTL {
		return a.healthStatus, healthLabel(a.healthStatus)
	}

	status = "healthy"
	prosody := a.probeProsody(ctx)
	if !prosody.OK {
		status = "down"
	} else if a.Errors != nil && len(a.Errors.Recent()) > 0 {
		status = "degraded"
	}

	a.healthStatus = status
	a.healthAt = time.Now()
	return status, healthLabel(status)
}

func healthLabel(status string) string {
	switch status {
	case "degraded":
		return "Degraded"
	case "down":
		return "Down"
	default:
		return "Healthy"
	}
}

func shortCommit(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "unknown" {
		return ""
	}
	if len(raw) > 7 {
		return raw[:7]
	}
	return raw
}

func displayBuildDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "unknown" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format("2006-01-02")
	}
	if len(raw) >= 10 {
		return raw[:10]
	}
	return raw
}

func formatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm %ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// applyAppLinks fills store and self-hosted APK fields used by invite and user pages.
func (a *App) applyAppLinks(page *webui.PageData) {
	if a.AppCache == nil {
		page.PlayStoreURL = a.playStoreURL()
		page.FDroidURL = a.fDroidURL()
		return
	}
	page.PlayStoreURL = a.AppCache.PlayURL()
	page.FDroidURL = a.AppCache.FDroidWebURL()
	if a.AppCache.Ready() {
		meta := a.AppCache.Meta()
		page.AndroidAPKReady = true
		page.AndroidDownloadURL = "/download/android.apk"
		page.AndroidAPKVersion = meta.Version
		page.AndroidAPKSizeLabel = webui.FormatBytes(meta.Size)
	}
}

// playStoreURL returns the configured Play Store link, or the listing for the
// configured package id when none is set.
func (a *App) playStoreURL() string {
	if a.Cfg != nil && a.Cfg.PlayStoreURL != "" {
		return a.Cfg.PlayStoreURL
	}
	return androidPlayStoreBase + a.androidPackageID()
}

// fDroidURL returns the configured F-Droid web link, or the package page for
// the configured package id when none is set. A market:// override is not a
// web link, so it falls through to the default page.
func (a *App) fDroidURL() string {
	if a.Cfg != nil && strings.HasPrefix(a.Cfg.FDroidURL, "http") {
		return a.Cfg.FDroidURL
	}
	return androidFDroidPackages + a.androidPackageID() + "/"
}

// androidPackageID returns the configured Android package id for store links.
func (a *App) androidPackageID() string {
	if a.AppCache != nil {
		return a.AppCache.PackageID()
	}
	if a.Cfg != nil && a.Cfg.AndroidPackageID != "" {
		return a.Cfg.AndroidPackageID
	}
	return defaultAndroidPackageID
}

// androidFDroidMarketURL returns the F-Droid market:// link for invite pages.
func (a *App) androidFDroidMarketURL() string {
	if a.AppCache != nil {
		return a.AppCache.FDroidMarketURL()
	}
	return "market://details?id=" + a.androidPackageID()
}

func stringOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
