package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pquerna/otp/totp"

	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/credstore"
	"github.com/sudo-ivan/snikketx/web-portal/internal/csrf"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

const (
	// pathLoginVerify is the second-factor step of the sign-in flow.
	pathLoginVerify = "/login/verify"
	// pathUserSecurity is the account page managing passkeys and TOTP.
	pathUserSecurity = "/user/security"
	// pendingMaxAge bounds how long a password grant token may sit in the
	// pending state waiting for the second factor.
	pendingMaxAge = 5 * time.Minute
	// maxPasskeyName bounds the friendly label stored per credential.
	maxPasskeyName = 60
)

// mountSecurity registers the second-factor routes of the sign-in flow and
// the account security settings.
func (a *App) mountSecurity(mux *http.ServeMux) {
	mux.HandleFunc("GET "+pathLoginVerify, a.handleVerifyForm)
	mux.HandleFunc("POST "+pathLoginVerify, a.handleVerifyTOTP)
	mux.HandleFunc("POST "+pathLoginVerify+"/passkey/begin", a.handleVerifyPasskeyBegin)
	mux.HandleFunc("POST "+pathLoginVerify+"/passkey/finish", a.handleVerifyPasskeyFinish)

	mux.HandleFunc("GET "+pathUserSecurity, a.handleSecurityForm)
	mux.HandleFunc("POST "+pathUserSecurity+"/passkey/begin", a.handlePasskeyRegisterBegin)
	mux.HandleFunc("POST "+pathUserSecurity+"/passkey/finish", a.handlePasskeyRegisterFinish)
	mux.HandleFunc("POST "+pathUserSecurity+"/passkey/delete", a.handlePasskeyDelete)
	mux.HandleFunc("POST "+pathUserSecurity+"/totp/setup", a.handleTOTPSetup)
	mux.HandleFunc("POST "+pathUserSecurity+"/totp/confirm", a.handleTOTPConfirm)
	mux.HandleFunc("POST "+pathUserSecurity+"/totp/cancel", a.handleTOTPCancel)
	mux.HandleFunc("POST "+pathUserSecurity+"/totp/disable", a.handleTOTPDisable)
}

// requiresSecondFactor reports whether the account enrolled a passkey or TOTP
// and therefore owes a second-factor step after the password grant.
func (a *App) requiresSecondFactor(jid string) bool {
	if a.Credentials == nil {
		return false
	}
	return a.Credentials.Get(jid).NeedsSecondFactor()
}

// requirePending returns the session when it holds a fresh pending token from
// the password grant. Anything else is sent back to the login page.
func (a *App) requirePending(w http.ResponseWriter, r *http.Request) (session.Data, bool) {
	sess := a.Sessions.Get(r)
	if sess.HasSession() {
		http.Redirect(w, r, pathUserHome, http.StatusSeeOther)
		return nil, false
	}
	age := sess.PendingAge()
	if !sess.HasPending() || age < 0 || age > pendingMaxAge {
		sess.ClearPending()
		sess.PushFlash("Sign-in expired, please start again.", "alert")
		_ = a.Sessions.Save(w, sess)
		a.redirectToLogin(w, r)
		return nil, false
	}
	return sess, true
}

// completePendingLogin promotes a verified pending session into a full one,
// identical to what a password-only login produces.
func (a *App) completePendingLogin(w http.ResponseWriter, r *http.Request, sess session.Data, jid, factor string) {
	if !sess.PromotePending() {
		a.redirectToLogin(w, r)
		return
	}
	sess.RotateAuthSurface()
	_ = csrf.Rotate(sess)
	a.recordAudit(r, sess, "auth.login", jid, factor)
	a.flashRedirect(w, r, sess, "Login successful.", "success", pathUserHome)
}

