package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestLocalpartFromClaim(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"alice", "alice", false},
		{"Alice@Example.COM", "alice", false},
		{"alice.smith", "alice.smith", false},
		{"alice_2-b", "alice_2-b", false},
		{" alice@corp.test ", "alice", false},
		{"", "", true},
		{"@example.com", "", true},
		{"a b", "", true},
		{"alice@corp.test/extra", "", true},
		{".alice", "", true},
		{"alice.", "", true},
		{"-alice", "", true},
		{"alice_", "", true},
		{"a..b", "", true},
		{"alice/bob", "", true},
		{strings.Repeat("a", 65), "", true},
		{strings.Repeat("a", 64), strings.Repeat("a", 64), false},
	}
	for _, tc := range cases {
		got, err := LocalpartFromClaim(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("LocalpartFromClaim(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("LocalpartFromClaim(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("LocalpartFromClaim(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPKCEChallenge(t *testing.T) {
	verifier, err := PKCEVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) < 43 {
		t.Fatalf("verifier too short: %d", len(verifier))
	}
	challenge := PKCEChallenge(verifier)
	if challenge == verifier {
		t.Fatal("challenge must not equal verifier")
	}
	if got := PKCEChallenge(verifier); got != challenge {
		t.Fatal("challenge not deterministic")
	}
}

// stubIdP is a minimal OIDC provider for tests.
type stubIdP struct {
	*httptest.Server
	key *rsa.PrivateKey
	kid string
}

func newStubIdP(t *testing.T) *stubIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &stubIdP{key: key, kid: "test-key"}
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
				"kid": idp.kid,
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
			return
		}
		idp.serveToken(w, r)
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub":                "user-1",
			"preferred_username": "alice",
		})
	})
	idp.Server = httptest.NewServer(mux)
	t.Cleanup(idp.Close)
	return idp
}

// serveToken answers the token endpoint; tests override tokenClaims to shape
// the signed ID token.
var tokenClaims map[string]any

func (s *stubIdP) serveToken(w http.ResponseWriter, r *http.Request) {
	claims := jwt.MapClaims{
		"iss": s.URL,
		"aud": "client-1",
		"sub": "user-1",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range tokenClaims {
		claims[k] = v
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.kid
	raw, err := token.SignedString(s.key)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "access-token",
		"id_token":     raw,
		"token_type":   "Bearer",
	})
}

func testProvider(idp *stubIdP) *Provider {
	return New(Config{
		Issuer:        idp.URL,
		ClientID:      "client-1",
		ClientSecret:  "secret",
		RedirectURL:   "https://portal.test/auth/oidc/callback",
		UsernameClaim: "preferred_username",
		Scopes:        []string{"openid", "profile"},
	}, idp.Client())
}

func TestFlow(t *testing.T) {
	idp := newStubIdP(t)
	p := testProvider(idp)
	ctx := context.Background()

	target, err := p.AuthorizationURL(ctx, "state-1", "nonce-1", "challenge-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"response_type=code", "client_id=client-1", "state=state-1", "nonce=nonce-1", "code_challenge=challenge-1", "code_challenge_method=S256"} {
		if !strings.Contains(target, want) {
			t.Errorf("authorization url %q missing %q", target, want)
		}
	}

	tokenClaims = map[string]any{"nonce": "nonce-1", "preferred_username": "Alice"}
	defer func() { tokenClaims = nil }()

	access, rawID, err := p.Exchange(ctx, "code-1", "verifier-1")
	if err != nil {
		t.Fatal(err)
	}
	if access != "access-token" || rawID == "" {
		t.Fatalf("unexpected tokens: %q / %q", access, rawID)
	}

	claims, err := p.VerifyIDToken(ctx, rawID, "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" {
		t.Fatalf("sub = %q", claims.Subject)
	}
	localpart, err := p.Localpart(claims)
	if err != nil {
		t.Fatal(err)
	}
	if localpart != "alice" {
		t.Fatalf("localpart = %q", localpart)
	}
}

func TestVerifyIDTokenRejects(t *testing.T) {
	idp := newStubIdP(t)
	p := testProvider(idp)
	ctx := context.Background()

	sign := func(claims jwt.MapClaims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = idp.kid
		raw, err := token.SignedString(idp.key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	base := jwt.MapClaims{
		"iss":   idp.URL,
		"aud":   "client-1",
		"sub":   "user-1",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": "n",
	}

	cases := map[string]jwt.MapClaims{
		"wrong nonce":   {"iss": idp.URL, "aud": "client-1", "sub": "u", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": "other"},
		"wrong issuer":  {"iss": "https://evil.test", "aud": "client-1", "sub": "u", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": "n"},
		"wrong client":  {"iss": idp.URL, "aud": "other", "sub": "u", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": "n"},
		"expired":       {"iss": idp.URL, "aud": "client-1", "sub": "u", "iat": time.Now().Add(-time.Hour).Unix(), "exp": time.Now().Add(-time.Minute * 5).Unix(), "nonce": "n"},
		"missing sub":   {"sub": nil},
		"missing nonce": {"nonce": nil},
	}
	for name, override := range cases {
		t.Run(name, func(t *testing.T) {
			claims := jwt.MapClaims{}
			for k, v := range base {
				claims[k] = v
			}
			for k, v := range override {
				if v == nil {
					delete(claims, k)
				} else {
					claims[k] = v
				}
			}
			if _, err := p.VerifyIDToken(ctx, sign(claims), "n"); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestDiscoverIssuerMismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 "https://other.test",
			"authorization_endpoint": "https://other.test/authorize",
			"token_endpoint":         "https://other.test/token",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := New(Config{Issuer: srv.URL, ClientID: "c"}, srv.Client())
	if _, err := p.discover(context.Background()); err == nil {
		t.Fatal("expected issuer mismatch error")
	}
}

func TestExchangeRefused(t *testing.T) {
	idp := newStubIdP(t)
	p := testProvider(idp)
	if _, _, err := p.Exchange(context.Background(), "code", ""); err == nil {
		t.Fatal("expected error for missing verifier")
	}
}
