package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/csrf"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
)

func TestLoginRejectsWrongDomain(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()

	cookie := bareCSRFCookie(t, app)
	body := "address=alice@evil.example&password=secretsecret&" + csrf.FieldName + "=csrftoken"
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), errCredentials) {
		t.Fatalf("expected uniform credentials error, got %s", rec.Body.String())
	}
}

func TestLoginRejectsOversizedPassword(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	cookie := bareCSRFCookie(t, app)
	pw := strings.Repeat("x", app.LoginGate.MaxPasswordLen()+1)
	body := "address=alice&password=" + pw + "&" + csrf.FieldName + "=csrftoken"
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	app.LoginGate = authlimit.New()
	handler := app.Routes()

	// Force lockout without waiting on min latency by using Failure path via empty password.
	cookie := bareCSRFCookie(t, app)
	for i := range 10 {
		body := "address=alice&password=&" + csrf.FieldName + "=csrftoken"
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "198.51.100.50:4444"
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		start := time.Now()
		handler.ServeHTTP(rec, req)
		if i == 0 && time.Since(start) < 200*time.Millisecond {
			t.Fatalf("expected login padding, took %s", time.Since(start))
		}
		if rec.Code == http.StatusTooManyRequests {
			return
		}
	}
	t.Fatal("expected rate limit")
}

func TestLoginRotatesCSRF(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	cookie := bareCSRFCookie(t, app)
	body := "address=alice&password=secretsecret&" + csrf.FieldName + "=csrftoken"
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("missing session cookie")
	}
	req2 := httptest.NewRequest(http.MethodGet, "/user/", nil)
	req2.AddCookie(sessionCookie)
	out := app.Sessions.Get(req2)
	if out[session.KeyCSRF] == "csrftoken" {
		t.Fatal("CSRF token was not rotated on login")
	}
	if !out.HasSession() {
		t.Fatal("expected authenticated session")
	}
}

func TestMetricsUnauthorizedWithoutToken(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	app.Cfg.MetricsToken = ""
	handler := app.Routes()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestMetricsAuthorizedWithBearer(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer metrics-test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "portal_up") {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestMetricsRejectsQueryToken(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	req := httptest.NewRequest(http.MethodGet, "/metrics?token=metrics-test-token", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("query token must not authenticate, got %d", rec.Code)
	}
}

func TestCSRFRejectsForgedPOST(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	cookie := adminCookie(t, app)
	req := httptest.NewRequest(http.MethodPost, "/admin/circle/-/new", strings.NewReader("name=Nope&csrf_token=forged"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestSessionCookieNotReadable(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	cookie := adminCookie(t, app)
	if strings.Contains(cookie.Value, "tok") || strings.Contains(cookie.Value, "alice@") {
		t.Fatal("encrypted cookie still contains plaintext auth material")
	}
}

func TestRaceSessionSave(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			data := session.Data{}
			data.SetAuth("tok", "prosody:admin", "alice@example.test")
			if err := app.Sessions.Save(rec, data); err != nil {
				t.Errorf("save: %v", err)
			}
		}(i)
	}
	wg.Wait()
}

func bareCSRFCookie(t *testing.T, app *App) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	data := session.Data{}
	data[session.KeyCSRF] = "csrftoken"
	if err := app.Sessions.Save(rec, data); err != nil {
		t.Fatal(err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			return c
		}
	}
	t.Fatal("no cookie")
	return nil
}
