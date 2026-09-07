package handlers

import (
	"encoding/json"
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

type logLineView struct {
	Time    string
	Display string
	Text    string
}

type logsPage struct {
	webui.PageData
	Sources   []logSource
	Service   string
	Tail      int
	Lines     []string
	Entries   []logLineView
	Truncated bool
	FetchedAt string
	Note      string
}

type logsJSONResponse struct {
	Service   string        `json:"service"`
	Tail      int           `json:"tail"`
	Entries   []logLineView `json:"entries"`
	Truncated bool          `json:"truncated"`
	FetchedAt string        `json:"fetched_at"`
	Note      string        `json:"note,omitempty"`
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
	wantJSON := strings.EqualFold(r.URL.Query().Get("format"), "json") ||
		strings.Contains(r.Header.Get("Accept"), "application/json")

	page := a.newPage(w, r, sess, "Logs", "logs", webui.ShellAdmin)
	view := logsPage{
		PageData:  page,
		Sources:   sources,
		Service:   service,
		Tail:      tail,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}

	switch service {
	case "portal-errors":
		view.Entries = a.portalErrorEntries(tail)
		view.Lines = entriesToLines(view.Entries)
	default:
		if a.Updater == nil || !a.Updater.Enabled() {
			view.Note = "Updater is not configured, so container logs are unavailable. Portal errors still work."
			view.Service = "portal-errors"
			view.Entries = a.portalErrorEntries(tail)
			view.Lines = entriesToLines(view.Entries)
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
		view.Entries = updaterEntriesToView(res.Entries, res.Lines)
		view.Truncated = res.Truncated
		if res.FetchedAt != "" {
			view.FetchedAt = res.FetchedAt
		}
		if res.Label != "" {
			for i := range view.Sources {
				if view.Sources[i].ID == res.Service {
					view.Sources[i].Label = res.Label
				}
			}
		}
	}

	if wantJSON {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(logsJSONResponse{
			Service:   view.Service,
			Tail:      view.Tail,
			Entries:   view.Entries,
			Truncated: view.Truncated,
			FetchedAt: view.FetchedAt,
			Note:      view.Note,
		})
		return
	}

	a.recordAudit(r, sess, "logs.view", service, fmt.Sprintf("tail=%d", tail))
	a.render(w, r, http.StatusOK, "admin_logs.html", view)
}

func (a *App) portalErrorEntries(limit int) []logLineView {
	if a.Errors == nil {
		return nil
	}
	entries := a.Errors.Recent()
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]logLineView, 0, len(entries))
	for _, e := range entries {
		when := e.When.UTC()
		text := e.Message
		if e.RequestID != "" {
			text += " request_id=" + e.RequestID
		}
		out = append(out, logLineView{
			Time:    when.Format(time.RFC3339),
			Display: when.Format("15:04:05"),
			Text:    text,
		})
	}
	return out
}

func entriesToLines(entries []logLineView) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Display != "" {
			out = append(out, e.Display+"  "+e.Text)
			continue
		}
		out = append(out, e.Text)
	}
	return out
}

func updaterEntriesToView(entries []updater.LogEntry, fallback []string) []logLineView {
	if len(entries) > 0 {
		out := make([]logLineView, 0, len(entries))
		for _, e := range entries {
			out = append(out, logLineView{Time: e.Time, Display: e.Display, Text: e.Text})
		}
		return out
	}
	out := make([]logLineView, 0, len(fallback))
	for _, line := range fallback {
		out = append(out, logLineView{Text: line})
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
