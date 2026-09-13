package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/linkpreview"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

// LinkPreviewer is the part of linkpreview.Fetcher the handlers call.
// *linkpreview.Fetcher satisfies it implicitly; tests substitute a fake.
type LinkPreviewer interface {
	FetchMetadata(ctx context.Context, rawURL string) (*linkpreview.Metadata, error)
	FetchImage(ctx context.Context, rawURL string) (data []byte, contentType string, err error)
}

// mountLinkPreview registers the link preview API used by the Android app.
// Both endpoints authenticate with HTTP Basic: the username is a bare JID or
// localpart on the configured domain and the password is the account
// password, verified against Prosody exactly like the portal login.
func (a *App) mountLinkPreview(mux *http.ServeMux) {
	if a.Cfg == nil || !a.Cfg.LinkPreviewEnabled {
		return
	}
	if a.LinkPreview == nil {
		a.LinkPreview = linkpreview.New()
	}
	if a.LinkPreviewGate == nil {
		a.LinkPreviewGate = linkpreview.NewLimiter(linkpreview.DefaultUserHourlyLimit)
	}
	mux.HandleFunc("GET /api/link-preview", a.handleLinkPreview)
	mux.HandleFunc("GET /api/link-preview/image", a.handleLinkPreviewImage)
}

// handleLinkPreview returns the OpenGraph metadata for the requested URL.
func (a *App) handleLinkPreview(w http.ResponseWriter, r *http.Request) {
	jid, ok := a.linkPreviewAuth(w, r)
	if !ok {
		return
	}
	if !a.linkPreviewQuota(w, jid) {
		return
	}

	meta, err := a.LinkPreview.FetchMetadata(r.Context(), r.URL.Query().Get("url"))
	if err != nil {
		a.linkPreviewError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// handleLinkPreviewImage streams a proxied preview image or favicon. Only
// image/* upstream responses are relayed and the size is capped.
func (a *App) handleLinkPreviewImage(w http.ResponseWriter, r *http.Request) {
	jid, ok := a.linkPreviewAuth(w, r)
	if !ok {
		return
	}
	if !a.linkPreviewQuota(w, jid) {
		return
	}

	data, contentType, err := a.LinkPreview.FetchImage(r.Context(), r.URL.Query().Get("url"))
	if err != nil {
		a.linkPreviewError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=900")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data) // #nosec G705 -- image bytes proxied with an image/* Content-Type only
}

// linkPreviewAuth validates the HTTP Basic credentials against Prosody. The
// username may be a bare JID or a localpart; non-local domains are rejected.
// Attempts share the portal login gate so password guessing is throttled the
// same way as the login form. On success it returns the bare JID.
func (a *App) linkPreviewAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	username, password, hasBasic := r.BasicAuth()
	ip := authlimit.ClientIP(r)

	localpart, domain, _ := xmpp.SplitJID(username)
	if localpart == "" {
		localpart, domain = domain, a.Cfg.Domain
	}
	localpart = strings.ToLower(strings.TrimSpace(localpart))

	gate := a.LoginGate
	if gate == nil {
		gate = authlimit.New()
		a.LoginGate = gate
	}

	deny := func(status int, message string) (string, bool) {
		w.Header().Set("WWW-Authenticate", `Basic realm="link-preview", charset="UTF-8"`)
		writeJSONError(w, status, message)
		return "", false
	}

	decision := gate.Allow(ip, localpart)
	if !decision.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())+1))
		return deny(http.StatusTooManyRequests, "too many attempts")
	}

	if !hasBasic || localpart == "" || password == "" ||
		len(localpart) > gate.MaxLocalpartLen() || len(password) > gate.MaxPasswordLen() ||
		!strings.EqualFold(domain, a.Cfg.Domain) {
		gate.Failure(ip, localpart)
		return deny(http.StatusUnauthorized, "invalid credentials")
	}

	jid := localpart + "@" + a.Cfg.Domain
	tokenInfo, err := a.Prosody.Login(r.Context(), jid, password)
	if err != nil {
		gate.Failure(ip, localpart)
		if errors.Is(err, prosody.ErrInvalidCredentials) || prosody.StatusOf(err) == http.StatusUnauthorized {
			return deny(http.StatusUnauthorized, "invalid credentials")
		}
		a.recordError(r, err)
		return deny(http.StatusBadGateway, "authentication backend unavailable")
	}
	gate.Success(ip, localpart)

	// The token only proved the password; it is not needed afterwards.
	if err := a.Prosody.Logout(r.Context(), tokenInfo.Token); err != nil {
		slog.Warn("link preview token revoke failed",
			slog.String("request_id", requestID(r)),
			slog.String("error", err.Error()),
		)
	}
	return jid, true
}

// linkPreviewQuota enforces the per account hourly preview limit.
func (a *App) linkPreviewQuota(w http.ResponseWriter, jid string) bool {
	if a.LinkPreviewGate == nil {
		return true
	}
	allowed, retryAfter := a.LinkPreviewGate.Allow(jid)
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return false
	}
	return true
}

// linkPreviewError maps a fetch failure onto an API status code.
func (a *App) linkPreviewError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, linkpreview.ErrInvalidURL):
		writeJSONError(w, http.StatusBadRequest, "invalid url")
	case errors.Is(err, linkpreview.ErrBlockedAddress), errors.Is(err, linkpreview.ErrNoAddress):
		writeJSONError(w, http.StatusForbidden, "url target is not allowed")
	case errors.Is(err, linkpreview.ErrTooManyRedirects),
		errors.Is(err, linkpreview.ErrNotHTML), errors.Is(err, linkpreview.ErrNotImage),
		errors.Is(err, linkpreview.ErrTooLarge):
		writeJSONError(w, http.StatusBadGateway, "target could not be previewed")
	case errors.Is(err, context.DeadlineExceeded):
		writeJSONError(w, http.StatusGatewayTimeout, "target did not respond in time")
	default:
		var upstream *linkpreview.UpstreamError
		if errors.As(err, &upstream) {
			writeJSONError(w, http.StatusBadGateway, "target could not be fetched")
			return
		}
		a.recordError(r, err)
		writeJSONError(w, http.StatusBadGateway, "target could not be fetched")
	}
}
