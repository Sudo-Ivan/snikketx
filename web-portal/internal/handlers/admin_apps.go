package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/appcache"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

type appsPage struct {
	webui.PageData
	Settings appcache.Settings
	Meta     appcache.Meta
	Ready    bool
	Note     string
}

func (a *App) handleApps(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if a.AppCache == nil {
		page := a.newPage(w, r, sess, "Apps", "apps", webui.ShellAdmin)
		a.render(w, r, http.StatusOK, "admin_apps.html", appsPage{
			PageData: page,
			Note:     "Android APK hosting is unavailable in this build.",
		})
		return
	}
	page := a.newPage(w, r, sess, "Apps", "apps", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_apps.html", appsPage{
		PageData: page,
		Settings: a.AppCache.Settings(),
		Meta:     a.AppCache.Meta(),
		Ready:    a.AppCache.Ready(),
	})
}

func (a *App) handleAppsSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if a.AppCache == nil {
		a.flashRedirect(w, r, sess, "Android APK hosting is unavailable.", "alert", "/admin/apps")
		return
	}
	action := strings.TrimSpace(r.FormValue("action"))
	switch action {
	case "save":
		next := a.AppCache.Settings()
		next.Enabled = r.FormValue("enabled") == "1"
		next.SourceURL = strings.TrimSpace(r.FormValue("source_url"))
		next.PackageID = strings.TrimSpace(r.FormValue("package_id"))
		next.PlayStoreURL = strings.TrimSpace(r.FormValue("play_store_url"))
		next.FDroidURL = strings.TrimSpace(r.FormValue("fdroid_url"))
		if v := strings.TrimSpace(r.FormValue("download_limit")); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				a.flashRedirect(w, r, sess, "Download limit must be a number.", "alert", "/admin/apps")
				return
			}
			next.DownloadLimitPerHour = n
		}
		if v := strings.TrimSpace(r.FormValue("refresh_hours")); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				a.flashRedirect(w, r, sess, "Refresh interval must be a number.", "alert", "/admin/apps")
				return
			}
			next.RefreshIntervalHours = n
		}
		if err := a.AppCache.SaveSettings(next); err != nil {
			a.flashRedirect(w, r, sess, err.Error(), "alert", "/admin/apps")
			return
		}
		if a.APKGate != nil {
			a.APKGate.SetLimit(a.AppCache.DownloadLimit())
		}
		a.recordAudit(r, sess, "apps.settings", next.PackageID, "enabled="+strconv.FormatBool(next.Enabled))
		a.flashRedirect(w, r, sess, "App hosting settings saved.", "success", "/admin/apps")
	case "refresh":
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		if err := a.AppCache.Refresh(ctx); err != nil {
			a.flashRedirect(w, r, sess, "Refresh failed: "+err.Error(), "alert", "/admin/apps")
			return
		}
		a.recordAudit(r, sess, "apps.refresh", a.AppCache.PackageID(), a.AppCache.Meta().SHA256)
		a.flashRedirect(w, r, sess, "Cached Android APK refreshed.", "success", "/admin/apps")
	case "upload":
		file, header, err := r.FormFile("apk_file")
		if err != nil {
			a.flashRedirect(w, r, sess, "Choose an APK file to upload.", "alert", "/admin/apps")
			return
		}
		defer func() { _ = file.Close() }()
		meta, err := a.AppCache.StoreUpload(file, header.Filename)
		if err != nil {
			a.flashRedirect(w, r, sess, "Upload failed: "+err.Error(), "alert", "/admin/apps")
			return
		}
		a.recordAudit(r, sess, "apps.upload", a.AppCache.PackageID(), meta.SHA256)
		a.flashRedirect(w, r, sess, "APK uploaded into the local cache.", "success", "/admin/apps")
	case "clear":
		if err := a.AppCache.Clear(); err != nil {
			a.flashRedirect(w, r, sess, err.Error(), "alert", "/admin/apps")
			return
		}
		a.recordAudit(r, sess, "apps.clear", a.AppCache.PackageID(), "")
		a.flashRedirect(w, r, sess, "Cached APK cleared.", "success", "/admin/apps")
	default:
		a.flashRedirect(w, r, sess, "Unknown action.", "alert", "/admin/apps")
	}
}
