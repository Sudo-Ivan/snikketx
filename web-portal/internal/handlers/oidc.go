package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/csrf"
	"github.com/sudo-ivan/snikketx/web-portal/internal/oidc"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

const (
	// pathOIDCLogin starts the authorization code flow at the provider.
	pathOIDCLogin = "/auth/oidc/login"
	// pathOIDCCallback receives the authorization code.
	pathOIDCCallback = "/auth/oidc/callback"
	// pathUserApp is the "add this account to a chat app" page.
	pathUserApp = "/user/app"

	// oidcFlowMaxAge bounds the lifetime of the state, nonce and PKCE
	// verifier stored in the session cookie between login and callback.
	oidcFlowMaxAge = 10 * time.Minute
	// oidcInviteTTL is the lifetime in seconds of the password reset
	// invitation minted as the app bootstrap token.
	oidcInviteTTL = 86400
	// serviceTokenMaxAge keeps a cached service token comfortably below
	// the 24 hour Prosody access token lifetime.
	serviceTokenMaxAge = 12 * time.Hour
)

// errServiceNotConfigured reports that no portal service account was set.
var errServiceNotConfigured = errors.New("portal service account is not configured")

// serviceAuth caches the Prosody bearer token of the configured service
// account. Single sign-on users have no password to trade for a token, so
// account provisioning and bootstrap invitations run under this account.
type serviceAuth struct {
	mu       sync.Mutex
	address  string
	password string
	token    string
	issuedAt time.Time
}

func newServiceAuth(address, password string) *serviceAuth {
	return &serviceAuth{address: address, password: password}
}

// getToken returns a service token, logging the service account in when the
// cached token is missing, forced or near its end of life.
func (s *serviceAuth) getToken(ctx context.Context, p ProsodyClient, force bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !force && s.token != "" && time.Since(s.issuedAt) < serviceTokenMaxAge {
		return s.token, nil
	}
	info, err := p.Login(ctx, s.address, s.password)
	if err != nil {
		return "", err
	}
	s.token = info.Token
	s.issuedAt = time.Now()
	return s.token, nil
}

// serviceCall runs fn with the service token. A rejected token is refreshed
// once before the failure is reported.
func (a *App) serviceCall(ctx context.Context, fn func(token string) error) error {
	if a.Service == nil {
		return errServiceNotConfigured
	}
	token, err := a.Service.getToken(ctx, a.Prosody, false)
	if err != nil {
		return err
	}
	if err := fn(token); err != nil {
		if prosody.StatusOf(err) == http.StatusUnauthorized {
			fresh, ferr := a.Service.getToken(ctx, a.Prosody, true)
			if ferr != nil {
				return ferr
			}
			return fn(fresh)
		}
		return err
	}
	return nil
}

// mountOIDC registers the single sign-on routes. The handlers no-op to 404
// when the feature is not configured.
func (a *App) mountOIDC(mux *http.ServeMux) {
	mux.HandleFunc("GET "+pathOIDCLogin, a.handleOIDCLogin)
	mux.HandleFunc("GET "+pathOIDCCallback, a.handleOIDCCallback)
	mux.HandleFunc("GET "+pathUserApp, a.handleAppBootstrap)
}

