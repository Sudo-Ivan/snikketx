package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/health"
	"github.com/sudo-ivan/snikketx/web-portal/internal/hostmetrics"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

// roleChoice is one access level offered when editing a user or an invitation.
type roleChoice struct {
	Value string
	Label string
	Hint  string
}

// roleChoices are the access levels the portal exposes.
var roleChoices = []roleChoice{
	{Value: prosody.ScopeRestricted, Label: "Limited", Hint: "Can only chat with members of their circles."},
	{Value: prosody.ScopeDefault, Label: "Normal user", Hint: "Can add contacts and join chats freely."},
	{Value: prosody.ScopeAdmin, Label: "Administrator", Hint: "Full access to this portal and the chat server."},
}

// validRole reports whether value is one of the offered access levels.
func validRole(value string) bool {
	for _, choice := range roleChoices {
		if choice.Value == value {
			return true
		}
	}
	return false
}

// mountAdmin registers the administration routes.
func (a *App) mountAdmin(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /admin/{$}", a.handleAdminHome)

	mux.HandleFunc("GET "+pathAdminUsers, a.handleAdminUsers)
	mux.HandleFunc("GET /admin/user/{localpart}/{$}", a.handleEditUserForm)
	mux.HandleFunc("POST /admin/user/{localpart}/{$}", a.handleEditUserSubmit)
	mux.HandleFunc("GET /admin/user/{localpart}/delete", a.handleDeleteUserForm)
	mux.HandleFunc("POST /admin/user/{localpart}/delete", a.handleDeleteUserSubmit)
	mux.HandleFunc("GET /admin/user/{localpart}/debug", a.handleDebugUser)
	mux.HandleFunc("GET /admin/users/password-reset/{id}", a.handleResetLinkForm)
	mux.HandleFunc("POST /admin/users/password-reset/{id}", a.handleResetLinkSubmit)

	mux.HandleFunc("GET "+pathAdminInvitations, a.handleInvitations)
	mux.HandleFunc("POST "+pathAdminInvitations, a.handleInvitationsSubmit)
	mux.HandleFunc("GET "+pathAdminInviteNew, a.handleCreateInviteForm)
	mux.HandleFunc("POST "+pathAdminInviteNew, a.handleCreateInviteSubmit)
	mux.HandleFunc("GET /admin/invitation/{id}", a.handleEditInviteForm)
	mux.HandleFunc("POST /admin/invitation/{id}", a.handleEditInviteSubmit)

	mux.HandleFunc("GET "+pathAdminCircles, a.handleCircles)
	mux.HandleFunc("GET "+pathAdminCircleNew, a.handleCreateCircleForm)
	mux.HandleFunc("POST "+pathAdminCircleNew, a.handleCreateCircleSubmit)
	mux.HandleFunc("GET /admin/circle/{id}", a.handleEditCircleForm)
	mux.HandleFunc("POST /admin/circle/{id}", a.handleEditCircleSubmit)
	mux.HandleFunc("GET /admin/circle/{id}/delete", a.handleDeleteCircleForm)
	mux.HandleFunc("POST /admin/circle/{id}/delete", a.handleDeleteCircleSubmit)
	mux.HandleFunc("GET /admin/circle/{id}/add_chat", a.handleAddChatForm)
	mux.HandleFunc("POST /admin/circle/{id}/add_chat", a.handleAddChatSubmit)

	mux.HandleFunc("GET "+pathAdminSystem, a.handleSystemForm)
	mux.HandleFunc("POST "+pathAdminSystem, a.handleSystemSubmit)
	mux.HandleFunc("GET "+pathAdminHealth, a.handleAdminHealth)
	mux.HandleFunc("GET /admin/audit/export.csv", a.handleAuditExportCSV)
	mux.HandleFunc("GET /admin/audit/export.json", a.handleAuditExportJSON)
	mux.HandleFunc("GET /admin/audit/", a.handleAuditLog)
	mux.HandleFunc("GET "+pathAdminMUCs, a.handleMUCs)
	mux.HandleFunc("POST "+pathAdminMUCs, a.handleMUCsSubmit)
	mux.HandleFunc("GET "+pathAdminMUCNew, a.handleCreateMUCForm)
	mux.HandleFunc("POST "+pathAdminMUCNew, a.handleCreateMUCSubmit)
	mux.HandleFunc("GET /admin/muc/{localpart}", a.handleMUCDetail)
	mux.HandleFunc("POST /admin/muc/{localpart}", a.handleMUCDetailSubmit)

	a.mountAdminOps(mux)
}

// serverMetrics is the subset of the Prosody metrics document the portal shows.
type serverMetrics struct {
	Available bool
	Devices   *int64
	Memory    *int64
	Uploads   *int64
	CPU       *float64
	Active1d  *int64
	Active7d  *int64
	Active30d *int64
}

