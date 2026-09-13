package handlers

import (
	"net/http"

	"github.com/sudo-ivan/snikketx/web-portal/internal/bots"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

// mountAdminBots registers the operator facing bot control pages. They
// share the same management API as the user pages but see every owner
// and can lock the service down.
func (a *App) mountAdminBots(mux *http.ServeMux) {
	if a.Cfg == nil || !a.Cfg.BotsEnabled() {
		return
	}
	if a.Bots == nil {
		a.Bots = bots.New(a.Cfg.BotsEndpoint, a.Cfg.BotsHost, a.Cfg.BotsAdminToken)
	}
	mux.HandleFunc("GET "+pathAdminBots, a.handleAdminBotsPage)
	mux.HandleFunc("POST "+pathAdminBots+"/lockdown", a.handleAdminBotsLockdown)
	mux.HandleFunc("POST "+pathAdminBots+"/{name}/toggle", a.handleAdminBotsToggle)
	mux.HandleFunc("POST "+pathAdminBots+"/{name}/delete", a.handleAdminBotsDelete)
}

// adminBotsData carries the full bot list plus the audit filter state.
type adminBotsData struct {
	webui.PageData
	Bots      []bots.Bot
	Open      bool
	Audit     []bots.AuditEntry
	AuditBot  string
	Enabled   bool
	ServiceUp bool
}

// renderAdminBots draws the operator page. filter narrows the audit
// trail to one bot.
func (a *App) renderAdminBots(w http.ResponseWriter, r *http.Request, sess session.Data, filter string, status int) {
	data := adminBotsData{AuditBot: filter, Enabled: true, ServiceUp: true, Open: true}

	list, open, err := a.Bots.List(r.Context(), "")
	if err != nil {
		a.recordError(r, err)
		data.ServiceUp = false
		data.Errors = append(data.Errors, "The bot service could not be reached. Is mod_snikketx_bots enabled?")
	} else {
		data.Bots = list
		data.Open = open
		if entries, aerr := a.Bots.Audit(r.Context(), filter, 200); aerr == nil {
			data.Audit = entries
		}
	}

	data.PageData = a.newPage(w, r, sess, "Bots", "bots", webui.ShellAdmin)
	a.render(w, r, status, "admin_bots.html", data)
}

func (a *App) handleAdminBotsPage(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	a.renderAdminBots(w, r, sess, r.URL.Query().Get("bot"), http.StatusOK)
}

// handleAdminBotsLockdown flips the global registration lock.
func (a *App) handleAdminBotsLockdown(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	locked := r.FormValue("lock") == "1"
	open, err := a.Bots.SetLockdown(r.Context(), locked)
	if err != nil {
		a.flashRedirect(w, r, sess, botErrorMessage(err), "alert", pathAdminBots)
		return
	}
	a.recordAudit(r, sess, "bots.lockdown", map[bool]string{true: "locked", false: "unlocked"}[locked], "")
	if open {
		a.flashRedirect(w, r, sess, "Bot registrations reopened.", "success", pathAdminBots)
	} else {
		a.flashRedirect(w, r, sess, "Bot registrations locked down.", "success", pathAdminBots)
	}
}

// handleAdminBotsToggle flips a bot's disabled flag. Admin can manage
// any bot: the management token carries that right already.
func (a *App) handleAdminBotsToggle(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	bot, ok := a.botExists(w, r, sess, name)
	if !ok {
		return
	}
	if err := a.Bots.SetDisabled(r.Context(), name, !bot.Disabled); err != nil {
		a.flashRedirect(w, r, sess, botErrorMessage(err), "alert", pathAdminBots)
		return
	}
	action := "enabled"
	if !bot.Disabled {
		action = "disabled"
	}
	a.recordAudit(r, sess, "bots."+action, name, "admin")
	a.flashRedirect(w, r, sess, "Bot "+name+" "+action+".", "success", pathAdminBots)
}

// handleAdminBotsDelete removes any bot.
func (a *App) handleAdminBotsDelete(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	if _, ok := a.botExists(w, r, sess, name); !ok {
		return
	}
	if err := a.Bots.Delete(r.Context(), name); err != nil {
		a.flashRedirect(w, r, sess, botErrorMessage(err), "alert", pathAdminBots)
		return
	}
	a.recordAudit(r, sess, "bots.delete", name, "admin")
	a.flashRedirect(w, r, sess, "Bot "+name+" deleted.", "success", pathAdminBots)
}

// botExists resolves a bot for admin operations: unlike botOwnedBy it
// does not restrict to the caller's own bots.
func (a *App) botExists(w http.ResponseWriter, r *http.Request, sess session.Data, name string) (*bots.Bot, bool) {
	list, _, err := a.Bots.List(r.Context(), "")
	if err != nil {
		a.recordError(r, err)
		a.flashRedirect(w, r, sess, "The bot service could not be reached. Try again later.", "alert", pathAdminBots)
		return nil, false
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i], true
		}
	}
	a.flashRedirect(w, r, sess, "Unknown bot.", "alert", pathAdminBots)
	return nil, false
}
