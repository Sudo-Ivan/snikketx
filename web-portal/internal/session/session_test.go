package session_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/quick"

	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
)

func testSecret() []byte {
	return []byte("0123456789abcdef0123456789abcdef")
}

func TestRoundTrip(t *testing.T) {
	store, err := session.New(testSecret(), false)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	in := session.Data{}
	in.SetAuth("token-value", "prosody:admin", "alice@example.test")
	in.PushFlash("hi", "success")
	if err := store.Save(rec, in); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	out := store.Get(req)
	if out.Token() != "token-value" || out.JID() != "alice@example.test" || !out.IsAdmin() {
		t.Fatalf("round trip lost auth: %#v", out)
	}
	flash := out.PopFlash()
	if flash == nil || flash.Message != "hi" || flash.Category != "success" {
		t.Fatalf("flash: %#v", flash)
	}
	cookie := rec.Result().Cookies()[0].Value
	if strings.Contains(cookie, "token-value") || strings.Contains(cookie, "alice@") {
		t.Fatal("cookie leaks plaintext session fields")
	}
}

func TestTamperRejected(t *testing.T) {
	store, err := session.New(testSecret(), false)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	data := session.Data{"prosody_jid": "alice@example.test"}
	if err := store.Save(rec, data); err != nil {
		t.Fatal(err)
	}
	raw := rec.Result().Cookies()[0].Value
	tampered := raw[:len(raw)/2] + "AAAA" + raw[len(raw)/2:]
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: tampered})
	out := store.Get(req)
	if out.HasSession() || out.JID() != "" {
		t.Fatalf("tampered cookie accepted: %#v", out)
	}
}

func TestPropertyRoundTrip(t *testing.T) {
	store, err := session.New(testSecret(), true)
	if err != nil {
		t.Fatal(err)
	}
	f := func(token, scope, jid string) bool {
		if len(token) > 200 || len(scope) > 200 || len(jid) > 200 {
			return true
		}
		rec := httptest.NewRecorder()
		in := session.Data{}
		in.SetAuth(token, scope, jid)
		if err := store.Save(rec, in); err != nil {
			return false
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, c := range rec.Result().Cookies() {
			req.AddCookie(c)
			if !c.Secure || !c.HttpOnly {
				return false
			}
		}
		out := store.Get(req)
		return out.Token() == token && out.Scope() == scope && out.JID() == jid
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

func FuzzDecode(f *testing.F) {
	store, err := session.New(testSecret(), false)
	if err != nil {
		f.Fatal(err)
	}
	rec := httptest.NewRecorder()
	_ = store.Save(rec, session.Data{"prosody_jid": "a@b.c"})
	f.Add(rec.Result().Cookies()[0].Value)
	f.Add("")
	f.Add("!!!")
	f.Fuzz(func(t *testing.T, val string) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: val})
		_ = store.Get(req)
	})
}

func BenchmarkSaveGet(b *testing.B) {
	store, err := session.New(testSecret(), false)
	if err != nil {
		b.Fatal(err)
	}
	data := session.Data{}
	data.SetAuth("tok", "prosody:admin", "alice@example.test")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		if err := store.Save(rec, data); err != nil {
			b.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, c := range rec.Result().Cookies() {
			req.AddCookie(c)
		}
		_ = store.Get(req)
	}
}
