package handlers

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/appcache"
	"github.com/sudo-ivan/snikketx/web-portal/internal/audit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/config"
	"github.com/sudo-ivan/snikketx/web-portal/internal/health"
	"github.com/sudo-ivan/snikketx/web-portal/internal/metrics"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
	"github.com/sudo-ivan/snikketx/web-portal/web"
)

func stubProsody(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/oauth2/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"cid","client_secret":"secret"}`))
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"bearer","scope":"prosody:registered prosody:admin"}`))
	})
	mux.HandleFunc("/admin_api/users", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"username":"alice","display_name":"Alice","role":"prosody:admin","enabled":true,"last_active":1700000000,"avatar_info":[{"hash":"aabbccddeeff00112233445566778899aabbccdd","bytes":100,"type":"image/png"}]},{"username":"bob","enabled":false,"role":"prosody:registered"}]`))
	})
	mux.HandleFunc("/admin_api/users/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/debug") {
			_, _ = w.Write([]byte(`{"sessions":[],"roster":2}`))
			return
		}
		_, _ = w.Write([]byte(`{"username":"alice","display_name":"Alice","role":"prosody:admin","enabled":true,"deletion_request":{"deleted_at":1700000000,"pending_until":1800000000}}`))
	})
	mux.HandleFunc("/admin_api/invites", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"inv1","type":"register","token":"inv1","created_at":1700000000,"expires":4000000000,"groups":["g1"],"roles":["prosody:registered"],"note":"For Carol"}]`))
	})
	mux.HandleFunc("/admin_api/invites/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"inv1","type":"register","jid":"alice@example.test","created_at":1700000000,"expires":4000000000,"groups":["g1"],"roles":["prosody:registered"],"note":"For Carol","xmpp_uri":"xmpp:example.test?register;preauth=inv1"}`))
	})
	mux.HandleFunc("/admin_api/groups", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"g1","name":"Family","members":{"alice":true,"bob":true},"chats":{"c1":{"id":"c1","jid":"family@groups.example.test","name":"Family chat"}}}]`))
	})
	mux.HandleFunc("/admin_api/groups/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"g1","name":"Family","members":{"alice":true,"bob":true},"chats":[{"id":"c1","jid":"family@groups.example.test","name":"Family chat"}]}`))
	})
	mux.HandleFunc("/snikket_audit_api/events", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"events":[{"id":"a1","when":1700000000,"source":"prosody","actor":"alice","action":"user-created","target":"bob","detail":"account created","ip":"203.0.113.9"}]}`))
	})
	mux.HandleFunc("/snikket_muc_api/rooms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"jid":"friends@groups.example.test","localpart":"friends","name":"Friends","description":"","occupants":0,"persistent":true,"public":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"host":"groups.example.test","rooms":[{"jid":"family@groups.example.test","localpart":"family","name":"Family","description":"Home","occupants":2,"persistent":true,"public":false,"members_only":true}]}`))
	})
	mux.HandleFunc("/snikket_muc_api/rooms/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"jid":"family@groups.example.test","localpart":"family","name":"Family","description":"Home","occupants":1,"persistent":true,"public":false,"occupants_list":[{"nick":"Alice","jid":"alice@example.test","role":"moderator","affiliation":"owner"}]}`))
	})
	mux.HandleFunc("/snikket_ops_api/clients", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"count":1,"clients":[{"user":"alice","client_id":"c1","name":"Conversations","has_push":true,"push_service":"fcm","last_seen":1700000000,"ip":"203.0.113.10"}]}`))
	})
	mux.HandleFunc("/snikket_ops_api/clients/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/snikket_ops_api/me/clients", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`{"revoked":1,"user":"alice"}`))
			return
		}
		_, _ = w.Write([]byte(`{"count":1,"user":"alice","clients":[{"user":"alice","client_id":"c1","name":"Conversations","has_push":true,"push_service":"fcm","last_seen":1700000000,"ip":"203.0.113.10"}]}`))
	})
	mux.HandleFunc("/snikket_ops_api/me/clients/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/snikket_ops_api/uploads", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"host":"share.example.test","available":true,"used_bytes":12345,"file_count":2,"orphan_count":1,"retention_days":7,"largest":[{"id":"f1","name":"photo.jpg","size":12000,"uploader":"alice","when":1700000000}],"orphans":[{"id":"f2","name":"lost.bin","size":345,"when":1700000000}],"by_user":[{"user":"alice","bytes":12000,"files":1}]}`))
	})
	mux.HandleFunc("/snikket_ops_api/uploads/purge", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"removed":1,"mode":"orphans"}`))
	})
	mux.HandleFunc("/snikket_ops_api/invites/stats", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"outstanding":2,"used":5,"conversion_rate":0.71,"by_source":{"portal":4,"app":3},"tracking":[{"token":"t1","source":"portal","user":"carol","when":1700000000}],"daily":[{"day":"2026-09-01","used":2},{"day":"2026-09-02","used":3}],"bootstrap":{"records":1,"configured":true}}`))
	})
	mux.HandleFunc("/snikket_ops_api/archives", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"mam_total":100,"offline_total":3,"muc_mam_available":true,"retention_days":7,"users":[{"username":"alice","mam":80,"offline":1},{"username":"bob","mam":20,"offline":2}]}`))
	})
	mux.HandleFunc("/snikket_ops_api/updates", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"branch":"release","current":{"version":"Snikket release 1.0"},"latest":"1.1","secure":"1.0","check_enabled":true}`))
	})
	mux.HandleFunc("/snikket_ops_api/export/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"format":"snikketx-account-v1","username":"alice","roster":{},"vcard":{}}`))
	})
	mux.HandleFunc("/snikket_ops_api/import/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"username":"alice","written":["roster","vcard"]}`))
	})
	mux.HandleFunc("/admin_api/server/metrics", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"memory":123456789,"c2s":7,"uploads":98765432,"cpu":{"value":12.5,"since":1700000000},"users":{"active_1d":3,"active_7d":5,"active_30d":9}}`))
	})
	mux.HandleFunc("/register_api/invite/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "reset") {
			_, _ = w.Write([]byte(`{"uri":"xmpp:example.test?roster","reset":"alice","domain":"example.test"}`))
			return
		}
		_, _ = w.Write([]byte(`{"inviter":"Alice","uri":"xmpp:example.test?register;preauth=inv1","domain":"example.test"}`))
	})
	mux.HandleFunc("/xep227/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<server-data xmlns="urn:xmpp:pie:0"></server-data>`))
	})
	mux.HandleFunc("/xep227/import", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/rest", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Content-Type"), "xml") {
			w.Header().Set("Content-Type", "application/xmpp+xml")
			_, _ = w.Write([]byte(`<iq type="result" id="1"><pubsub xmlns="http://jabber.org/protocol/pubsub"><items node="http://jabber.org/protocol/nick"><item><nick xmlns="http://jabber.org/protocol/nick">Alice</nick></item></items></pubsub></iq>`))
			return
		}
		_, _ = w.Write([]byte(`{"kind":"iq","type":"result","version":{"version":"prosody 13.0.1"}}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func newTestApp(t *testing.T, endpoint string) *App {
	t.Helper()

	templateFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		t.Fatal(err)
	}
	sprite, err := fs.ReadFile(staticFS, "img/icons.svg")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := webui.New(templateFS, webui.Options{Sprite: sprite})
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		SecretKey:        []byte("0123456789abcdef0123456789abcdef"),
		ProsodyEndpoint:  endpoint,
		Domain:           "example.test",
		SiteName:         "Example Chat",
		AvatarCacheTTL:   time.Minute,
		AppleStoreURL:    "https://apps.apple.com/app/id1",
		MaxAvatarSize:    1 << 20,
		ShowMetrics:      true,
		MetricsToken:     "metrics-test-token",
		TOSURI:           "https://example.test/tos",
		PrivacyURI:       "https://example.test/privacy",
		AbuseEmail:       "abuse@example.test",
		SecurityEmail:    "security@example.test",
		Version:          "test",
		BuildCommit:      "abc1234",
		BuildDate:        "2026-09-07",
		AndroidPackageID: "org.snikket.android",
	}

	store, err := session.New(cfg.SecretKey, false)
	if err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	auditStore, err := audit.Open(stateDir, 32)
	if err != nil {
		t.Fatal(err)
	}
	apkCache, err := appcache.Open(filepath.Join(stateDir, "android"), appcache.Defaults{
		PackageID:            "org.snikket.android",
		DownloadLimitPerHour: 4,
		RefreshIntervalHours: 12,
	})
	if err != nil {
		t.Fatal(err)
	}

	return &App{
		Cfg:       cfg,
		Prosody:   prosody.New(endpoint, "example.test", "test"),
		Sessions:  store,
		Templates: renderer,
		Errors:    health.NewRing(8),
		Audit:     auditStore,
		Metrics:   metrics.New(),
		LoginGate: authlimit.NewFast(),
		AppCache:  apkCache,
		APKGate:   appcache.NewDownloadLimiter(4),
		Started:   time.Now().Add(-time.Hour),
		Static:    http.StripPrefix("/static/", http.FileServerFS(staticFS)),
	}
}

func adminCookie(t *testing.T, app *App) *http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	data := session.Data{}
	data.SetAuth("tok", "prosody:registered prosody:admin", "alice@example.test")
	data["_csrf"] = "csrftoken"
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

func TestGetRoutesRender(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	paths := []string{
		"/", "/login", "/meta/about.html", "/policies/", "/terms", "/privacy",
		"/.well-known/security.txt", "/site.webmanifest", "/_health", "/_health/ready",
		"/metrics", "/static/css/app.css",
		"/user/", "/user/passwd", "/user/profile", "/user/manage_data", "/user/logout",
		"/admin/", "/admin/users", "/admin/user/alice/", "/admin/user/alice/delete",
		"/admin/user/alice/debug", "/admin/users/password-reset/inv1",
		"/admin/invitations", "/admin/invitation/-/new", "/admin/invitation/inv1",
		"/admin/circles", "/admin/circle/-/new", "/admin/circle/g1",
		"/admin/circle/g1/delete", "/admin/circle/g1/add_chat",
		"/admin/system/", "/admin/health/", "/admin/audit/", "/admin/audit/export.csv", "/admin/audit/export.json", "/admin/mucs", "/admin/muc/-/new", "/admin/muc/family",
		"/admin/devices", "/admin/storage", "/admin/invites/analytics", "/admin/archives", "/admin/updates", "/admin/apps",
		"/admin/certs/", "/admin/limits/", "/admin/backup/", "/admin/backup/export/alice", "/admin/logs/",
		"/download/android.apk",
		"/invite/inv1/", "/invite/inv1/register", "/invite/reset-inv/reset",
		"/invite/reset-inv/", "/invite/success", "/invite/success/reset",
		"/invite/missing-x/",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(cookie)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code >= 500 {
				t.Fatalf("status %d\n%s", recorder.Code, recorder.Body.String())
			}
			body := recorder.Body.String()
			if strings.Contains(body, "ZgotmplZ") {
				t.Errorf("template escaped a URL to ZgotmplZ")
			}
			if strings.Contains(body, "&lt;svg") {
				t.Errorf("sprite was escaped instead of inlined")
			}
			t.Logf("%d %s (%d bytes)", recorder.Code, path, len(body))
		})
	}

	for _, entry := range app.Errors.Recent() {
		t.Errorf("recorded error: %s", entry.Message)
	}
}

func TestPostRoutesRender(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	posts := []struct {
		path string
		form string
	}{
		{"/login", "address=alice&password=secretsecret"},
		{"/user/passwd", "current_password=old&new_password=short"},
		{"/user/profile", "nickname=Alice&profile_access_model=presence"},
		{"/user/manage_data", ""},
		{"/admin/user/alice/", "action=save&display_name=Alice&role=prosody:admin"},
		{"/admin/invitations", "revoke=inv1"},
		{"/admin/invitation/-/new", "circles=g1&role=prosody:registered&lifetime=604800&type=account"},
		{"/admin/circle/-/new", "name=Friends"},
		{"/admin/circle/g1", "action=save&name=Family"},
		{"/admin/circle/g1/add_chat", "name=Chat"},
		{"/admin/system/", "action=preview&text=hello"},
		{"/invite/inv1/register", "localpart=carol&password=short&password_confirm=short"},
		{"/invite/reset-inv/reset", "password=verylongpassword&password_confirm=verylongpassword"},
	}

	for _, post := range posts {
		t.Run(post.path, func(t *testing.T) {
			body := post.form
			if body != "" {
				body += "&"
			}
			body += "csrf_token=csrftoken"

			req := httptest.NewRequest(http.MethodPost, post.path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(cookie)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code >= 500 {
				t.Fatalf("status %d\n%s", recorder.Code, recorder.Body.String())
			}
			t.Logf("%d %s", recorder.Code, post.path)
		})
	}
}

func TestCSRFRejectsMissingToken(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	handler := app.Routes()
	cookie := adminCookie(t, app)

	req := httptest.NewRequest(http.MethodPost, "/admin/circle/-/new", strings.NewReader("name=Nope"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
}
