package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

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
	if err := json.NewEncoder(w).Encode(meta); err != nil {
		slog.Warn("android version encode failed", slog.String("error", err.Error()))
	}
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