// webAuthnFor builds the relying party for the current request. The rpID and
// origin derive from the request host so the portal works under whichever
// hostname the proxy exposes; the configured domain is the fallback. A
// mismatched host simply fails verification, so a forged Host header cannot
// widen anything.
func (a *App) webAuthnFor(r *http.Request) (*webauthn.WebAuthn, error) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		host = a.Cfg.Domain
	}

	scheme := "https"
	if r.TLS == nil {
		switch proto := r.Header.Get("X-Forwarded-Proto"); proto {
		case "http", "https":
			scheme = proto
		default:
			if a.Cfg.InsecureCookies {
				scheme = "http"
			}
		}
	}

	return webauthn.New(&webauthn.Config{
		RPDisplayName:         a.Cfg.SiteName,
		RPID:                  host,
		RPOrigins:             []string{scheme + "://" + r.Host},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationPreferred,
		},
	})
}

// webAuthnUser adapts a credstore account to the webauthn.User interface.
type webAuthnUser struct {
	jid  string
	acct *credstore.Account
}

func (u webAuthnUser) WebAuthnID() []byte   { return u.acct.UserID }
func (u webAuthnUser) WebAuthnName() string { return u.jid }
func (u webAuthnUser) WebAuthnDisplayName() string {
	localpart, _, _ := xmpp.SplitJID(u.jid)
	if localpart == "" {
		return u.jid
	}
	return localpart
}

func (u webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	creds := make([]webauthn.Credential, 0, len(u.acct.Passkeys))
	for _, pk := range u.acct.Passkeys {
		creds = append(creds, pk.Credential)
	}
	return creds
}

// passkeyUserFor loads the account state and guarantees a user handle exists.
func (a *App) passkeyUserFor(jid string) (webAuthnUser, error) {
	if _, err := a.Credentials.EnsureUserID(jid); err != nil {
		return webAuthnUser{}, err
	}
	acct := a.Credentials.Get(jid)
	if acct == nil {
		return webAuthnUser{}, errors.New("credstore: account missing after ensure")
	}
	return webAuthnUser{jid: jid, acct: acct}, nil
}

// storeCeremony keeps the WebAuthn session data in the encrypted session
// cookie until the finish call arrives.
func (a *App) storeCeremony(w http.ResponseWriter, sess session.Data, data *webauthn.SessionData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	sess[session.KeyWebAuthn] = string(raw)
	return a.Sessions.Save(w, sess)
}

// takeCeremony returns and clears the stored WebAuthn session data.
func (a *App) takeCeremony(sess session.Data) (webauthn.SessionData, bool) {
	raw := sess[session.KeyWebAuthn]
	delete(sess, session.KeyWebAuthn)
	if raw == "" {
		return webauthn.SessionData{}, false
	}
	var data webauthn.SessionData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return webauthn.SessionData{}, false
	}
	return data, true
}

// writeJSON emits a JSON response body.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeJSONError reports a failed JSON call.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// passkeyView is one credential row of the security page.
type passkeyView struct {
	ID        string
	Name      string
	CreatedAt time.Time
	LastUsed  time.Time
}

// securityPage is the data behind the account security settings.
type securityPage struct {
	webui.PageData
	Passkeys      []passkeyView
	TOTPEnabled   bool
	TOTPCreatedAt time.Time
	TOTPSetup     bool
	TOTPSecret    string
	TOTPURI       string
}

// totpURI renders the otpauth:// URL the QR code encodes.
func totpURI(issuer, account, secret string) string {
	label := issuer + ":" + account
	return "otpauth://totp/" + url.PathEscape(label) +
		"?secret=" + url.QueryEscape(secret) +
		"&issuer=" + url.QueryEscape(issuer) +
		"&algorithm=SHA1&digits=6&period=30"
}

// securityAccount returns the stored second-factor state for the session
// account, tolerating a missing store.
func (a *App) securityAccount(sess session.Data) *credstore.Account {
	if a.Credentials == nil {
		return nil
	}
	return a.Credentials.Get(sess.JID())
}

