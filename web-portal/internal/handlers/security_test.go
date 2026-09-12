package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
)

// sessionCookie extracts the session cookie from a response.
func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == session.CookieName {
			return cookie
		}
	}
	t.Fatal("no session cookie issued")
	return nil
}

// pendingCookie builds a session that holds a pending token waiting for the
// second factor, like a successful password grant produces.
func pendingCookie(t *testing.T, app *App, jid string) *http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	data := session.Data{}
	data.SetPendingAuth("tok", "prosody:registered prosody:admin", jid, time.Now())
	data["_csrf"] = "csrftoken"
	if err := app.Sessions.Save(recorder, data); err != nil {
		t.Fatal(err)
	}
	return sessionCookie(t, recorder)
}

// loginWithPassword posts the login form and returns the response and the
// resulting session cookie.
func loginWithPassword(t *testing.T, handler http.Handler, cookie *http.Cookie) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	body := "address=alice&password=secretsecret&csrf_token=csrftoken"
	req := httptest.NewRequest(http.MethodPost, pathLogin, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, sessionCookie(t, rec)
}

func serve(handler http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func sessionOf(app *App, cookie *http.Cookie) session.Data {
	return app.Sessions.Get(&http.Request{Header: http.Header{"Cookie": []string{cookie.String()}}})
}

func TestLoginWithTOTPRequiresSecondFactor(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()

	const secret = "JBSWY3DPEHPK3PXP"
	if err := app.Credentials.SetTOTP("alice@example.test", secret); err != nil {
		t.Fatal(err)
	}

	rec, _ := loginWithPassword(t, handler, adminCookie(t, app))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want redirect", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != pathLoginVerify {
		t.Fatalf("Location = %q, want %q", loc, pathLoginVerify)
	}
}

func TestPendingSessionIsNotAuthenticated(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	if err := app.Credentials.SetTOTP("alice@example.test", "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}
	cookie := pendingCookie(t, app, "alice@example.test")

	rec := serve(handler, cookie, http.MethodGet, pathUserHome, "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != pathLogin {
		t.Fatalf("pending session reached /user/: status %d loc %q", rec.Code, rec.Header().Get("Location"))
	}

	// The verification page renders for the pending session.
	rec = serve(handler, cookie, http.MethodGet, pathLoginVerify, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify page status %d", rec.Code)
	}
}

func TestVerifyTOTPPromotesSession(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()

	const secret = "JBSWY3DPEHPK3PXP"
	if err := app.Credentials.SetTOTP("alice@example.test", secret); err != nil {
		t.Fatal(err)
	}
	cookie := pendingCookie(t, app, "alice@example.test")

	// A wrong code is rejected and keeps the pending state.
	bad := "000000"
	if c, err := totp.GenerateCode(secret, time.Now()); err == nil && c == bad {
		bad = "111111"
	}
	rec := serve(handler, cookie, http.MethodPost, pathLoginVerify, "code="+bad+"&csrf_token=csrftoken")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad code status %d, want 401", rec.Code)
	}
	if sessionOf(app, sessionCookie(t, rec)).HasSession() {
		t.Fatal("rejected code produced a session")
	}

	// The current code promotes the session.
	good, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec = serve(handler, cookie, http.MethodPost, pathLoginVerify, "code="+good+"&csrf_token=csrftoken")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != pathUserHome {
		t.Fatalf("verify status %d loc %q", rec.Code, rec.Header().Get("Location"))
	}
	sess := sessionOf(app, sessionCookie(t, rec))
	if !sess.HasSession() || sess.JID() != "alice@example.test" {
		t.Fatalf("promoted session missing auth: %v", sess)
	}
	if sess.HasPending() {
		t.Fatal("pending state survived promotion")
	}
}

