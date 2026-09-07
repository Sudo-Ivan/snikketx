package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/health"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

func (a *App) mountAdminOps(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/devices", a.handleDevices)
	mux.HandleFunc("POST /admin/devices", a.handleDevicesSubmit)

	mux.HandleFunc("GET /admin/storage", a.handleStorage)
	mux.HandleFunc("POST /admin/storage", a.handleStorageSubmit)

	mux.HandleFunc("GET /admin/invites/analytics", a.handleInviteAnalytics)

	mux.HandleFunc("GET /admin/archives", a.handleArchives)

	mux.HandleFunc("GET /admin/updates", a.handleUpdates)

	mux.HandleFunc("GET /admin/certs/", a.handleCerts)

	mux.HandleFunc("GET /admin/limits/", a.handleLimits)
	mux.HandleFunc("POST /admin/limits/", a.handleLimitsSubmit)

	mux.HandleFunc("GET /admin/backup/", a.handleBackup)
	mux.HandleFunc("POST /admin/backup/", a.handleBackupSubmit)
	mux.HandleFunc("GET /admin/backup/config.json", a.handleBackupConfig)
	mux.HandleFunc("GET /admin/backup/export/{localpart}", a.handleBackupExport)

	mux.HandleFunc("POST /admin/users/bulk", a.handleUsersBulk)
}

type devicesPage struct {
	webui.PageData
	Clients []prosody.ClientDevice
	Filter  string
	Note    string
}

func (a *App) handleDevices(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	filter := strings.TrimSpace(r.URL.Query().Get("user"))
	clients, err := a.Prosody.ListClientDevices(r.Context(), sess.Token(), filter)
	note := ""
	if err != nil {
		note = "Device list unavailable: " + apiErrorMessage(err)
		clients = nil
	}
	page := a.newPage(w, r, sess, "Devices", "devices", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_devices.html", devicesPage{
		PageData: page,
		Clients:  clients,
		Filter:   filter,
		Note:     note,
	})
}

func (a *App) handleDevicesSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	user := strings.TrimSpace(r.FormValue("user"))
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	if user == "" || clientID == "" {
		a.flashRedirect(w, r, sess, "Missing device identity.", "alert", "/admin/devices")
		return
	}
	if err := a.Prosody.RevokeClientDevice(r.Context(), sess.Token(), user, clientID); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/devices")
		return
	}
	a.recordAudit(r, sess, "device.revoke", user, clientID)
	a.flashRedirect(w, r, sess, "Device revoked.", "success", "/admin/devices")
}

type storagePage struct {
	webui.PageData
	Info *prosody.UploadsInfo
	Note string
}

func (a *App) handleStorage(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	info, err := a.Prosody.GetUploadsInfo(r.Context(), sess.Token())
	note := ""
	if err != nil {
		note = "Upload storage unavailable: " + apiErrorMessage(err)
		info = &prosody.UploadsInfo{}
	}
	page := a.newPage(w, r, sess, "Shared storage", "storage", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_storage.html", storagePage{
		PageData: page,
		Info:     info,
		Note:     note,
	})
}

func (a *App) handleStorageSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	mode := strings.TrimSpace(r.FormValue("mode"))
	if mode == "" {
		mode = "orphans"
	}
	removed, err := a.Prosody.PurgeUploads(r.Context(), sess.Token(), mode)
	if err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/storage")
		return
	}
	a.recordAudit(r, sess, "storage.purge", mode, strconv.Itoa(removed))
	a.flashRedirect(w, r, sess, fmt.Sprintf("Purged %d upload records (%s).", removed, mode), "success", "/admin/storage")
}

type inviteAnalyticsPage struct {
	webui.PageData
	Stats *prosody.InviteStats
	Note  string
}

func (a *App) handleInviteAnalytics(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	stats, err := a.Prosody.GetInviteStats(r.Context(), sess.Token())
	note := ""
	if err != nil {
		note = "Invite analytics unavailable: " + apiErrorMessage(err)
		stats = &prosody.InviteStats{BySource: map[string]int{}}
	}
	page := a.newPage(w, r, sess, "Invite analytics", "invites", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_invite_analytics.html", inviteAnalyticsPage{
		PageData: page,
		Stats:    stats,
		Note:     note,
	})
}