// loadSecurityPage assembles the security settings page data.
func (a *App) loadSecurityPage(w http.ResponseWriter, r *http.Request, sess session.Data) securityPage {
	acct := a.securityAccount(sess)
	page := a.newPage(w, r, sess, "Security", "security", webui.ShellApp)
	data := securityPage{PageData: page}
	if acct != nil {
		data.TOTPEnabled = acct.TOTPEnabled
		data.TOTPCreatedAt = acct.TOTPCreatedAt
		for _, pk := range acct.Passkeys {
			data.Passkeys = append(data.Passkeys, passkeyView{
				ID:        base64.RawURLEncoding.EncodeToString(pk.ID),
				Name:      pk.Name,
				CreatedAt: pk.CreatedAt,
				LastUsed:  pk.LastUsedAt,
			})
		}
	}
	if pending := sess[session.KeyTOTPSetup]; pending != "" {
		data.TOTPSetup = true
		data.TOTPSecret = pending
		data.TOTPURI = totpURI(a.Cfg.SiteName, sess.JID(), pending)
	}
	return data
}

// handleSecurityForm shows the passkey and TOTP settings.
func (a *App) handleSecurityForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	a.render(w, r, http.StatusOK, "user_security.html", a.loadSecurityPage(w, r, sess))
}

// handlePasskeyRegisterBegin starts a WebAuthn registration ceremony for the
// signed-in account and answers with the creation options JSON.
func (a *App) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSessionJSON(w, r)
	if !ok {
		return
	}
	if a.Credentials == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Credential storage is unavailable.")
		return
	}

	user, err := a.passkeyUserFor(sess.JID())
	if err != nil {
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "Passkey setup failed.")
		return
	}

	wa, err := a.webAuthnFor(r)
	if err != nil {
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "Passkey setup failed.")
		return
	}

	creation, ceremony, err := wa.BeginRegistration(user)
	if err != nil {
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "Passkey setup failed.")
		return
	}
	if err := a.storeCeremony(w, sess, ceremony); err != nil {
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "Passkey setup failed.")
		return
	}
	writeJSON(w, http.StatusOK, creation)
}

// handlePasskeyRegisterFinish verifies the attestation and stores the new
// credential. The friendly name travels in the query string because the
// request body carries the attestation document itself.
func (a *App) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSessionJSON(w, r)
	if !ok {
		return
	}
	if a.Credentials == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Credential storage is unavailable.")
		return
	}

	user, err := a.passkeyUserFor(sess.JID())
	if err != nil {
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "Passkey setup failed.")
		return
	}
	ceremony, ok := a.takeCeremony(sess)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "The passkey ceremony expired. Start again.")
		return
	}
	_ = a.Sessions.Save(w, sess)

	wa, err := a.webAuthnFor(r)
	if err != nil {
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "Passkey setup failed.")
		return
	}

	cred, err := wa.FinishRegistration(user, ceremony, r)
	if err != nil {
		slog.Debug("passkey registration rejected",
			slog.String("request_id", requestID(r)),
			slog.String("error", err.Error()))
		writeJSONError(w, http.StatusBadRequest, "The passkey was rejected. Try again.")
		return
	}

	name := strings.TrimSpace(r.URL.Query().Get(fieldName))
	if len(name) > maxPasskeyName {
		name = name[:maxPasskeyName]
	}
	if err := a.Credentials.AddPasskey(sess.JID(), name, *cred); err != nil {
		if errors.Is(err, credstore.ErrTooManyPasskeys) {
			writeJSONError(w, http.StatusBadRequest, "The passkey limit for this account is reached.")
			return
		}
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "The passkey could not be stored.")
		return
	}
	a.recordAudit(r, sess, "security.passkey.add", sess.JID(), name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePasskeyDelete removes one registered credential.
func (a *App) handlePasskeyDelete(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	if a.Credentials == nil {
		a.flashRedirect(w, r, sess, "Credential storage is unavailable.", "alert", pathUserSecurity)
		return
	}

	id, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(r.FormValue("credential_id")))
	if err != nil || len(id) == 0 {
		a.flashRedirect(w, r, sess, "Unknown passkey.", "alert", pathUserSecurity)
		return
	}
	removed, err := a.Credentials.RemovePasskey(sess.JID(), id)
	if err != nil {
		a.recordError(r, err)
		a.flashRedirect(w, r, sess, "The passkey could not be removed.", "alert", pathUserSecurity)
		return
	}
	if !removed {
		a.flashRedirect(w, r, sess, "Unknown passkey.", "alert", pathUserSecurity)
		return
	}
	a.recordAudit(r, sess, "security.passkey.remove", sess.JID(), "")
	a.flashRedirect(w, r, sess, "Passkey removed.", "success", pathUserSecurity)
}

