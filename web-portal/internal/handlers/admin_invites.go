package handlers

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

// lifetimeChoice is one invitation validity period.
type lifetimeChoice struct {
	Seconds int
	Label   string
}

// lifetimeChoices are the invitation validity periods offered by the portal.
var lifetimeChoices = []lifetimeChoice{
	{Seconds: 3600, Label: "One hour"},
	{Seconds: 12 * 3600, Label: "Twelve hours"},
	{Seconds: 86400, Label: "One day"},
	{Seconds: 7 * 86400, Label: "One week"},
	{Seconds: 28 * 86400, Label: "Four weeks"},
}

// inviteURL returns the address a guest opens to accept an invitation.
func (a *App) inviteURL(invite *prosody.AdminInviteInfo) string {
	if invite == nil {
		return ""
	}
	if invite.LandingPage != "" {
		return invite.LandingPage
	}
	return "https://" + a.Cfg.Domain + "/invite/" + url.PathEscape(invite.ID) + "/"
}

// inviteView pairs an invitation with the display values the templates need.
type inviteView struct {
	Invite  prosody.AdminInviteInfo
	URL     string
	Circles []string
	Expired bool
}

// adminInvitesPage is the data behind the invitation list.
type adminInvitesPage struct {
	webui.PageData
	Invites   []inviteView
	Circles   []prosody.AdminGroupInfo
	Roles     []roleChoice
	Lifetimes []lifetimeChoice
	Circle    string
}

// handleInvitations lists the outstanding invitations, newest first. Password
// reset links are left out because they belong to the account pages.
func (a *App) handleInvitations(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	invites, err := a.Prosody.ListInvites(r.Context(), sess.Token())
	if err != nil {
		a.failAPI(w, r, err)
		return
	}

	circles, err := a.Prosody.ListGroups(r.Context(), sess.Token())
	if err != nil {
		circles = nil
	}
	sortCircles(circles)

	names := map[string]string{}
	for _, circle := range circles {
		names[circle.ID] = circle.Name
	}

	views := make([]inviteView, 0, len(invites))
	for _, invite := range invites {
		if invite.IsReset {
			continue
		}
		views = append(views, a.newInviteView(invite, names))
	}
	slices.SortFunc(views, func(x, y inviteView) int {
		return y.Invite.CreatedAt.Compare(x.Invite.CreatedAt)
	})

	page := a.newPage(w, r, sess, "Invitations", "invites", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_invites.html", adminInvitesPage{
		PageData:  page,
		Invites:   views,
		Circles:   circles,
		Roles:     roleChoices,
		Lifetimes: lifetimeChoices,
	})
}

// newInviteView resolves the circle names of an invitation for display.
func (a *App) newInviteView(invite prosody.AdminInviteInfo, names map[string]string) inviteView {
	view := inviteView{
		Invite:  invite,
		URL:     a.inviteURL(&invite),
		Expired: !invite.Expires.IsZero() && invite.Expires.Before(time.Now()),
	}
	for _, id := range invite.GroupIDs {
		if name, ok := names[id]; ok {
			view.Circles = append(view.Circles, name)
			continue
		}
		view.Circles = append(view.Circles, id)
	}
	return view
}

// handleInvitationsSubmit revokes an invitation from the list page.
func (a *App) handleInvitationsSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	id := r.FormValue("revoke")
	if id == "" {
		http.Redirect(w, r, pathAdminInvitations, http.StatusSeeOther)
		return
	}

	if err := a.Prosody.DeleteInvite(r.Context(), sess.Token(), id); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminInvitations)
		return
	}
	a.recordAudit(r, sess, "invite.revoke", id, "")
	a.flashRedirect(w, r, sess, msgInviteRevoked, "success", pathAdminInvitations)
}

// adminCreateInvitePage is the data behind the invitation creation form.
type adminCreateInvitePage struct {
	webui.PageData
	Circles   []prosody.AdminGroupInfo
	Roles     []roleChoice
	Lifetimes []lifetimeChoice
	Circle    string
}

