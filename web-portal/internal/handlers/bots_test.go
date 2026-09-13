package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
)

// stubBotsAPI imitates the mod_snikketx_bots management API.
func stubBotsAPI(t *testing.T) *httptest.Server {
	t.Helper()
	bots := map[string]map[string]any{
		"echo": {
			"name": "echo", "jid": "echo@bots.example.test",
			"owner": "alice@example.test", "label": "Echo",
			"disabled": false, "created": 1700000000,
		},
		"other": {
			"name": "other", "jid": "other@bots.example.test",
			"owner":    "mallory@example.test",
			"disabled": false, "created": 1700000000,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer admintoken" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/bots")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case path == "/" && r.Method == http.MethodGet:
			list := make([]map[string]any, 0, len(bots))
			for _, b := range bots {
				list = append(list, b)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"bots": list, "registration_open": true})
		case path == "/lockdown" && r.Method == http.MethodPost:
			var in struct {
				Open   *bool `json:"open"`
				Locked bool  `json:"locked"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			open := true
			if in.Locked {
				open = false
			}
			if in.Open != nil {
				open = *in.Open
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"registration_open": open})
		case path == "/audit" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": []map[string]any{
				{"id": 1, "ts": 1700000000, "action": "bot.create", "actor": "alice@example.test", "bot": "echo"},
			}})
		case path == "/access" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": []map[string]any{
				{"ts": 1700000000, "ip": "127.0.0.1", "method": "GET", "path": "/bots/echo/rooms", "status": 200, "bot": "echo"},
			}})
		case path == "/" && r.Method == http.MethodPost:
			var in struct {
				Name  string `json:"name"`
				Owner string `json:"owner"`
				Label string `json:"label"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if _, dup := bots[in.Name]; dup {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "conflict"})
				return
			}
			bot := map[string]any{
				"name": in.Name, "jid": in.Name + "@bots.example.test",
				"owner": in.Owner, "label": in.Label,
				"disabled": false, "created": 1700000000,
				"token": "sxb_testtoken",
			}
			bots[in.Name] = bot
			_ = json.NewEncoder(w).Encode(bot)
		case strings.HasSuffix(path, "/tokens") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tokens": []map[string]any{
					{"id": "tok1", "scopes": []string{"read", "write"}, "created": 1700000000},
				},
			})
		case strings.HasSuffix(path, "/tokens") && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "sxb_newtoken", "id": "tok2"})
		case strings.HasSuffix(path, "/tokens/tok1") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"revoked": "tok1"})
		case r.Method == http.MethodPatch:
			name := strings.TrimPrefix(path, "/")
			bot, ok := bots[name]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "not-found"})
				return
			}
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if d, ok := in["disabled"]; ok {
				bot["disabled"] = d
			}
			_ = json.NewEncoder(w).Encode(bot)
		case r.Method == http.MethodDelete:
			name := strings.TrimPrefix(path, "/")
			if _, ok := bots[name]; !ok {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "not-found"})
				return
			}
			delete(bots, name)
			_ = json.NewEncoder(w).Encode(map[string]any{"deleted": name})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not-found"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func botsTestApp(t *testing.T, api *httptest.Server) *App {
	t.Helper()
	app := newTestApp(t, "http://prosody.invalid")
	app.Cfg.BotsEndpoint = api.URL + "/bots"
	app.Cfg.BotsAdminToken = "admintoken"
	return app
}

func TestBotsDisabled(t *testing.T) {
	app := newTestApp(t, "http://prosody.invalid")
	handler := app.Routes()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/user/bots", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404 when bots are not configured", rec.Code)
	}
}

func TestBotsPage(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	req := httptest.NewRequest(http.MethodGet, "/user/bots", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "echo@bots.example.test") {
		t.Fatal("own bot not listed")
	}
	// The other owner's bot must be filtered out.
	if strings.Contains(body, "other@bots.example.test") {
		t.Fatal("foreign bot leaked into list")
	}
	if !strings.Contains(body, "tok1") {
		t.Fatal("token metadata missing")
	}
}

func TestBotsCreate(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()

	recorder := httptest.NewRecorder()
	data := session.Data{}
	data.SetAuth("tok", "prosody:registered", "alice@example.test")
	data["_csrf"] = "csrftoken"
	if err := app.Sessions.Save(recorder, data); err != nil {
		t.Fatal(err)
	}
	var cookie *http.Cookie
	for _, c := range recorder.Result().Cookies() {
		if c.Name == session.CookieName {
			cookie = c
		}
	}

	form := url.Values{"csrf_token": {"csrftoken"}, "name": {"newbot"}, "label": {"Test"}}
	req := httptest.NewRequest(http.MethodPost, "/user/bots", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sxb_testtoken") {
		t.Fatal("new token not shown")
	}
}