// handleOIDCLogin starts the flow: it mints state, nonce and PKCE verifier,
// stores them in the encrypted session cookie and redirects to the provider.
func (a *App) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		a.notFound(w, r)
		return
	}
	sess := a.Sessions.Get(r)
	if sess.HasSession() {
		http.Redirect(w, r, pathUserHome, http.StatusSeeOther)
		return
	}

	state, err := oidc.PKCEVerifier()
	if err != nil {
		a.recordError(r, err)
		a.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
			"The sign-in could not be started. Try again later.")
		return
	}
	nonce, err := oidc.PKCEVerifier()
	if err != nil {
		a.recordError(r, err)
		a.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
			"The sign-in could not be started. Try again later.")
		return
	}
	verifier, err := oidc.PKCEVerifier()
	if err != nil {
		a.recordError(r, err)
		a.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
			"The sign-in could not be started. Try again later.")
		return
	}

	target, err := a.OIDC.AuthorizationURL(r.Context(), state, nonce, oidc.PKCEChallenge(verifier))
	if err != nil {
		a.recordError(r, err)
		slog.Error("oidc authorization url failed",
			slog.String("request_id", requestID(r)),
			slog.String("error", err.Error()))
		a.renderError(w, r, http.StatusBadGateway, "Identity provider unavailable",
			"The identity provider could not be reached. Try again later.")
		return
	}

	sess[session.KeyOIDCState] = state
	sess[session.KeyOIDCNonce] = nonce
	sess[session.KeyOIDCVerifier] = verifier
	sess[session.KeyOIDCAt] = strconv.FormatInt(time.Now().Unix(), 10)
	if err := a.Sessions.Save(w, sess); err != nil {
		a.recordError(r, err)
		a.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
			"The sign-in could not be started. Try again later.")
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleOIDCCallback finishes the flow: it checks the state, exchanges the
// code, verifies the ID token, makes sure the XMPP account exists and signs
// the user into the portal.
func (a *App) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		a.notFound(w, r)
		return
	}
	sess := a.Sessions.Get(r)

	fail := func(status int, title, detail, logLine string, cause error) {
		sess.ClearOIDCFlow()
		_ = a.Sessions.Save(w, sess)
		if cause != nil {
			slog.Warn("oidc sign-in failed",
				slog.String("request_id", requestID(r)),
				slog.String("reason", logLine),
				slog.String("error", cause.Error()))
		} else {
			slog.Warn("oidc sign-in failed",
				slog.String("request_id", requestID(r)),
				slog.String("reason", logLine))
		}
		a.renderError(w, r, status, title, detail)
	}

	query := r.URL.Query()
	if providerErr := query.Get("error"); providerErr != "" {
		detail := query.Get("error_description")
		slog.Warn("oidc provider refused sign-in",
			slog.String("request_id", requestID(r)),
			slog.String("provider_error", providerErr),
			slog.String("provider_error_description", detail))
		fail(http.StatusBadRequest, "Sign-in was not completed",
			"The identity provider refused the sign-in request. Start again.", "provider_error", nil)
		return
	}

	state := query.Get("state")
	code := query.Get("code")
	expected := sess[session.KeyOIDCState]
	verifier := sess[session.KeyOIDCVerifier]
	nonce := sess[session.KeyOIDCNonce]
	issuedUnix, _ := strconv.ParseInt(sess[session.KeyOIDCAt], 10, 64)
	age := time.Since(time.Unix(issuedUnix, 0))

	switch {
	case expected == "" || verifier == "" || nonce == "" || state == "" || code == "":
		fail(http.StatusBadRequest, "Sign-in could not be verified",
			"This sign-in was not started on this service, or it expired. Start again.", "missing_flow_state", nil)
		return
	case subtle.ConstantTimeCompare([]byte(state), []byte(expected)) != 1:
		fail(http.StatusBadRequest, "Sign-in could not be verified",
			"The sign-in state did not match. Start again.", "state_mismatch", nil)
		return
	case issuedUnix <= 0 || age < 0 || age > oidcFlowMaxAge:
		fail(http.StatusBadRequest, "Sign-in expired",
			"The sign-in took too long. Start again.", "flow_expired", nil)
		return
	}
	sess.ClearOIDCFlow()

	accessToken, idToken, err := a.OIDC.Exchange(r.Context(), code, verifier)
	if err != nil {
		fail(http.StatusBadGateway, "Identity provider error",
			"The identity provider did not complete the sign-in. Try again later.", "exchange", err)
		return
	}

	var claims *oidc.Claims
	if idToken != "" {
		claims, err = a.OIDC.VerifyIDToken(r.Context(), idToken, nonce)
		if err != nil {
			fail(http.StatusUnauthorized, "Sign-in could not be verified",
				"The identity token of the provider could not be verified. Start again.", "id_token", err)
			return
		}
	} else {
		claims, err = a.OIDC.Userinfo(r.Context(), accessToken)
		if err != nil {
			fail(http.StatusUnauthorized, "Sign-in could not be verified",
				"The identity provider did not return a usable identity. Try again later.", "userinfo", err)
			return
		}
	}

	localpart, err := a.OIDC.Localpart(claims)
	if err != nil {
		fail(http.StatusBadRequest, "Username not usable",
			"The username your identity provider returned cannot be used on this service. Contact the operator.", "localpart", err)
		return
	}
	jid := localpart + "@" + a.Cfg.Domain

	created, err := a.oidcEnsureAccount(r.Context(), localpart)
	if err != nil {
		if errors.Is(err, errServiceNotConfigured) {
			slog.Warn("oidc account provisioning unavailable",
				slog.String("request_id", requestID(r)),
				slog.String("reason", "service_account_not_configured"))
		} else {
			a.recordError(r, err)
			slog.Error("oidc account provisioning failed",
				slog.String("request_id", requestID(r)),
				slog.String("error", err.Error()))
			fail(http.StatusBadGateway, "Account setup failed",
				"The chat server could not prepare your account. Try again later.", "provisioning", nil)
			return
		}
	}

	sess.RotateAuthSurface()
	sess.SetOIDCAuth(jid)
	_ = csrf.Rotate(sess)
	a.recordAudit(r, sess, "auth.login", jid, "oidc")
	if created {
		sess.PushFlash("Your account was created.", "success")
	} else {
		sess.PushFlash("Login successful.", "success")
	}
	_ = a.Sessions.Save(w, sess)
	http.Redirect(w, r, pathUserApp, http.StatusSeeOther)
}