// handleTOTPSetup generates a fresh pending TOTP secret. The previous secret
// stays active until the new one is confirmed, so regeneration doubles as the
// re-enrollment path.
func (a *App) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      a.Cfg.SiteName,
		AccountName: sess.JID(),
		Period:      30,
		SecretSize:  20,
		Digits:      6,
	})
	if err != nil {
		a.recordError(r, err)
		a.flashRedirect(w, r, sess, "Could not generate an authenticator secret.", "alert", pathUserSecurity)
		return
	}
	sess[session.KeyTOTPSetup] = key.Secret()
	a.flashRedirect(w, r, sess, "Scan the code and confirm with a one-time code.", "info", pathUserSecurity)
}

// handleTOTPConfirm activates the pending secret after the first valid code.
func (a *App) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	pending := sess[session.KeyTOTPSetup]
	if pending == "" {
		a.flashRedirect(w, r, sess, "No authenticator setup is in progress.", "alert", pathUserSecurity)
		return
	}

	fail := func(message string) {
		page := a.loadSecurityPage(w, r, sess)
		page.AddError("%s", message)
		a.render(w, r, http.StatusBadRequest, "user_security.html", page)
	}

	code := normalizeTOTPCode(r.FormValue("code"))
	if !totp.Validate(code, pending) {
		fail("That code did not match. Check the time on your device and try again.")
		return
	}
	if a.Credentials == nil {
		fail("Credential storage is unavailable.")
		return
	}
	if err := a.Credentials.SetTOTP(sess.JID(), pending); err != nil {
		a.recordError(r, err)
		fail("The authenticator secret could not be stored.")
		return
	}
	delete(sess, session.KeyTOTPSetup)
	a.recordAudit(r, sess, "security.totp.enable", sess.JID(), "")
	a.flashRedirect(w, r, sess, "Authenticator app enabled.", "success", pathUserSecurity)
}

// handleTOTPCancel abandons a pending enrollment.
func (a *App) handleTOTPCancel(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	delete(sess, session.KeyTOTPSetup)
	a.flashRedirect(w, r, sess, "Authenticator setup cancelled.", "info", pathUserSecurity)
}

// handleTOTPDisable turns TOTP off after the account password is confirmed.
func (a *App) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}

	fail := func(message string) {
		page := a.loadSecurityPage(w, r, sess)
		page.AddError("%s", message)
		a.render(w, r, http.StatusBadRequest, "user_security.html", page)
	}

	password := r.FormValue(fieldPassword)
	if password == "" {
		fail("Enter your current password to disable the authenticator app.")
		return
	}
	if _, err := a.Prosody.Login(r.Context(), sess.JID(), password); err != nil {
		fail(msgIncorrectPassword)
		return
	}
	if a.Credentials == nil {
		fail("Credential storage is unavailable.")
		return
	}
	if err := a.Credentials.ClearTOTP(sess.JID()); err != nil {
		a.recordError(r, err)
		fail("The authenticator could not be disabled.")
		return
	}
	delete(sess, session.KeyTOTPSetup)
	a.recordAudit(r, sess, "security.totp.disable", sess.JID(), "")
	a.flashRedirect(w, r, sess, "Authenticator app disabled.", "success", pathUserSecurity)
}

