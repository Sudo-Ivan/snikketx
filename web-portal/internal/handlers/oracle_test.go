package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sudo-ivan/snikketx/web-portal/internal/csrf"
)

// TestLoginErrorOracle checks that domain mismatch and empty credentials both
// surface the same user-visible failure class without leaking which check failed
// for domain mistakes.
func TestLoginErrorOracle(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	cookie := bareCSRFCookie(t, app)

	cases := []struct {
		name string
		body string
	}{
		{"wrong-domain", "address=alice@evil.example&password=secretsecret&" + csrf.FieldName + "=csrftoken"},
		{"oversize", "address=alice&password=" + strings.Repeat("x", app.LoginGate.MaxPasswordLen()+8) + "&" + csrf.FieldName + "=csrftoken"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status %d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), errCredentials) {
				t.Fatalf("missing uniform error: %s", rec.Body.String())
			}
			if strings.Contains(strings.ToLower(rec.Body.String()), "does not exist") ||
				strings.Contains(strings.ToLower(rec.Body.String()), "unknown user") {
				t.Fatal("response leaked account existence wording")
			}
		})
	}
}
