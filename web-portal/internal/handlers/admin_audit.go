package handlers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

// auditEventView is one row on the audit log page.
type auditEventView struct {
	When      time.Time `json:"when"`
	Source    string    `json:"source"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Request   string    `json:"request_id,omitempty"`
}

// auditFilter holds shareable audit search query parameters.
type auditFilter struct {
	Query  string
	Source string
	Actor  string
	Action string
}

func parseAuditFilter(r *http.Request) auditFilter {
	q := r.URL.Query()
	return auditFilter{
		Query:  strings.TrimSpace(q.Get("q")),
		Source: strings.TrimSpace(q.Get("source")),
		Actor:  strings.TrimSpace(q.Get("actor")),
		Action: strings.TrimSpace(q.Get("action")),
	}
}

func (f auditFilter) values(page int) url.Values {
	values := url.Values{}
	if f.Query != "" {
		values.Set("q", f.Query)
	}
	if f.Source != "" {
		values.Set("source", f.Source)
	}
	if f.Actor != "" {
		values.Set("actor", f.Actor)
	}
	if f.Action != "" {
		values.Set("action", f.Action)
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	return values
}

func (f auditFilter) pageURL(page int) string {
	encoded := f.values(page).Encode()
	if encoded == "" {
		return "/admin/audit/"
	}
	return "/admin/audit/?" + encoded
}

func (f auditFilter) shareURL() string {
	return f.pageURL(1)
}

func (f auditFilter) exportURL(format string) string {
	encoded := f.values(0).Encode()
	path := "/admin/audit/export." + format
	if encoded == "" {
		return path
	}
	return path + "?" + encoded
}

func (f auditFilter) matches(ev auditEventView) bool {
	if f.Source != "" && !strings.EqualFold(ev.Source, f.Source) {
		return false
	}
	if f.Actor != "" && !strings.Contains(strings.ToLower(ev.Actor), strings.ToLower(f.Actor)) {
		return false
	}
	if f.Action != "" && !strings.Contains(strings.ToLower(ev.Action), strings.ToLower(f.Action)) {
		return false
	}
	if f.Query == "" {
		return true
	}
	needle := strings.ToLower(f.Query)
	hay := strings.ToLower(strings.Join([]string{
		ev.Source, ev.Actor, ev.Action, ev.Target, ev.Detail, ev.IP, ev.UserAgent, ev.Request,
	}, " "))
	return strings.Contains(hay, needle)
}

// adminAuditPage is the data behind the audit log.
type adminAuditPage struct {
	webui.PageData
	Filter     auditFilter
	Query      string
	Source     string
	Actor      string
	Action     string
	ShareURL   string
	ExportCSV  string
	ExportJSON string
	Events     []auditEventView
	Note       string
	Page       int
	PageSize   int
	Total      int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevURL    string
	NextURL    string
}

const auditPageSize = 50
const auditFetchLimit = 500

func (a *App) collectAuditEvents(ctx context.Context, token string, filter auditFilter) ([]auditEventView, string) {
	events := make([]auditEventView, 0, 128)
	search := filter.Query
	if a.Audit != nil {
		for _, ev := range a.Audit.Recent(auditFetchLimit, search) {
			row := auditEventView{
				When:      ev.When,
				Source:    ev.Source,
				Actor:     ev.Actor,
				Action:    ev.Action,
				Target:    ev.Target,
				Detail:    ev.Detail,
				IP:        ev.IP,
				UserAgent: ev.UserAgent,
				Request:   ev.RequestID,
			}
			if filter.matches(row) {
				events = append(events, row)
			}
		}
	}

	note := ""
	if prosodyEvents, err := a.Prosody.ListAuditEvents(ctx, token, auditFetchLimit, search); err != nil {
		note = "Chat server audit feed unavailable: " + apiErrorMessage(err)
	} else {
		for _, ev := range prosodyEvents {
			when := time.Unix(ev.When, 0).UTC()
			row := auditEventView{
				When:   when,
				Source: stringOr(ev.Source, "prosody"),
				Actor:  ev.Actor,
				Action: ev.Action,
				Target: ev.Target,
				Detail: ev.Detail,
				IP:     ev.IP,
			}
			if filter.matches(row) {
				events = append(events, row)
			}
		}
	}

	slices.SortFunc(events, func(x, y auditEventView) int {
		return y.When.Compare(x.When)
	})
	return events, note
}

// handleAuditLog shows searchable portal and Prosody audit events.
func (a *App) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	filter := parseAuditFilter(r)
	pageNum := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			pageNum = n
		}
	}

	events, note := a.collectAuditEvents(r.Context(), sess.Token(), filter)

	total := len(events)
	totalPages := total / auditPageSize
	if total%auditPageSize != 0 {
		totalPages++
	}
	if totalPages < 1 {
		totalPages = 1
	}
	if pageNum > totalPages {
		pageNum = totalPages
	}

	start := (pageNum - 1) * auditPageSize
	end := start + auditPageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	pageEvents := events[start:end]

	page := a.newPage(w, r, sess, "Audit log", "audit", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_audit.html", adminAuditPage{
		PageData:   page,
		Filter:     filter,
		Query:      filter.Query,
		Source:     filter.Source,
		Actor:      filter.Actor,
		Action:     filter.Action,
		ShareURL:   "https://" + a.Cfg.Domain + filter.shareURL(),
		ExportCSV:  filter.exportURL("csv"),
		ExportJSON: filter.exportURL("json"),
		Events:     pageEvents,
		Note:       note,
		Page:       pageNum,
		PageSize:   auditPageSize,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    pageNum > 1,
		HasNext:    pageNum < totalPages,
		PrevURL:    filter.pageURL(pageNum - 1),
		NextURL:    filter.pageURL(pageNum + 1),
	})
}

func (a *App) handleAuditExportCSV(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	filter := parseAuditFilter(r)
	events, _ := a.collectAuditEvents(r.Context(), sess.Token(), filter)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="snikketx-audit.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"when", "source", "actor", "action", "target", "ip", "user_agent", "detail", "request_id"})
	for _, ev := range events {
		_ = writer.Write([]string{
			ev.When.UTC().Format(time.RFC3339),
			ev.Source,
			ev.Actor,
			ev.Action,
			ev.Target,
			ev.IP,
			ev.UserAgent,
			ev.Detail,
			ev.Request,
		})
	}
	writer.Flush()
}

func (a *App) handleAuditExportJSON(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	filter := parseAuditFilter(r)
	events, _ := a.collectAuditEvents(r.Context(), sess.Token(), filter)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="snikketx-audit.json"`)
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"filter": filter,
		"count":  len(events),
		"events": events,
	})
}