// normalizeTOTPCode strips the spacing authenticator inputs often allow.
func normalizeTOTPCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' {
			return -1
		}
		return r
	}, strings.TrimSpace(code))
}

// verifyPage is the data behind the second-factor step of sign-in.
type verifyPage struct {
	webui.PageData
	Address     string
	HasTOTP     bool
	HasPasskeys bool
}

// loadVerifyPage builds the second-factor page data from the pending account.
func (a *App) loadVerifyPage(w http.ResponseWriter, r *http.Request, sess session.Data) verifyPage {
	jid := sess.PendingJID()
	page := a.newPage(w, r, sess, "Two-factor verification", "", webui.ShellBare)
	data := verifyPage{PageData: page, Address: jid}
	if a.Credentials != nil {
		if acct := a.Credentials.Get(jid); acct != nil {
			data.HasTOTP = acct.TOTPEnabled
			data.HasPasskeys = len(acct.Passkeys) > 0
		}
	}
	return data
}

// handleVerifyForm shows the second-factor step after a successful password.
func (a *App) handleVerifyForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requirePending(w, r)
	if !ok {
		return
	}
	data := a.loadVerifyPage(w, r, sess)
	if !data.HasTOTP && !data.HasPasskeys {
		// The account factors were removed between password auth and now;
		// a completed password grant is enough on its own.
		a.completePendingLogin(w, r, sess, sess.PendingJID(), "password")
		return
	}
	a.render(w, r, http.StatusOK, "login_verify.html", data)
}

// handleVerifyTOTP checks the submitted authenticator code against the
// pending account and finishes the sign-in.
func (a *App) handleVerifyTOTP(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requirePending(w, r)
	if !ok {
		return
	}
	jid := sess.PendingJID()
	localpart, _, _ := xmpp.SplitJID(jid)
	ip := authlimit.ClientIP(r)
	gate := a.LoginGate

	fail := func(message string, status int) {
		page := a.loadVerifyPage(w, r, sess)
		page.AddError("%s", message)
		a.render(w, r, status, "login_verify.html", page)
	}

	if gate != nil {
		if decision := gate.Allow(ip, localpart); !decision.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())+1))
			fail("Too many attempts. Try again later.", http.StatusTooManyRequests)
			return
		}
	}

	code := normalizeTOTPCode(r.FormValue("code"))
	if a.Credentials == nil {
		fail("Credential storage is unavailable.", http.StatusServiceUnavailable)
		return
	}
	secret, enabled, err := a.Credentials.TOTPSecret(jid)
	if err != nil {
		a.recordError(r, err)
		fail("Verification failed. Try again.", http.StatusInternalServerError)
		return
	}
	if !enabled || !totp.Validate(code, secret) {
		if gate != nil {
			gate.Failure(ip, localpart)
		}
		a.recordAudit(r, sess, "auth.second_factor_failed", jid, "totp")
		fail("That code did not match. Try again.", http.StatusUnauthorized)
		return
	}

	if gate != nil {
		gate.Success(ip, localpart)
	}
	a.completePendingLogin(w, r, sess, jid, "totp")
}

// handleVerifyPasskeyBegin starts the assertion ceremony for the pending
// account and returns the request options JSON.
func (a *App) handleVerifyPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.Get(r)
	if sess.HasSession() || !sess.HasPending() || sess.PendingAge() < 0 || sess.PendingAge() > pendingMaxAge {
		writeJSONError(w, http.StatusUnauthorized, "The sign-in expired. Start again.")
		return
	}
	jid := sess.PendingJID()

	if a.Credentials == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Credential storage is unavailable.")
		return
	}
	acct := a.Credentials.Get(jid)
	if acct == nil || len(acct.Passkeys) == 0 {
		writeJSONError(w, http.StatusBadRequest, "No passkey is registered for this account.")
		return
	}

	wa, err := a.webAuthnFor(r)
	if err != nil {
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "Passkey sign-in failed.")
		return
	}

	assertion, ceremony, err := wa.BeginLogin(webAuthnUser{jid: jid, acct: acct})
	if err != nil {
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "Passkey sign-in failed.")
		return
	}
	if err := a.storeCeremony(w, sess, ceremony); err != nil {
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "Passkey sign-in failed.")
		return
	}
	writeJSON(w, http.StatusOK, assertion)
}

