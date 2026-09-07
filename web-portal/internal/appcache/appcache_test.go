package appcache_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/appcache"
)

func TestStoreUploadAndReady(t *testing.T) {
	dir := t.TempDir()
	c, err := appcache.Open(dir, appcache.Defaults{PackageID: "org.example.app"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Ready() {
		t.Fatal("expected not ready before enable and upload")
	}
	payload := bytes.Repeat([]byte("SNIKKETX-APK"), 200)
	meta, err := c.StoreUpload(bytes.NewReader(payload), "demo.apk")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Size != int64(len(payload)) {
		t.Fatalf("size=%d", meta.Size)
	}
	if meta.SHA256 == "" {
		t.Fatal("missing sha256")
	}
	if err := c.SaveSettings(appcache.Settings{
		Enabled:              true,
		PackageID:            "org.example.app",
		DownloadLimitPerHour: 3,
		RefreshIntervalHours: 6,
	}); err != nil {
		t.Fatal(err)
	}
	if !c.Ready() {
		t.Fatal("expected ready")
	}
	if got := c.PlayURL(); got != "https://play.google.com/store/apps/details?id=org.example.app" {
		t.Fatalf("play url=%q", got)
	}
}

func TestDownloadLimiter(t *testing.T) {
	l := appcache.NewDownloadLimiter(2)
	if ok, _ := l.Allow("1.2.3.4"); !ok {
		t.Fatal("first should allow")
	}
	if ok, _ := l.Allow("1.2.3.4"); !ok {
		t.Fatal("second should allow")
	}
	if ok, retry := l.Allow("1.2.3.4"); ok || retry <= 0 {
		t.Fatalf("third should block retry=%v", retry)
	}
	if ok, _ := l.Allow("5.6.7.8"); !ok {
		t.Fatal("other ip should allow")
	}
}

func TestRefreshDirectURL(t *testing.T) {
	payload := bytes.Repeat([]byte("APKDATA"), 300)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	c, err := appcache.Open(dir, appcache.Defaults{
		Enabled:   true,
		SourceURL: srv.URL + "/app.apk",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if !c.Ready() {
		t.Fatal("expected ready after refresh")
	}
	meta := c.Meta()
	if meta.Size != int64(len(payload)) {
		t.Fatalf("size=%d", meta.Size)
	}
	if _, err := os.Stat(filepath.Join(dir, "latest.apk")); err != nil {
		t.Fatal(err)
	}
}
