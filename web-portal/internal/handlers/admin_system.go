package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/health"
	"github.com/sudo-ivan/snikketx/web-portal/internal/hostmetrics"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

// adminSystemPage is the data behind the system page.
type adminSystemPage struct {
	webui.PageData
	ProsodyVersion string
	Metrics        serverMetrics
	Host           hostmetrics.Stats
	Uptime         string
	Announcement   string
}

// handleSystemForm shows the system page with the announcement form and the
// process and chat server counters.
func (a *App) handleSystemForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	a.renderSystem(w, r, sess, "", http.StatusOK)
}

// renderSystem draws the system page, keeping any typed announcement in place.
func (a *App) renderSystem(w http.ResponseWriter, r *http.Request, sess session.Data, announcement string, status int) {
	data := adminSystemPage{
		Host:         hostmetrics.Collect(),
		Uptime:       time.Since(a.Started).Round(time.Second).String(),
		Announcement: announcement,
	}

	if a.Cfg.ShowMetrics {
		version, err := a.Prosody.GetServerVersion(r.Context(), sess.Token(), sess.JID())
		if err != nil {
			version = "unknown"
		}
		data.ProsodyVersion = version
		data.Metrics = parseServerMetrics(a.adminMetrics(r.Context(), sess.Token()))
	}

	data.PageData = a.newPage(w, r, sess, "System", "system", webui.ShellAdmin)
	a.render(w, r, status, "admin_system.html", data)
}

// handleSystemSubmit posts a server announcement. A preview is delivered to the
// administrator only and leaves the form on screen.
func (a *App) handleSystemSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	body := strings.TrimSpace(r.FormValue("text"))
	if body == "" {
		page := a.newPage(w, r, sess, "System", "system", webui.ShellAdmin)
		page.AddError("%s", "Write a message before sending it.")
		data := adminSystemPage{
			PageData: page,
			Host:     hostmetrics.Collect(),
			Uptime:   time.Since(a.Started).Round(time.Second).String(),
		}
		a.render(w, r, http.StatusBadRequest, "admin_system.html", data)
		return
	}

	recipients := "self"
	if r.FormValue(fieldAction) == "post_all" {
		recipients = "all"
		if r.FormValue("online_only") != "" {
			recipients = "online"
		}
	}

	if err := a.Prosody.PostAnnouncement(r.Context(), sess.Token(), body, recipients, sess.JID()); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminSystem)
		return
	}

	if recipients == "self" {
		a.recordAudit(r, sess, "announcement.preview", "", "")
		a.flashRedirect(w, r, sess, "Preview sent to your own account.", "success", pathAdminSystem)
		return
	}
	a.recordAudit(r, sess, "announcement.send", recipients, "")
	a.flashRedirect(w, r, sess, "Announcement sent.", "success", pathAdminSystem)
}

// adminHealthPage is the data behind the health page.
type adminHealthPage struct {
	webui.PageData
	Components   []health.Component
	RecentErrors []health.ErrorEntry
	Uptime       string
	Healthy      bool
}

// handleAdminHealth shows the component table and the recent portal errors.
func (a *App) handleAdminHealth(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	hostStats := hostmetrics.Collect()
	components := []health.Component{
		{
			Name:   "web portal",
			OK:     true,
			Detail: "running version " + a.Cfg.Version,
		},
		a.probeProsody(r.Context()),
		{
			Name:   "oauth client",
			OK:     a.Prosody.IsClientRegistered(),
			Detail: registrationDetail(a.Prosody.IsClientRegistered()),
		},
		{
			Name:   "metrics endpoint",
			OK:     a.Cfg.ShowMetrics,
			Detail: metricsDetail(a.Cfg.ShowMetrics, a.Cfg.MetricsToken != ""),
		},
		health.ProbeDomain(r.Context(), a.Cfg.Domain),
		health.ProbeTLS(r.Context(), a.Cfg.Domain),
		health.ProbeXMPPTLS(r.Context(), a.Cfg.Domain, a.Cfg.ProsodyEndpoint),
		health.ProbeS2S(r.Context(), a.Cfg.Domain, a.Cfg.ProsodyEndpoint),
		health.ProbePush(r.Context(), a.Cfg.Domain),
		health.ProbeTURN(r.Context(), a.Cfg.Domain, a.Cfg.ProsodyEndpoint),
		health.ProbeHTTPS(r.Context(), a.Cfg.Domain),
		a.probeStorage(hostStats),
		health.ProbeMemory(hostStats),
		health.ProbeCPU(hostStats),
		health.ProbePressure(hostStats),
	}

	healthy := true
	for _, component := range components {
		if !component.OK {
			healthy = false
		}
	}

	var recent []health.ErrorEntry
	if a.Errors != nil {
		recent = a.Errors.Recent()
	}

	page := a.newPage(w, r, sess, "Health", "health", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_health.html", adminHealthPage{
		PageData:     page,
		Components:   components,
		RecentErrors: recent,
		Uptime:       time.Since(a.Started).Round(time.Second).String(),
		Healthy:      healthy,
	})
}

// registrationDetail describes the OAuth client registration state.
func registrationDetail(registered bool) string {
	if registered {
		return "credentials obtained from the chat server"
	}
	return "not registered yet, the first sign in will register the portal"
}

// metricsDetail describes how the metrics endpoint is exposed.
func metricsDetail(enabled, tokenSet bool) string {
	switch {
	case !enabled:
		return "disabled by configuration"
	case tokenSet:
		return "exposed at /metrics and protected by a token"
	default:
		return "exposed at /metrics without a token"
	}
}

// probeStorage reports host disk fill for the root filesystem.
func (a *App) probeStorage(stats hostmetrics.Stats) health.Component {
	component := health.Component{Name: "storage"}
	if stats.DiskTotal == nil || stats.DiskUsedRatio == nil {
		component.Detail = "disk usage unavailable"
		return component
	}
	component.OK = *stats.DiskUsedRatio < 0.95
	used := "n/a"
	total := "n/a"
	if stats.DiskUsed != nil {
		used = webui.FormatBytes(*stats.DiskUsed)
	}
	if stats.DiskTotal != nil {
		total = webui.FormatBytes(*stats.DiskTotal)
	}
	component.Detail = used + " of " + total + " (" + webui.FormatPercent(*stats.DiskUsedRatio) + ")"
	if *stats.DiskUsedRatio >= 0.95 {
		component.Detail += ", critically full"
	}
	return component
}