func TestVerifyFormWithoutPendingRedirects(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()

	rec := serve(handler, nil, http.MethodGet, pathLoginVerify, "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != pathLogin {
		t.Fatalf("status %d loc %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestLoginWithoutSecondFactorUnchanged(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()

	rec, cookie := loginWithPassword(t, handler, adminCookie(t, app))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != pathUserHome {
		t.Fatalf("status %d loc %q", rec.Code, rec.Header().Get("Location"))
	}
	if !sessionOf(app, cookie).HasSession() {
		t.Fatal("login did not create a session")
	}
}

func TestSecurityPageRenders(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	rec := serve(handler, cookie, http.MethodGet, pathUserSecurity, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Passkeys", "Authenticator app", "data-passkey-register"} {
		if !strings.Contains(body, want) {
			t.Fatalf("security page missing %q", want)
		}
	}
}

func TestTOTPEnrollFlow(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	// Start enrollment: the pending secret lands in the session.
	rec := serve(handler, cookie, http.MethodPost, pathUserSecurity+"/totp/setup", "csrf_token=csrftoken")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup status %d", rec.Code)
	}
	cookie = sessionCookie(t, rec)

	pending := sessionOf(app, cookie)[session.KeyTOTPSetup]
	if pending == "" {
		t.Fatal("no pending TOTP secret in session")
	}

	// The security page shows the QR while a setup is pending.
	rec = serve(handler, cookie, http.MethodGet, pathUserSecurity, "")
	if !strings.Contains(rec.Body.String(), "data-qrdata=") || !strings.Contains(rec.Body.String(), pending) {
		t.Fatal("setup page missing QR or secret")
	}

	// A wrong code keeps the enrollment pending.
	rec = serve(handler, cookie, http.MethodPost, pathUserSecurity+"/totp/confirm", "code=000000&csrf_token=csrftoken")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad confirm status %d", rec.Code)
	}
	if _, enabled, _ := app.Credentials.TOTPSecret("alice@example.test"); enabled {
		t.Fatal("TOTP enabled by a wrong code")
	}

	// Confirm with a valid code.
	code, err := totp.GenerateCode(pending, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec = serve(handler, cookie, http.MethodPost, pathUserSecurity+"/totp/confirm", "code="+code+"&csrf_token=csrftoken")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("confirm status %d\n%s", rec.Code, rec.Body.String())
	}
	secret, enabled, err := app.Credentials.TOTPSecret("alice@example.test")
	if err != nil || !enabled || secret != pending {
		t.Fatalf("stored secret=%q enabled=%v err=%v", secret, enabled, err)
	}

	// Disabling requires the account password.
	rec = serve(handler, cookie, http.MethodPost, pathUserSecurity+"/totp/disable", "password=secretsecret&csrf_token=csrftoken")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("disable status %d\n%s", rec.Code, rec.Body.String())
	}
	if _, enabled, _ := app.Credentials.TOTPSecret("alice@example.test"); enabled {
		t.Fatal("TOTP still enabled after disable")
	}
}

func TestTOTPDisableRejectsWrongPassword(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)

	// The stub token endpoint accepts anything, so point the app at a
	// backend that rejects the password grant.
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"cid","client_secret":"secret"}`))
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	rejecting := httptest.NewServer(mux)
	t.Cleanup(rejecting.Close)
	app.Prosody = prosody.New(rejecting.URL, "example.test", "test")

	if err := app.Credentials.SetTOTP("alice@example.test", "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}
	handler := app.Routes()
	cookie := adminCookie(t, app)

	rec := serve(handler, cookie, http.MethodPost, pathUserSecurity+"/totp/disable", "password=wrong&csrf_token=csrftoken")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disable status %d, want 400", rec.Code)
	}
	if _, enabled, _ := app.Credentials.TOTPSecret("alice@example.test"); !enabled {
		t.Fatal("TOTP disabled despite a wrong password")
	}
}

func TestPasskeyRegisterBeginRequiresSession(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()

	req := httptest.NewRequest(http.MethodPost, pathUserSecurity+"/passkey/begin", nil)
	req.Header.Set("X-CSRF-Token", "csrftoken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// Without a session cookie there is no CSRF token to compare, so the
	// middleware rejects with 403 before the handler runs.
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}

func TestPasskeyRegisterBeginReturnsOptions(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	req := httptest.NewRequest(http.MethodPost, pathUserSecurity+"/passkey/begin", nil)
	req.Header.Set("X-CSRF-Token", "csrftoken")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d\n%s", rec.Code, rec.Body.String())
	}

	var options struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"rp"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	if options.PublicKey.Challenge == "" || options.PublicKey.User.ID == "" {
		t.Fatal("creation options incomplete")
	}
	if options.PublicKey.RP.ID != "example.com" {
		t.Fatalf("rpId = %q, want request host", options.PublicKey.RP.ID)
	}
	if options.PublicKey.RP.Name != "Example Chat" {
		t.Fatalf("rp name = %q", options.PublicKey.RP.Name)
	}

	// The ceremony data is parked in the session cookie.
	if sessionOf(app, sessionCookie(t, rec))[session.KeyWebAuthn] == "" {
		t.Fatal("ceremony data missing from session")
	}
}

func TestVerifyPasskeyBeginWithoutPending(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()

	// A full session without pending state gets a JSON 401.
	cookie := adminCookie(t, app)
	req := httptest.NewRequest(http.MethodPost, pathLoginVerify+"/passkey/begin", nil)
	req.Header.Set("X-CSRF-Token", "csrftoken")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}