// handleCreateInviteForm shows the standalone invitation creation form.
func (a *App) handleCreateInviteForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	circles, err := a.Prosody.ListGroups(r.Context(), sess.Token())
	page := a.newPage(w, r, sess, "New invitation", "invites", webui.ShellAdmin)
	if err != nil {
		page.AddError("%s", "Circles could not be loaded. Create or repair circles before inviting.")
		circles = nil
	} else {
		sortCircles(circles)
	}
	a.render(w, r, http.StatusOK, "admin_create_invite.html", adminCreateInvitePage{
		PageData:  page,
		Circles:   circles,
		Roles:     roleChoices,
		Lifetimes: lifetimeChoices,
		Circle:    r.URL.Query().Get("circle"),
	})
}

// handleCreateInviteSubmit issues a new account or group invitation.
func (a *App) handleCreateInviteSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderError(w, r, http.StatusBadRequest, "Invalid form", "The submitted form could not be read.")
		return
	}

	circleIDs := nonEmpty(r.PostForm["circles"])
	if len(circleIDs) == 0 {
		a.flashRedirect(w, r, sess, "At least one circle must be selected.", "alert", pathAdminInviteNew)
		return
	}

	role := r.FormValue(fieldRole)
	if !validRole(role) {
		role = prosody.ScopeDefault
	}
	note := strings.TrimSpace(r.FormValue("note"))
	ttl := parseLifetime(r.FormValue("lifetime"))

	var invite *prosody.AdminInviteInfo
	var err error
	if r.FormValue("type") == "group" {
		invite, err = a.Prosody.CreateGroupInvite(r.Context(), sess.Token(), prosody.GroupInviteOptions{
			GroupIDs:  circleIDs,
			RoleNames: []string{role},
			TTL:       ttl,
			Note:      note,
		})
	} else {
		invite, err = a.Prosody.CreateAccountInvite(r.Context(), sess.Token(), prosody.AccountInviteOptions{
			GroupIDs:         circleIDs,
			RoleNames:        []string{role},
			RestrictUsername: strings.TrimSpace(r.FormValue("username")),
			TTL:              ttl,
			Note:             note,
		})
	}
	if err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminInvitations)
		return
	}

	a.recordAudit(r, sess, "invite.create", invite.ID, note)
	a.flashRedirect(w, r, sess, "Invitation created.", "success",
		"/admin/invitation/"+url.PathEscape(invite.ID))
}

// parseLifetime resolves a submitted validity period, falling back to one week.
func parseLifetime(value string) int {
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 7 * 86400
	}
	for _, choice := range lifetimeChoices {
		if choice.Seconds == seconds {
			return seconds
		}
	}
	return 7 * 86400
}

// nonEmpty drops blank entries from a set of submitted values.
func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// adminEditInvitePage is the data behind the single invitation page.
type adminEditInvitePage struct {
	webui.PageData
	Invite inviteView
}

// handleEditInviteForm shows one invitation together with its share link.
func (a *App) handleEditInviteForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	id := r.PathValue(paramID)
	invite, err := a.Prosody.GetInviteByID(r.Context(), sess.Token(), id)
	if err != nil {
		if prosody.StatusOf(err) == http.StatusNotFound {
			a.flashRedirect(w, r, sess, "No such invitation exists.", "alert", pathAdminInvitations)
			return
		}
		a.failAPI(w, r, err)
		return
	}

	names := map[string]string{}
	if circles, err := a.Prosody.ListGroups(r.Context(), sess.Token()); err == nil {
		for _, circle := range circles {
			names[circle.ID] = circle.Name
		}
	}

	page := a.newPage(w, r, sess, "Invitation", "invites", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_edit_invite.html", adminEditInvitePage{
		PageData: page,
		Invite:   a.newInviteView(*invite, names),
	})
}

// handleEditInviteSubmit revokes an invitation.
func (a *App) handleEditInviteSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	id := r.PathValue(paramID)
	if r.FormValue(fieldAction) != "revoke" {
		http.Redirect(w, r, "/admin/invitation/"+url.PathEscape(id), http.StatusSeeOther)
		return
	}

	if err := a.Prosody.DeleteInvite(r.Context(), sess.Token(), id); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminInvitations)
		return
	}
	a.recordAudit(r, sess, "invite.revoke", id, "")
	a.flashRedirect(w, r, sess, msgInviteRevoked, "success", pathAdminInvitations)
}
