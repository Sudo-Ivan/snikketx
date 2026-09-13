package handlers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sudo-ivan/snikketx/web-portal/internal/oidc"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
)

// stubOIDCProvider is a minimal identity provider for handler tests.
type stubOIDCProvider struct {
	*httptest.Server
	key *rsa.PrivateKey
}

func newStubOIDCProvider(t *testing.T, idToken func(iss string) jwt.MapClaims) *stubOIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &stubOIDCProvider{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 idp.URL,
			"authorization_endpoint": idp.URL + "/authorize",
			"token_endpoint":         idp.URL + "/token",
			"userinfo_endpoint":      idp.URL + "/userinfo",
			"jwks_uri":               idp.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"kid": "k1",
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(65537).Bytes()),
			}},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		claims := idToken(idp.URL)
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "k1"
		raw, err := token.SignedString(key)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at",
			"id_token":     raw,
			"token_type":   "Bearer",
		})
	})
	idp.Server = httptest.NewServer(mux)
	t.Cleanup(idp.Close)
	return idp
}

func oidcTestApp(t *testing.T, idp *stubOIDCProvider) *App {
	t.Helper()
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	app.OIDC = oidc.New(oidc.Config{
		Issuer:        idp.URL,
		ClientID:      "portal-client",
		ClientSecret:  "s",
		RedirectURL:   "https://portal.test/auth/oidc/callback",
		UsernameClaim: "preferred_username",
		Scopes:        []string{"openid", "profile"},
	}, idp.Client())
	app.Service = newServiceAuth("service@example.test", "svc-pass")
	app.Prosody = &fakeProsody{
		login: func(ctx context.Context, address, password string) (*prosody.TokenInfo, error) {
			if address != "service@example.test" || password != "svc-pass" {
				t.Errorf("service login with %q", address)
			}
			return &prosody.TokenInfo{Token: "svc-tok"}, nil
		},
	}
	return app
}

// flowCookie starts an OIDC flow and returns the session cookie carrying the
// state, nonce and verifier.
func flowCookie(t *testing.T, app *App, state, nonce, verifier string) *http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	data := session.Data{}
	data[session.KeyOIDCState] = state
	data[session.KeyOIDCNonce] = nonce
	data[session.KeyOIDCVerifier] = verifier
	data[session.KeyOIDCAt] = strconv.FormatInt(time.Now().Unix(), 10)
	if err := app.Sessions.Save(recorder, data); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == session.CookieName {
			return cookie
		}
	}
	t.Fatal("no session cookie issued")
	return nil
}

func TestOIDCCallbackHappyPath(t *testing.T) {
	var registeredUser string
	idp := newStubOIDCProvider(t, func(iss string) jwt.MapClaims {
		return jwt.MapClaims{
			"iss": iss, "aud": "portal-client", "sub": "user-1",
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
			"nonce": "nonce-1", "preferred_username": "Alice",
		}
	})
	app := oidcTestApp(t, idp)
	if fp, ok := app.Prosody.(*fakeProsody); ok {
		fp.registerWithToken = func(ctx context.Context, inviteToken, username, password string) (string, error) {
			registeredUser = username
			if len(password) < 32 {
				t.Errorf("generated password too short: %d", len(password))
			}
			return username + "@example.test", nil
		}
	}
	handler := app.Routes()
	cookie := flowCookie(t, app, "state-1", "nonce-1", "verifier-1")

	req := httptest.NewRequest(http.MethodGet,
		pathOIDCCallback+"?state=state-1&code=code-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != pathUserApp {
		t.Fatalf("Location = %q, want %q", loc, pathUserApp)
	}
	if registeredUser != "alice" {
		t.Fatalf("registered user = %q, want alice", registeredUser)
	}

	// The new session cookie must be an OIDC session.
	var sessCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			sessCookie = c
		}
	}
	if sessCookie == nil {
		t.Fatal("no session cookie in callback response")
	}
	got := app.Sessions.Get(&http.Request{Header: http.Header{"Cookie": []string{sessCookie.String()}}})
	if !got.IsOIDC() || got.JID() != "alice@example.test" {
		t.Fatalf("session = OIDC:%v JID:%q", got.IsOIDC(), got.JID())
	}
}

func TestOIDCCallbackStateMismatch(t *testing.T) {
	idp := newStubOIDCProvider(t, func(string) jwt.MapClaims { return jwt.MapClaims{} })
	app := oidcTestApp(t, idp)
	handler := app.Routes()
	cookie := flowCookie(t, app, "state-1", "nonce-1", "verifier-1")

	req := httptest.NewRequest(http.MethodGet,
		pathOIDCCallback+"?state=wrong&code=code-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
}

func TestOIDCDisabled(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	app.Prosody = &fakeProsody{}
	handler := app.Routes()

	for _, path := range []string{pathOIDCLogin, pathOIDCCallback, pathUserApp} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		// login and callback are 404; the app page needs a session and
		// redirects to login when there is none.
		if path == pathUserApp {
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("%s: status %d, want 303", path, rec.Code)
			}
			continue
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d, want 404", path, rec.Code)
		}
	}
}

func TestOIDCSessionGuards(t *testing.T) {
	idp := newStubOIDCProvider(t, func(string) jwt.MapClaims { return jwt.MapClaims{} })
	app := oidcTestApp(t, idp)
	handler := app.Routes()

	// An OIDC session: JID set, no Prosody token.
	recorder := httptest.NewRecorder()
	data := session.Data{}
	data.SetOIDCAuth("alice@example.test")
	if err := app.Sessions.Save(recorder, data); err != nil {
		t.Fatal(err)
	}
	var cookie *http.Cookie
	for _, c := range recorder.Result().Cookies() {
		if c.Name == session.CookieName {
			cookie = c
		}
	}

	// Token-backed pages send the OIDC session to the app bootstrap page.
	for _, path := range []string{"/user/", "/user/passwd", "/user/profile", "/user/manage_data"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != pathUserApp {
			t.Fatalf("%s: status %d location %q, want 303 -> %s",
				path, rec.Code, rec.Header().Get("Location"), pathUserApp)
		}
	}

	// /login redirects an OIDC session instead of testing an empty token.
	req := httptest.NewRequest(http.MethodGet, pathLogin, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != pathUserApp {
		t.Fatalf("/login: status %d location %q", rec.Code, rec.Header().Get("Location"))
	}

	// The bootstrap page renders and mints an invite.
	req = httptest.NewRequest(http.MethodGet, pathUserApp, nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d, want 200\n%s", pathUserApp, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "alice@example.test") {
		t.Fatal("bootstrap page missing JID")
	}

	// Logout must not call the backend for an OIDC session.
	logoutCalled := false
	if fp, ok := app.Prosody.(*fakeProsody); ok {
		fp.logout = func(ctx context.Context, token string) error {
			logoutCalled = true
			return nil
		}
	}
	data["_csrf"] = "csrftoken"
	rec2 := httptest.NewRecorder()
	if err := app.Sessions.Save(rec2, data); err != nil {
		t.Fatal(err)
	}
	var cookie2 *http.Cookie
	for _, c := range rec2.Result().Cookies() {
		if c.Name == session.CookieName {
			cookie2 = c
		}
	}
	form := url.Values{"csrf_token": {"csrftoken"}}
	req = httptest.NewRequest(http.MethodPost, "/user/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie2)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout: status %d", rec.Code)
	}
	if logoutCalled {
		t.Fatal("Prosody.Logout was called for an OIDC session")
	}
}
