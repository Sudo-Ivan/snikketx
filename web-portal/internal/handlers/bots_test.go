package handlers

import (
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
			_ = json.NewEncoder(w).Encode(map[string]any{"bots": list})
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