// postBotForm posts a CSRF protected form as the alice admin session.
func postBotForm(t *testing.T, handler http.Handler, cookie *http.Cookie, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set("csrf_token", "csrftoken")
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestBotsToggle(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	rec := postBotForm(t, handler, cookie, "/user/bots/echo/toggle", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
}

func TestBotsDelete(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	rec := postBotForm(t, handler, cookie, "/user/bots/echo/delete", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
}

func TestBotsRejectsForeignBot(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	// "other" belongs to mallory@example.test: every mutation must bounce
	// on the ownership check without reaching the API.
	for _, path := range []string{
		"/user/bots/other/delete",
		"/user/bots/other/toggle",
		"/user/bots/other/tokens",
		"/user/bots/other/tokens/tok1/delete",
	} {
		rec := postBotForm(t, handler, cookie, path, url.Values{})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s: status %d, want 303 with unknown-bot flash", path, rec.Code)
		}
	}
}

func TestBotsTokenMintShowsTokenOnce(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	rec := postBotForm(t, handler, cookie, "/user/bots/echo/tokens",
		url.Values{"token_name": {"laptop"}, "ttl_days": {"7"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sxb_newtoken") {
		t.Fatal("newly minted token not shown")
	}
}

func TestBotsTokenRevoke(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	rec := postBotForm(t, handler, cookie, "/user/bots/echo/tokens/tok1/delete", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
}

func TestBotsCreateRejectsBadName(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	form := url.Values{"csrf_token": {"csrftoken"}, "name": {"Bad Name!"}}
	req := httptest.NewRequest(http.MethodPost, "/user/bots", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303 redirect with flash", rec.Code)
	}
}

func TestBotDetailPage(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	req := httptest.NewRequest(http.MethodGet, "/user/bots/echo", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Audit log", "Access log", "bot.create", "/bots/echo/rooms", "tok1", "Any IP"} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail page missing %q", want)
		}
	}
}

func TestBotDetailRejectsForeign(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	req := httptest.NewRequest(http.MethodGet, "/user/bots/other", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "other@bots.example.test") {
		t.Fatal("foreign bot detail leaked")
	}
}

func TestAdminBotsPage(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	req := httptest.NewRequest(http.MethodGet, "/admin/bots", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Admin sees every bot including foreign owned ones.
	for _, want := range []string{"echo@bots.example.test", "other@bots.example.test", "Registrations open"} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin bots page missing %q", want)
		}
	}
}

func TestAdminBotsPageDeniedForUser(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()

	recorder := httptest.NewRecorder()
	data := session.Data{}
	data.SetAuth("tok", "prosody:registered", "alice@example.test")
	data["_csrf"] = "csrftoken"
	if err := app.Sessions.Save(recorder, data); err != nil {
		t.Fatal(err)
	}
	var cookie *http.Cookie
	for _, c := range recorder.Result().Cookies() {
		if c.Name == session.CookieName {
			cookie = c
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/bots", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403 for non admin", rec.Code)
	}
}

func TestAdminBotsLockdown(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	rec := postBotForm(t, handler, cookie, "/admin/bots/lockdown", url.Values{"lock": {"1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	rec = postBotForm(t, handler, cookie, "/admin/bots/lockdown", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
}

func TestAdminBotsToggleForeign(t *testing.T) {
	api := stubBotsAPI(t)
	app := botsTestApp(t, api)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	// Admin manages a bot owned by someone else.
	rec := postBotForm(t, handler, cookie, "/admin/bots/other/toggle", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
}

func TestAdminAuthPage(t *testing.T) {
	app := newTestApp(t, "http://prosody.invalid")
	handler := app.Routes()
	cookie := adminCookie(t, app)

	req := httptest.NewRequest(http.MethodGet, "/admin/auth", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Authentication") {
		t.Fatal("auth page did not render")
	}
}

func TestAdminAuthSnippet(t *testing.T) {
	app := newTestApp(t, "http://prosody.invalid")
	handler := app.Routes()
	cookie := adminCookie(t, app)

	rec := postBotForm(t, handler, cookie, "/admin/auth", url.Values{
		"mode":         {"ldap"},
		"ldap_host":    {"ldap.example.org"},
		"ldap_base_dn": {"ou=people,dc=example,dc=org"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "SNIKKET_TWEAK_LDAP=1") || !strings.Contains(body, "SNIKKET_LDAP_HOST=ldap.example.org") {
		t.Fatal("ldap snippet missing")
	}

	rec = postBotForm(t, handler, cookie, "/admin/auth", url.Values{
		"mode":            {"oauth"},
		"oauth_discovery": {"https://idp.example.org/.well-known/openid-configuration"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SNIKKET_TWEAK_OAUTH=1") {
		t.Fatal("oauth snippet missing")
	}
}

func TestSystemRestart(t *testing.T) {
	app := newTestApp(t, "http://prosody.invalid")
	calls := 0
	app.Prosody = &fakeProsody{
		restartServer: func(ctx context.Context, token string) error {
			calls++
			return nil
		},
	}
	handler := app.Routes()
	cookie := adminCookie(t, app)

	rec := postBotForm(t, handler, cookie, "/admin/system/restart", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if calls != 1 {
		t.Fatalf("restart called %d times, want 1", calls)
	}
}

func TestSystemRestartDeniedForUser(t *testing.T) {
	app := newTestApp(t, "http://prosody.invalid")
	handler := app.Routes()

	recorder := httptest.NewRecorder()
	data := session.Data{}
	data.SetAuth("tok", "prosody:registered", "alice@example.test")
	data["_csrf"] = "csrftoken"
	if err := app.Sessions.Save(recorder, data); err != nil {
		t.Fatal(err)
	}
	var cookie *http.Cookie
	for _, c := range recorder.Result().Cookies() {
		if c.Name == session.CookieName {
			cookie = c
		}
	}
	rec := postBotForm(t, handler, cookie, "/admin/system/restart", url.Values{})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403 for non admin", rec.Code)
	}
}