type archivesPage struct {
	webui.PageData
	Info *prosody.ArchivesInfo
	Note string
}

func (a *App) handleArchives(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	info, err := a.Prosody.GetArchivesInfo(r.Context(), sess.Token())
	note := ""
	if err != nil {
		note = "Archive health unavailable: " + apiErrorMessage(err)
		info = &prosody.ArchivesInfo{}
	}
	page := a.newPage(w, r, sess, "Archives", "archives", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_archives.html", archivesPage{
		PageData: page,
		Info:     info,
		Note:     note,
	})
}

type updatesPage struct {
	webui.PageData
	Info *prosody.UpdatesInfo
	Note string
}

func (a *App) handleUpdates(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	info, err := a.Prosody.GetUpdatesInfo(r.Context(), sess.Token())
	note := ""
	if err != nil {
		note = "Update channel unavailable: " + apiErrorMessage(err)
		info = &prosody.UpdatesInfo{}
	}
	page := a.newPage(w, r, sess, "Updates", "updates", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_updates.html", updatesPage{
		PageData: page,
		Info:     info,
		Note:     note,
	})
}

type certsPage struct {
	webui.PageData
	Hosts []health.CertHost
}

func (a *App) handleCerts(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	domain := a.Cfg.Domain
	hosts := []health.CertHost{
		health.ProbeCertHost(r.Context(), domain, "chat domain"),
		health.ProbeCertHost(r.Context(), "groups."+domain, "group chats"),
		health.ProbeCertHost(r.Context(), "share."+domain, "file sharing"),
	}
	page := a.newPage(w, r, sess, "Certificates and DNS", "certs", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_certs.html", certsPage{
		PageData: page,
		Hosts:    hosts,
	})
}

type limitsPage struct {
	webui.PageData
	Locks []authlimit.LockEntry
	Note  string
}

func (a *App) handleLimits(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	var locks []authlimit.LockEntry
	if a.LoginGate != nil {
		locks = a.LoginGate.Locks()
	}
	page := a.newPage(w, r, sess, "Rate limits", "limits", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_limits.html", limitsPage{
		PageData: page,
		Locks:    locks,
		Note:     "Portal login lockouts are listed here. Prosody limit_auth and firewall hits appear in the audit log when those modules report them.",
	})
}

func (a *App) handleLimitsSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	key := strings.TrimSpace(r.FormValue("key"))
	if a.LoginGate == nil || !a.LoginGate.Unlock(key) {
		a.flashRedirect(w, r, sess, "Lockout not found.", "alert", "/admin/limits/")
		return
	}
	a.recordAudit(r, sess, "limits.unlock", key, "")
	a.flashRedirect(w, r, sess, "Login lockout cleared.", "success", "/admin/limits/")
}

type backupPage struct {
	webui.PageData
	Users []prosody.AdminUserInfo
	Note  string
}

func (a *App) handleBackup(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	users, err := a.Prosody.ListUsers(r.Context(), sess.Token())
	note := ""
	if err != nil {
		note = "Accounts could not be listed: " + apiErrorMessage(err)
		users = nil
	}
	page := a.newPage(w, r, sess, "Backup and restore", "backup", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_backup.html", backupPage{
		PageData: page,
		Users:    users,
		Note:     note,
	})
}