// oidcEnsureAccount creates the XMPP account of a single sign-on user when it
// does not exist yet. It reports whether the account was created. The new
// account receives a generated password that is never shown: app bootstrap
// runs through invitations instead.
func (a *App) oidcEnsureAccount(ctx context.Context, localpart string) (bool, error) {
	if a.Service == nil {
		return false, errServiceNotConfigured
	}

	var found bool
	err := a.serviceCall(ctx, func(token string) error {
		_, err := a.Prosody.GetUserByLocalpart(ctx, token, localpart)
		return err
	})
	switch {
	case err == nil:
		found = true
	case prosody.StatusOf(err) == http.StatusNotFound:
		found = false
	default:
		if apiErr, ok := errors.AsType[*prosody.APIError](err); ok &&
			(apiErr.Condition == "item-not-found" || apiErr.Condition == "user-not-found") {
			found = false
		} else {
			return false, err
		}
	}
	if found {
		return false, nil
	}

	var invite *prosody.AdminInviteInfo
	err = a.serviceCall(ctx, func(token string) error {
		var ierr error
		invite, ierr = a.Prosody.CreateAccountInvite(ctx, token, prosody.AccountInviteOptions{
			RoleNames:        []string{prosody.ScopeDefault},
			RestrictUsername: localpart,
			TTL:              600,
			Note:             "Single sign-on account setup",
		})
		return ierr
	})
	if err != nil {
		return false, err
	}

	password, err := randomPassword()
	if err != nil {
		return false, err
	}
	if _, err := a.Prosody.RegisterWithToken(ctx, invite.Token, localpart, password); err != nil {
		return false, err
	}
	return true, nil
}

// oidcBootstrapInvite mints a password reset invitation whose token doubles
// as the preauth credential the chat app uses for in-band registration. The
// app picks the account password itself when it redeems the token, so the
// portal never has to display one.
func (a *App) oidcBootstrapInvite(ctx context.Context, localpart, jid string) (uri, landing string, err error) {
	if a.Service == nil {
		return "", "", errServiceNotConfigured
	}
	var invite *prosody.AdminInviteInfo
	err = a.serviceCall(ctx, func(token string) error {
		var ierr error
		invite, ierr = a.Prosody.CreatePasswordResetInvite(ctx, token, localpart, oidcInviteTTL)
		return ierr
	})
	if err != nil {
		return "", "", err
	}
	if invite == nil || invite.Token == "" {
		return "", "", errors.New("prosody: bootstrap invite carried no token")
	}

	uri = invite.XMPPURI
	if uri == "" {
		uri = "xmpp:" + jid + "?register;preauth=" + url.QueryEscape(invite.Token)
	}
	landing = invite.LandingPage
	if landing == "" {
		landing = "https://" + a.Cfg.Domain + "/invite/" + url.PathEscape(invite.Token) + "/"
	}
	return uri, landing, nil
}

// appBootstrapPage is the data behind the "add to app" page.
type appBootstrapPage struct {
	webui.PageData
	JID      string
	XMPPURI  string
	ResetURL string
	Note     string
}

// handleAppBootstrap mints a fresh bootstrap invitation and shows the page
// that hands it to a chat app. Only single sign-on accounts use it: password
// accounts can sign into apps directly.
func (a *App) handleAppBootstrap(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	if !sess.IsOIDC() {
		a.flashRedirect(w, r, sess, "Your devices can sign in with your account password.", "info", pathUserHome)
		return
	}

	jid := sess.JID()
	localpart, _, _ := xmpp.SplitJID(jid)
	data := appBootstrapPage{JID: jid}

	uri, landing, err := a.oidcBootstrapInvite(r.Context(), localpart, jid)
	switch {
	case errors.Is(err, errServiceNotConfigured):
		data.Note = "Automatic app setup is not configured on this service. Ask the operator to set up a portal service account."
	case err != nil:
		a.recordError(r, err)
		data.Note = "The app link could not be created. Try again later."
	default:
		data.XMPPURI = uri
		data.ResetURL = landing
	}

	page := a.newPage(w, r, sess, "Connect an app", "app", webui.ShellApp)
	data.PageData = page
	a.render(w, r, http.StatusOK, "oidc_app.html", data)
}

// randomPassword returns a generated password with well over 32 characters.
func randomPassword() (string, error) {
	buf := make([]byte, 33)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
