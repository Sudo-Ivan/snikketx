package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/updater"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

type logSource struct {
	ID    string
	Label string
}

type logsPage struct {
	webui.PageData
	Sources   []logSource
	Service   string
	Tail      int
	Lines     []string
	Truncated bool
	Note      string
}

func defaultLogSources() []logSource {
	return []logSource{
		{ID: "portal-errors", Label: "Portal errors (in memory)"},
		{ID: "snikket_server", Label: "Chat server (Prosody)"},
		{ID: "snikket_portal", Label: "Web portal"},
		{ID: "ravenguard", Label: "Edge (RavenGuard)"},
		{ID: "snikket_updater", Label: "Updater"},
		{ID: "snikket_backup", Label: "Backup"},
		{ID: "snikket_certs", Label: "Cert manager"},
	}
}

func (a *App) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	sources := defaultLogSources()
	service := strings.TrimSpace(r.URL.Query().Get("service"))
	if service == "" {
		service = "snikket_server"
	}
	tail := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("tail")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			tail = n
		}
	}
	if tail < 1 {
		tail = 100
	}
	if tail > 500 {
		tail = 500
	}

	page := a.newPage(w, r, sess, "Logs", "logs", webui.ShellAdmin)
	view := logsPage{
		PageData: page,
		Sources:  sources,
		Service:  service,
		Tail:     tail,
	}

	switch service {
	case "portal-errors":
		view.Lines = a.portalErrorLines(tail)
	default:
		if a.Updater == nil || !a.Updater.Enabled() {
			view.Note = "Updater is not configured, so container logs are unavailable. Portal errors still work."
			view.Service = "portal-errors"
			view.Lines = a.portalErrorLines(tail)
			break
		}
		res, err := a.Updater.Logs(r.Context(), service, tail)
		if err != nil {
			view.Note = err.Error()
			break
		}
		if len(res.Services) > 0 {
			view.Sources = mergeLogSources(res.Services)
		}
		view.Lines = res.Lines
		view.Truncated = res.Truncated
		if res.Label != "" {
			for i := range view.Sources {
				if view.Sources[i].ID == res.Service {
					view.Sources[i].Label = res.Label
				}
			}
		}
	}

	a.recordAudit(r, sess, "logs.view", service, fmt.Sprintf("tail=%d", tail))
	a.render(w, r, http.StatusOK, "admin_logs.html", view)
}

func (a *App) portalErrorLines(limit int) []string {
	if a.Errors == nil {
		return nil
	}
	entries := a.Errors.Recent()
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		when := e.When.UTC().Format(time.RFC3339)
		line := when + " " + e.Message
		if e.RequestID != "" {
			line += " request_id=" + e.RequestID
		}
		out = append(out, line)
	}
	return out
}

func mergeLogSources(remote []updater.LogService) []logSource {
	out := []logSource{{ID: "portal-errors", Label: "Portal errors (in memory)"}}
	for _, svc := range remote {
		out = append(out, logSource{ID: svc.ID, Label: svc.Label})
	}
	return out
}