func (a *App) handleBackupConfig(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	snapshot := map[string]any{
		"format":         "snikketx-host-config-v1",
		"exported_at":    time.Now().UTC().Format(time.RFC3339),
		"domain":         a.Cfg.Domain,
		"site_name":      a.Cfg.SiteName,
		"show_metrics":   a.Cfg.ShowMetrics,
		"tos_uri":        a.Cfg.TOSURI,
		"privacy_uri":    a.Cfg.PrivacyURI,
		"abuse_email":    a.Cfg.AbuseEmail,
		"security_email": a.Cfg.SecurityEmail,
		"env":            safeEnvSnapshot(),
	}
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		a.failAPI(w, r, err)
		return
	}
	a.recordAudit(r, sess, "backup.config", a.Cfg.Domain, "")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="snikketx-host-config.json"`)
	_, _ = w.Write(body)
}

func safeEnvSnapshot() map[string]string {
	keys := []string{
		"SNIKKET_DOMAIN", "SNIKKET_WEB_DOMAIN", "SNIKKET_WEB_SITE_NAME",
		"SNIKKET_UPLOAD_STORAGE_GB", "SNIKKET_DAILY_UPLOAD_LIMIT_PER_USER_GB",
		"SNIKKET_RETENTION_DAYS", "SNIKKET_MAX_USER_CLIENTS",
		"SNIKKET_TWEAK_INTERNAL_HTTP_PORT",
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if v := os.Getenv(key); v != "" {
			out[key] = v
		}
	}
	return out
}

func (a *App) handleBackupExport(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	localpart := strings.TrimSpace(r.PathValue("localpart"))
	data, err := a.Prosody.ExportAccountPackage(r.Context(), sess.Token(), localpart)
	if err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/backup/")
		return
	}
	a.recordAudit(r, sess, "backup.export", localpart, "")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-account.json"`, localpart))
	_, _ = w.Write(data)
}

func (a *App) handleBackupSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	localpart := strings.TrimSpace(r.FormValue("localpart"))
	if localpart == "" {
		a.flashRedirect(w, r, sess, "Choose an account to restore into.", "alert", "/admin/backup/")
		return
	}
	file, header, err := r.FormFile("package")
	if err != nil {
		a.flashRedirect(w, r, sess, "Upload a JSON account package.", "alert", "/admin/backup/")
		return
	}
	defer file.Close()
	if header.Size > maxImportSize {
		a.flashRedirect(w, r, sess, "Package is too large.", "alert", "/admin/backup/")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxImportSize+1))
	if err != nil || int64(len(data)) > maxImportSize {
		a.flashRedirect(w, r, sess, "Could not read the package.", "alert", "/admin/backup/")
		return
	}
	if err := a.Prosody.ImportAccountPackage(r.Context(), sess.Token(), localpart, data); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/backup/")
		return
	}
	a.recordAudit(r, sess, "backup.import", localpart, header.Filename)
	a.flashRedirect(w, r, sess, "Account package imported.", "success", "/admin/backup/")
}

func (a *App) handleUsersBulk(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	action := strings.TrimSpace(r.FormValue("action"))
	users := r.Form["users"]
	if len(users) == 0 {
		a.flashRedirect(w, r, sess, "Select at least one account.", "alert", "/admin/users")
		return
	}
	var failed int
	for _, localpart := range users {
		localpart = strings.TrimSpace(localpart)
		if localpart == "" {
			continue
		}
		var err error
		switch action {
		case "lock":
			err = a.Prosody.DisableUserAccount(r.Context(), sess.Token(), localpart)
			if err == nil {
				a.recordAudit(r, sess, "user.bulk_lock", localpart, "")
			}
		case "unlock":
			err = a.Prosody.EnableUserAccount(r.Context(), sess.Token(), localpart)
			if err == nil {
				a.recordAudit(r, sess, "user.bulk_unlock", localpart, "")
			}
		case "role":
			role := r.FormValue("role")
			if !validRole(role) {
				role = prosody.ScopeDefault
			}
			err = a.Prosody.UpdateUser(r.Context(), sess.Token(), localpart, prosody.UserUpdate{Role: &role})
			if err == nil {
				a.recordAudit(r, sess, "user.bulk_role", localpart, role)
			}
		case "circle_add":
			circleID := strings.TrimSpace(r.FormValue("circle_id"))
			if circleID == "" {
				failed++
				continue
			}
			err = a.Prosody.AddGroupMember(r.Context(), sess.Token(), circleID, localpart)
			if err == nil {
				a.recordAudit(r, sess, "user.bulk_circle", localpart, circleID)
			}
		default:
			a.flashRedirect(w, r, sess, "Unknown bulk action.", "alert", "/admin/users")
			return
		}
		if err != nil {
			failed++
		}
	}
	msg := fmt.Sprintf("Bulk %s finished for %d accounts.", action, len(users)-failed)
	if failed > 0 {
		msg = fmt.Sprintf("Bulk %s finished with %d failures.", action, failed)
		a.flashRedirect(w, r, sess, msg, "alert", "/admin/users")
		return
	}
	a.flashRedirect(w, r, sess, msg, "success", "/admin/users")
}
