package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/sudo-ivan/snikketx/web-portal/internal/appcache"
	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
)

// handleAndroidVersion returns the cached APK metadata as JSON so installed
// clients can check for updates without downloading the package.
func (a *App) handleAndroidVersion(w http.ResponseWriter, r *http.Request) {
	if a.AppCache == nil || !a.AppCache.Ready() {
		http.NotFound(w, r)
		return
	}
	meta, err := a.AppCache.PublicMeta()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	payload := struct {
		appcache.Meta
		SigningCertSHA256 string `json:"signing_cert_sha256,omitempty"`
	}{Meta: meta, SigningCertSHA256: a.Cfg.AndroidCertSHA256}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Warn("android version encode failed", slog.String("error", err.Error()))
	}
}

// handleAndroidSHA256 serves the checksum of the cached APK in sha256sum
// format so downloads can be verified with standard tooling.
func (a *App) handleAndroidSHA256(w http.ResponseWriter, r *http.Request) {
	if a.AppCache == nil || !a.AppCache.Ready() {
		http.NotFound(w, r)
		return
	}
	meta, err := a.AppCache.PublicMeta()
	if err != nil || meta.SHA256 == "" {
		http.NotFound(w, r)
		return
	}
	filename := meta.Filename
	if filename == "" {
		filename = "snikketx-android.apk"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = fmt.Fprintf(w, "%s  %s\n", meta.SHA256, filename)
}

// handleAndroidCertSHA256 serves the SHA-256 fingerprint of the APK signing
// certificate when the operator configured one.
func (a *App) handleAndroidCertSHA256(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.AndroidCertSHA256 == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = fmt.Fprintln(w, a.Cfg.AndroidCertSHA256)
}

// handleAndroidAPK serves the cached Android package with per IP rate limiting.
func (a *App) handleAndroidAPK(w http.ResponseWriter, r *http.Request) {
	if a.AppCache == nil || !a.AppCache.Ready() {
		http.NotFound(w, r)
		return
	}
	ip := authlimit.ClientIP(r)
	if a.APKGate != nil {
		a.APKGate.SetLimit(a.AppCache.DownloadLimit())
		ok, retry := a.APKGate.Allow(ip)
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
			http.Error(w, "download rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	f, meta, err := a.AppCache.OpenAPK()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	filename := meta.Filename
	if filename == "" {
		filename = "snikketx-android.apk"
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if meta.SHA256 != "" {
		w.Header().Set("ETag", `"`+meta.SHA256+`"`)
		if match := r.Header.Get("If-None-Match"); match == `"`+meta.SHA256+`"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	http.ServeContent(w, r, filename, meta.FetchedAt, f)
}
