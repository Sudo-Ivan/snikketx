package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAndroidAPKDownloadRateLimit(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	payload := bytes.Repeat([]byte("SNIKKETX-APK"), 200)
	if _, err := app.AppCache.StoreUpload(bytes.NewReader(payload), "demo.apk"); err != nil {
		t.Fatal(err)
	}
	settings := app.AppCache.Settings()
	settings.Enabled = true
	settings.DownloadLimitPerHour = 2
	if err := app.AppCache.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	app.APKGate.SetLimit(2)

	handler := app.Routes()
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/download/android.apk", nil)
		req.RemoteAddr = "198.51.100.20:4444"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("download %d: status %d", i+1, rec.Code)
		}
		if got := rec.Body.Len(); got != len(payload) {
			t.Fatalf("download %d: bytes %d", i+1, got)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/download/android.apk", nil)
	req.RemoteAddr = "198.51.100.20:4444"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

func TestAndroidAPKHiddenWhenDisabled(t *testing.T) {
	backend := stubProsody(t)
	app := newTestApp(t, backend.URL)
	payload := bytes.Repeat([]byte("SNIKKETX-APK"), 200)
	if _, err := app.AppCache.StoreUpload(bytes.NewReader(payload), "demo.apk"); err != nil {
		t.Fatal(err)
	}
	handler := app.Routes()
	req := httptest.NewRequest(http.MethodGet, "/download/android.apk", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when disabled, got %d", rec.Code)
	}
}
