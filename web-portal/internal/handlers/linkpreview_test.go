package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sudo-ivan/snikketx/web-portal/internal/linkpreview"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
)

// fakePreview implements LinkPreviewer for handler tests.
type fakePreview struct {
	meta  func(ctx context.Context, rawURL string) (*linkpreview.Metadata, error)
	image func(ctx context.Context, rawURL string) ([]byte, string, error)
}

func (f *fakePreview) FetchMetadata(ctx context.Context, rawURL string) (*linkpreview.Metadata, error) {
	return f.meta(ctx, rawURL)
}

func (f *fakePreview) FetchImage(ctx context.Context, rawURL string) ([]byte, string, error) {
	return f.image(ctx, rawURL)
}

// newLinkPreviewApp builds the app with a fake fetcher and a login stub that
// accepts only alice with password "correct-horse".
func newLinkPreviewApp(t *testing.T) (*App, *fakePreview, *int) {
	t.Helper()
	app := newTestApp(t, "http://prosody.invalid")

	preview := &fakePreview{
		meta: func(ctx context.Context, rawURL string) (*linkpreview.Metadata, error) {
			return &linkpreview.Metadata{
				URL:         "https://news.test/article",
				Title:       "An article",
				Description: "About things",
				SiteName:    "News",
				Image:       "https://news.test/pic.png",
				Favicon:     "https://news.test/favicon.ico",
			}, nil
		},
		image: func(ctx context.Context, rawURL string) ([]byte, string, error) {
			return []byte("\x89PNG\r\n\x1a\n"), "image/png", nil
		},
	}
	app.LinkPreview = preview

	var logins int
	app.Prosody = &fakeProsody{
		login: func(ctx context.Context, address, password string) (*prosody.TokenInfo, error) {
			logins++
			if address == "alice@example.test" && password == "correct-horse" {
				return &prosody.TokenInfo{Token: "tok", Scopes: []string{"prosody:registered"}}, nil
			}
			return nil, prosody.ErrInvalidCredentials
		},
	}
	return app, preview, &logins
}

func TestLinkPreviewRequiresAuth(t *testing.T) {
	app, _, _ := newLinkPreviewApp(t)
	handler := app.Routes()

	for _, path := range []string{"/api/link-preview?url=http://x.test/", "/api/link-preview/image?url=http://x.test/i.png"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", path, rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("%s: missing WWW-Authenticate", path)
		}
	}
}

func TestLinkPreviewRejectsBadPasswordAndForeignDomain(t *testing.T) {
	app, _, logins := newLinkPreviewApp(t)
	handler := app.Routes()

	// Wrong password.
	req := httptest.NewRequest(http.MethodGet, "/api/link-preview?url=http://x.test/", nil)
	req.SetBasicAuth("alice@example.test", "wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad password: status %d, want 401", rec.Code)
	}

	// A JID on another domain must be rejected before any backend call.
	before := *logins
	req = httptest.NewRequest(http.MethodGet, "/api/link-preview?url=http://x.test/", nil)
	req.SetBasicAuth("alice@other.test", "correct-horse")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign domain: status %d, want 401", rec.Code)
	}
	if *logins != before {
		t.Fatal("foreign domain credentials were sent to the backend")
	}
}

func TestLinkPreviewSuccess(t *testing.T) {
	app, _, _ := newLinkPreviewApp(t)
	handler := app.Routes()

	for _, username := range []string{"alice@example.test", "alice"} {
		req := httptest.NewRequest(http.MethodGet, "/api/link-preview?url="+
			"https%3A%2F%2Fnews.test%2Farticle", nil)
		req.SetBasicAuth(username, "correct-horse")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", username, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("Content-Type = %q", ct)
		}
		var meta linkpreview.Metadata
		if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
			t.Fatal(err)
		}
		if meta.Title != "An article" || meta.URL != "https://news.test/article" ||
			meta.SiteName != "News" || meta.Image == "" || meta.Favicon == "" {
			t.Fatalf("unexpected metadata: %+v", meta)
		}
	}
}

func TestLinkPreviewImage(t *testing.T) {
	app, _, _ := newLinkPreviewApp(t)
	handler := app.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/link-preview/image?url=https%3A%2F%2Fnews.test%2Fpic.png", nil)
	req.SetBasicAuth("alice", "correct-horse")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("empty image body")
	}
}

func TestLinkPreviewRateLimit(t *testing.T) {
	app, _, _ := newLinkPreviewApp(t)
	app.LinkPreviewGate = linkpreview.NewLimiter(1)
	handler := app.Routes()

	get := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/link-preview?url=http://x.test/", nil)
		req.SetBasicAuth("alice", "correct-horse")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := get(); code != http.StatusOK {
		t.Fatalf("first request: status %d", code)
	}
	if code := get(); code != http.StatusTooManyRequests {
		t.Fatalf("second request: status %d, want 429", code)
	}
}

func TestLinkPreviewBlockedTarget(t *testing.T) {
	app, _, _ := newLinkPreviewApp(t)
	// Use the real fetcher: a literal private address fails validation
	// before any network activity.
	app.LinkPreview = linkpreview.New()
	handler := app.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/link-preview?url=http%3A%2F%2F127.0.0.1%2Fx", nil)
	req.SetBasicAuth("alice", "correct-horse")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403, body %s", rec.Code, rec.Body.String())
	}
}

func TestLinkPreviewFetchError(t *testing.T) {
	app, preview, _ := newLinkPreviewApp(t)
	preview.meta = func(ctx context.Context, rawURL string) (*linkpreview.Metadata, error) {
		return nil, errors.New("dial tcp: boom")
	}
	handler := app.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/link-preview?url=http://x.test/", nil)
	req.SetBasicAuth("alice", "correct-horse")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", rec.Code)
	}
}

func TestLinkPreviewDisabled(t *testing.T) {
	app, _, _ := newLinkPreviewApp(t)
	app.Cfg.LinkPreviewEnabled = false
	handler := app.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/link-preview?url=http://x.test/", nil)
	req.SetBasicAuth("alice", "correct-horse")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}