// parseServerMetrics extracts the counters the portal displays from the loosely
// typed metrics document returned by the chat server.
func parseServerMetrics(raw map[string]any) serverMetrics {
	out := serverMetrics{}
	if len(raw) == 0 {
		return out
	}
	out.Available = true

	out.Devices = intFrom(raw["c2s"])
	out.Memory = intFrom(raw["memory"])
	out.Uploads = intFrom(raw["uploads"])

	if cpu, ok := raw["cpu"].(map[string]any); ok {
		value, valueOK := floatFrom(cpu["value"])
		since, sinceOK := floatFrom(cpu["since"])
		now := float64(time.Now().Unix())
		if valueOK && sinceOK && now > since {
			ratio := value / (now - since)
			out.CPU = &ratio
		}
	}

	if users, ok := raw["users"].(map[string]any); ok {
		out.Active1d = intFrom(users["active_1d"])
		out.Active7d = intFrom(users["active_7d"])
		out.Active30d = intFrom(users["active_30d"])
	}
	return out
}

// intFrom converts a JSON number into an optional integer.
func intFrom(value any) *int64 {
	n, ok := floatFrom(value)
	if !ok {
		return nil
	}
	rounded := int64(n)
	return &rounded
}

// floatFrom converts a JSON number into a float.
func floatFrom(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		parsed, err := n.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

// adminMetrics fetches the chat server metrics, treating an unsupported or
// forbidden endpoint as simply having nothing to show.
func (a *App) adminMetrics(ctx context.Context, token string) map[string]any {
	if !a.Cfg.ShowMetrics {
		return nil
	}
	raw, err := a.Prosody.GetSystemMetrics(ctx, token)
	if err != nil {
		return nil
	}
	if a.Metrics != nil && len(raw) > 0 {
		a.Metrics.SetProsodyCache(raw)
	}
	return raw
}

// sloItem is one green/yellow/red tile on the admin home SLO strip.
type sloItem struct {
	Name   string
	Level  string
	Label  string
	Detail string
	Href   string
}

// adminHomePage is the data behind the admin overview.
type adminHomePage struct {
	webui.PageData
	Users        int
	Admins       int
	Disabled     int
	Invitations  int
	Circles      int
	Metrics      serverMetrics
	Uptime       string
	RecentErrors int
	Degraded     []string
	Healthy      bool
	HealthyOK    int
	HealthyTotal int
	HealthRows   []health.Component
	SLO          []sloItem
}

// handleAdminHome shows the instance overview with the account, invitation and
// circle counts, plus a compact health snapshot.
func (a *App) handleAdminHome(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	data := adminHomePage{Uptime: time.Since(a.Started).Round(time.Second).String()}

	users, err := a.Prosody.ListUsers(r.Context(), sess.Token())
	if err != nil {
		a.failAPI(w, r, err)
		return
	}
	data.Users = len(users)
	for _, user := range users {
		if user.HasAdminRole() {
			data.Admins++
		}
		if !user.Enabled {
			data.Disabled++
		}
	}

	if invites, err := a.Prosody.ListInvites(r.Context(), sess.Token()); err == nil {
		for _, invite := range invites {
			if !invite.IsReset {
				data.Invitations++
			}
		}
	} else {
		data.Degraded = append(data.Degraded, "Invitations could not be counted.")
	}

	if circles, err := a.Prosody.ListGroups(r.Context(), sess.Token()); err == nil {
		data.Circles = len(circles)
	} else {
		data.Degraded = append(data.Degraded, "Circles could not be counted.")
	}

	data.Metrics = parseServerMetrics(a.adminMetrics(r.Context(), sess.Token()))
	if a.Errors != nil {
		data.RecentErrors = len(a.Errors.Recent())
	}

	slo, probes := a.buildHealthSLO(r.Context())
	data.SLO = slo

	hostStats := hostmetrics.Collect()
	rows := []health.Component{
		{Name: "web portal", OK: true, Detail: "running version " + a.Cfg.Version},
		probes.prosody,
		probes.https,
		probes.xmpp,
		probes.s2s,
		probes.push,
		probes.turn,
		health.ProbeMemory(hostStats),
	}
	data.Healthy = true
	for _, row := range rows {
		data.HealthyTotal++
		if row.OK {
			data.HealthyOK++
		} else {
			data.Healthy = false
		}
	}
	data.HealthRows = rows

	data.PageData = a.newPage(w, r, sess, "Admin", "home", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_home.html", data)
}

type homeProbes struct {
	prosody health.Component
	https   health.Component
	xmpp    health.Component
	s2s     health.Component
	push    health.Component
	turn    health.Component
}

// buildHealthSLO runs network probes once for the home SLO strip and summary rows.
func (a *App) buildHealthSLO(ctx context.Context) ([]sloItem, homeProbes) {
	var (
		probes      homeProbes
		updaterItem sloItem
	)
	var wg sync.WaitGroup
	wg.Go(func() { probes.prosody = a.probeProsody(ctx) })
	wg.Go(func() { probes.https = health.ProbeTLS(ctx, a.Cfg.Domain) })
	wg.Go(func() { probes.xmpp = health.ProbeXMPPTLS(ctx, a.Cfg.Domain, a.Cfg.ProsodyEndpoint) })
	wg.Go(func() { probes.s2s = health.ProbeS2S(ctx, a.Cfg.Domain, a.Cfg.ProsodyEndpoint) })
	wg.Go(func() { probes.push = health.ProbePush(ctx, a.Cfg.Domain) })
	wg.Go(func() { probes.turn = health.ProbeTURN(ctx, a.Cfg.Domain, a.Cfg.ProsodyEndpoint) })
	wg.Go(func() { updaterItem = a.updaterSLO(ctx) })
	wg.Wait()

	s2sPush := mergeProbeComponents("S2S / Push", probes.s2s, probes.push)

	return []sloItem{
		componentSLO("Prosody", pathAdminHealth, probes.prosody, "Down"),
		certSLO("HTTPS", "/admin/certs/", probes.https),
		certSLO("XMPP TLS", "/admin/certs/", probes.xmpp),
		componentSLO("S2S / Push", pathAdminHealth, s2sPush, "Blocked"),
		componentSLO("TURN", pathAdminHealth, probes.turn, "Blocked"),
		updaterItem,
	}, probes
}

func componentSLO(name, href string, probe health.Component, badLabel string) sloItem {
	item := sloItem{
		Name:   name,
		Href:   href,
		Detail: health.FormatComponentDetail(probe),
	}
	if probe.OK {
		item.Level = "ok"
		item.Label = "OK"
		return item
	}
	item.Level = "bad"
	item.Label = badLabel
	return item
}

func certSLO(name, href string, probe health.Component) sloItem {
	item := sloItem{
		Name:   name,
		Href:   href,
		Detail: health.FormatComponentDetail(probe),
	}
	switch {
	case !probe.OK:
		item.Level = "bad"
		item.Label = "Bad"
	case strings.Contains(probe.Detail, "renew soon"):
		item.Level = "warn"
		item.Label = "Renew"
	default:
		item.Level = "ok"
		item.Label = "OK"
	}
	return item
}

func mergeProbeComponents(name string, parts ...health.Component) health.Component {
	out := health.Component{Name: name, OK: true}
	details := make([]string, 0, len(parts))
	hints := make([]string, 0, len(parts))
	var maxLatency time.Duration
	for _, part := range parts {
		if !part.OK {
			out.OK = false
		}
		if d := strings.TrimSpace(part.Detail); d != "" {
			details = append(details, part.Name+": "+d)
		}
		if h := strings.TrimSpace(part.Hint); h != "" && !part.OK {
			hints = append(hints, h)
		}
		if part.Latency != "" {
			if lat, err := time.ParseDuration(part.Latency); err == nil && lat > maxLatency {
				maxLatency = lat
			}
		}
	}
	out.Detail = strings.Join(details, " | ")
	out.Hint = strings.Join(hints, " ")
	if maxLatency > 0 {
		out.Latency = maxLatency.Round(time.Millisecond).String()
	}
	return out
}

func (a *App) updaterSLO(ctx context.Context) sloItem {
	updater := sloItem{Name: "Updater", Href: pathAdminUpdates}
	switch {
	case a.Updater == nil || !a.Updater.Enabled():
		updater.Level = "warn"
		updater.Label = "Off"
		updater.Detail = "not configured"
	default:
		if err := a.Updater.Healthz(ctx); err != nil {
			updater.Level = "bad"
			updater.Label = "Down"
			updater.Detail = err.Error()
		} else if status, err := a.Updater.Status(ctx); err != nil {
			updater.Level = "warn"
			updater.Label = "Warn"
			updater.Detail = err.Error()
		} else if status.Status == "busy" || status.Phase == "checking" || status.Phase == "applying" {
			updater.Level = "warn"
			updater.Label = "Busy"
			updater.Detail = firstNonEmpty(status.Phase, status.Status)
		} else if status.Available {
			updater.Level = "warn"
			updater.Label = "Update"
			updater.Detail = "container updates available"
		} else {
			updater.Level = "ok"
			updater.Label = "OK"
			updater.Detail = "reachable"
		}
	}
	return updater
}