// handleVerifyPasskeyFinish validates the assertion and finishes the sign-in.
// A passkey assertion with user verification is phishing-resistant, so it
// satisfies the second factor even when the account also enrolled TOTP.
func (a *App) handleVerifyPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.Get(r)
	if sess.HasSession() || !sess.HasPending() || sess.PendingAge() < 0 || sess.PendingAge() > pendingMaxAge {
		writeJSONError(w, http.StatusUnauthorized, "The sign-in expired. Start again.")
		return
	}
	jid := sess.PendingJID()
	localpart, _, _ := xmpp.SplitJID(jid)
	ip := authlimit.ClientIP(r)
	gate := a.LoginGate

	if gate != nil {
		if decision := gate.Allow(ip, localpart); !decision.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())+1))
			writeJSONError(w, http.StatusTooManyRequests, "Too many attempts. Try again later.")
			return
		}
	}

	fail := func(status int, message string) {
		if gate != nil {
			gate.Failure(ip, localpart)
		}
		a.recordAudit(r, sess, "auth.second_factor_failed", jid, "passkey")
		writeJSONError(w, status, message)
	}

	if a.Credentials == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Credential storage is unavailable.")
		return
	}
	acct := a.Credentials.Get(jid)
	if acct == nil || len(acct.Passkeys) == 0 {
		fail(http.StatusBadRequest, "No passkey is registered for this account.")
		return
	}
	ceremony, ok := a.takeCeremony(sess)
	if !ok {
		fail(http.StatusBadRequest, "The passkey ceremony expired. Start again.")
		return
	}
	_ = a.Sessions.Save(w, sess)

	wa, err := a.webAuthnFor(r)
	if err != nil {
		a.recordError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "Passkey sign-in failed.")
		return
	}

	cred, err := wa.FinishLogin(webAuthnUser{jid: jid, acct: acct}, ceremony, r)
	if err != nil {
		slog.Debug("passkey assertion rejected",
			slog.String("request_id", requestID(r)),
			slog.String("error", err.Error()))
		fail(http.StatusUnauthorized, "The passkey was rejected. Try again.")
		return
	}

	// Persist the credential record the library returned so the authenticator
	// sign counter keeps tracking clones.
	if err := a.Credentials.UpdatePasskey(jid, *cred); err != nil {
		a.recordError(r, err)
	}
	if gate != nil {
		gate.Success(ip, localpart)
	}

	if !sess.PromotePending() {
		writeJSONError(w, http.StatusUnauthorized, "The sign-in expired. Start again.")
		return
	}
	sess.RotateAuthSurface()
	_ = csrf.Rotate(sess)
	sess.PushFlash("Login successful.", "success")
	_ = a.Sessions.Save(w, sess)
	a.recordAudit(r, sess, "auth.login", jid, "passkey")
	writeJSON(w, http.StatusOK, map[string]string{"redirect": pathUserHome})
}

// requireSessionJSON is requireSession for the JSON ceremony endpoints: it
// answers with a status instead of a redirect so the browser side can report
// the failure.
func (a *App) requireSessionJSON(w http.ResponseWriter, r *http.Request) (session.Data, bool) {
	sess := a.Sessions.Get(r)
	if !sess.HasSession() {
		writeJSONError(w, http.StatusUnauthorized, "Sign in required.")
		return nil, false
	}
	return sess, true
}
