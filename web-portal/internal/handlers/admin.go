package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/health"
	"github.com/sudo-ivan/snikketx/web-portal/internal/hostmetrics"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
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

// mountAdmin registers the administration routes.
func (a *App) mountAdmin(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /admin/{$}", a.handleAdminHome)

	mux.HandleFunc("GET /admin/users", a.handleAdminUsers)
	mux.HandleFunc("GET /admin/user/{localpart}/{$}", a.handleEditUserForm)
	mux.HandleFunc("POST /admin/user/{localpart}/{$}", a.handleEditUserSubmit)
	mux.HandleFunc("GET /admin/user/{localpart}/delete", a.handleDeleteUserForm)
	mux.HandleFunc("POST /admin/user/{localpart}/delete", a.handleDeleteUserSubmit)
	mux.HandleFunc("GET /admin/user/{localpart}/debug", a.handleDebugUser)
	mux.HandleFunc("GET /admin/users/password-reset/{id}", a.handleResetLinkForm)
	mux.HandleFunc("POST /admin/users/password-reset/{id}", a.handleResetLinkSubmit)

	mux.HandleFunc("GET /admin/invitations", a.handleInvitations)
	mux.HandleFunc("POST /admin/invitations", a.handleInvitationsSubmit)
	mux.HandleFunc("GET /admin/invitation/-/new", a.handleCreateInviteForm)
	mux.HandleFunc("POST /admin/invitation/-/new", a.handleCreateInviteSubmit)
	mux.HandleFunc("GET /admin/invitation/{id}", a.handleEditInviteForm)
	mux.HandleFunc("POST /admin/invitation/{id}", a.handleEditInviteSubmit)

	mux.HandleFunc("GET /admin/circles", a.handleCircles)
	mux.HandleFunc("GET /admin/circle/-/new", a.handleCreateCircleForm)
	mux.HandleFunc("POST /admin/circle/-/new", a.handleCreateCircleSubmit)
	mux.HandleFunc("GET /admin/circle/{id}", a.handleEditCircleForm)
	mux.HandleFunc("POST /admin/circle/{id}", a.handleEditCircleSubmit)
	mux.HandleFunc("GET /admin/circle/{id}/delete", a.handleDeleteCircleForm)
	mux.HandleFunc("POST /admin/circle/{id}/delete", a.handleDeleteCircleSubmit)
	mux.HandleFunc("GET /admin/circle/{id}/add_chat", a.handleAddChatForm)
	mux.HandleFunc("POST /admin/circle/{id}/add_chat", a.handleAddChatSubmit)

	mux.HandleFunc("GET /admin/system/", a.handleSystemForm)
	mux.HandleFunc("POST /admin/system/", a.handleSystemSubmit)
	mux.HandleFunc("GET /admin/health/", a.handleAdminHealth)
	mux.HandleFunc("GET /admin/audit/", a.handleAuditLog)
	mux.HandleFunc("GET /admin/mucs", a.handleMUCs)
	mux.HandleFunc("POST /admin/mucs", a.handleMUCsSubmit)
	mux.HandleFunc("GET /admin/muc/-/new", a.handleCreateMUCForm)
	mux.HandleFunc("POST /admin/muc/-/new", a.handleCreateMUCSubmit)
	mux.HandleFunc("GET /admin/muc/{localpart}", a.handleMUCDetail)
	mux.HandleFunc("POST /admin/muc/{localpart}", a.handleMUCDetailSubmit)
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
}

// handleAdminHome shows the instance overview with the account, invitation and
// circle counts, plus the live connection count when metrics are enabled.
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

	data.PageData = a.newPage(w, r, sess, "Admin", "home", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_home.html", data)
}

// adminUsersPage is the data behind the account list.
type adminUsersPage struct {
	webui.PageData
	Users   []prosody.AdminUserInfo
	Circles []prosody.AdminGroupInfo
	Roles   []roleChoice
}

// handleAdminUsers lists every account on the virtual host.
func (a *App) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	users, err := a.Prosody.ListUsers(r.Context(), sess.Token())
	if err != nil {
		a.failAPI(w, r, err)
		return
	}
	slices.SortFunc(users, func(x, y prosody.AdminUserInfo) int {
		return strings.Compare(x.Localpart, y.Localpart)
	})

	circles, err := a.Prosody.ListGroups(r.Context(), sess.Token())
	if err != nil {
		circles = nil
	}
	sortCircles(circles)

	page := a.newPage(w, r, sess, "Users", "users", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_users.html", adminUsersPage{
		PageData: page,
		Users:    users,
		Circles:  circles,
		Roles:    roleChoices,
	})
}

// adminEditUserPage is the data behind the account editor.
type adminEditUserPage struct {
	webui.PageData
	Target      *prosody.AdminUserInfo
	DisplayName string
	Role        string
	Roles       []roleChoice
}

// handleEditUserForm shows the account editor.
func (a *App) handleEditUserForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	localpart := r.PathValue("localpart")
	target, err := a.Prosody.GetUserByLocalpart(r.Context(), sess.Token(), localpart)
	if err != nil {
		a.failAPI(w, r, err)
		return
	}

	role := prosody.ScopeDefault
	if len(target.Roles) > 0 {
		role = target.Roles[0]
	}

	page := a.newPage(w, r, sess, "Edit "+localpart, "users", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_edit_user.html", adminEditUserPage{
		PageData:    page,
		Target:      target,
		DisplayName: target.DisplayName,
		Role:        role,
		Roles:       roleChoices,
	})
}

// handleEditUserSubmit applies the account editor actions: saving the profile,
// unlocking or restoring the account, or issuing a password reset link.
func (a *App) handleEditUserSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	localpart := r.PathValue("localpart")
	target := "/admin/user/" + url.PathEscape(localpart) + "/"

	switch r.FormValue("action") {
	case "create_reset":
		invite, err := a.Prosody.CreatePasswordResetInvite(r.Context(), sess.Token(), localpart, resetInviteTTL)
		if err != nil {
			a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", target)
			return
		}
		a.recordAudit(r, sess, "user.reset_create", localpart, "")
		a.flashRedirect(w, r, sess, "Password reset link created.", "success",
			"/admin/users/password-reset/"+url.PathEscape(invite.ID))
		return

	case "enable", "restore":
		if err := a.Prosody.EnableUserAccount(r.Context(), sess.Token(), localpart); err != nil {
			a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", target)
			return
		}
		a.recordAudit(r, sess, "user.unlock", localpart, "")
		a.flashRedirect(w, r, sess, "User account unlocked.", "success", "/admin/users")
		return

	case "disable":
		if err := a.Prosody.DisableUserAccount(r.Context(), sess.Token(), localpart); err != nil {
			a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", target)
			return
		}
		a.recordAudit(r, sess, "user.lock", localpart, "")
		a.flashRedirect(w, r, sess, "User account locked.", "success", "/admin/users")
		return
	}

	displayName := strings.TrimSpace(r.FormValue("display_name"))
	role := r.FormValue("role")
	if !validRole(role) {
		role = prosody.ScopeDefault
	}

	update := prosody.UserUpdate{DisplayName: &displayName, Role: &role}
	if err := a.Prosody.UpdateUser(r.Context(), sess.Token(), localpart, update); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", target)
		return
	}
	a.recordAudit(r, sess, "user.update", localpart, role)
	a.flashRedirect(w, r, sess, "User information updated.", "success", "/admin/users")
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

// adminTargetUserPage is the data behind the account pages that only display
// one account.
type adminTargetUserPage struct {
	webui.PageData
	Target *prosody.AdminUserInfo
	Dump   string
}

// handleDeleteUserForm asks for confirmation before deleting an account.
func (a *App) handleDeleteUserForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	localpart := r.PathValue("localpart")
	target, err := a.Prosody.GetUserByLocalpart(r.Context(), sess.Token(), localpart)
	if err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/users")
		return
	}

	page := a.newPage(w, r, sess, "Delete "+localpart, "users", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_delete_user.html", adminTargetUserPage{
		PageData: page,
		Target:   target,
	})
}

// handleDeleteUserSubmit deletes an account.
func (a *App) handleDeleteUserSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	localpart := r.PathValue("localpart")
	if r.FormValue("action") != "delete" {
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	if err := a.Prosody.DeleteUserByLocalpart(r.Context(), sess.Token(), localpart); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/users")
		return
	}
	a.recordAudit(r, sess, "user.delete", localpart, "")
	a.flashRedirect(w, r, sess, "User deleted.", "success", "/admin/users")
}

// handleDebugUser shows the raw diagnostic document of an account.
func (a *App) handleDebugUser(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	localpart := r.PathValue("localpart")
	target, err := a.Prosody.GetUserByLocalpart(r.Context(), sess.Token(), localpart)
	if err != nil {
		a.failAPI(w, r, err)
		return
	}

	dump := "{}"
	info, err := a.Prosody.GetUserDebugInfo(r.Context(), sess.Token(), localpart)
	if err == nil {
		if encoded, err := json.MarshalIndent(info, "", "  "); err == nil {
			dump = string(encoded)
		}
	} else {
		dump = "The chat server returned no diagnostic information."
	}

	page := a.newPage(w, r, sess, "Diagnostics for "+localpart, "users", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_debug_user.html", adminTargetUserPage{
		PageData: page,
		Target:   target,
		Dump:     dump,
	})
}

// adminResetLinkPage is the data behind the password reset link page.
type adminResetLinkPage struct {
	webui.PageData
	Localpart string
	Invite    *prosody.AdminInviteInfo
	InviteURL string
}

// handleResetLinkForm shows a password reset link issued for an account.
func (a *App) handleResetLinkForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	invite, err := a.Prosody.GetInviteByID(r.Context(), sess.Token(), id)
	if err != nil {
		a.flashRedirect(w, r, sess, "Password reset link not found.", "alert", "/admin/users")
		return
	}
	if invite.JID == "" {
		a.flashRedirect(w, r, sess, "Password reset link not found.", "alert", "/admin/users")
		return
	}

	localpart := invite.JID
	if at := strings.IndexByte(localpart, '@'); at > 0 {
		localpart = localpart[:at]
	}

	page := a.newPage(w, r, sess, "Password reset link", "users", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_reset_password.html", adminResetLinkPage{
		PageData:  page,
		Localpart: localpart,
		Invite:    invite,
		InviteURL: a.inviteURL(invite),
	})
}

// handleResetLinkSubmit revokes a password reset link.
func (a *App) handleResetLinkSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	if r.FormValue("action") != "revoke" {
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	if err := a.Prosody.DeleteInvite(r.Context(), sess.Token(), id); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/users")
		return
	}
	a.flashRedirect(w, r, sess, "Password reset link deleted.", "success", "/admin/users")
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
		http.Redirect(w, r, "/admin/invitations", http.StatusSeeOther)
		return
	}

	if err := a.Prosody.DeleteInvite(r.Context(), sess.Token(), id); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/invitations")
		return
	}
	a.recordAudit(r, sess, "invite.revoke", id, "")
	a.flashRedirect(w, r, sess, "Invitation revoked.", "success", "/admin/invitations")
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
		a.flashRedirect(w, r, sess, "At least one circle must be selected.", "alert", "/admin/invitation/-/new")
		return
	}

	role := r.FormValue("role")
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
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/invitations")
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

	id := r.PathValue("id")
	invite, err := a.Prosody.GetInviteByID(r.Context(), sess.Token(), id)
	if err != nil {
		if prosody.StatusOf(err) == http.StatusNotFound {
			a.flashRedirect(w, r, sess, "No such invitation exists.", "alert", "/admin/invitations")
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

	id := r.PathValue("id")
	if r.FormValue("action") != "revoke" {
		http.Redirect(w, r, "/admin/invitation/"+url.PathEscape(id), http.StatusSeeOther)
		return
	}

	if err := a.Prosody.DeleteInvite(r.Context(), sess.Token(), id); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/invitations")
		return
	}
	a.recordAudit(r, sess, "invite.revoke", id, "")
	a.flashRedirect(w, r, sess, "Invitation revoked.", "success", "/admin/invitations")
}

// adminCirclesPage is the data behind the circle list.
type adminCirclesPage struct {
	webui.PageData
	Circles []prosody.AdminGroupInfo
}

// handleCircles lists the circles of the virtual host.
func (a *App) handleCircles(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	circles, err := a.Prosody.ListGroups(r.Context(), sess.Token())
	page := a.newPage(w, r, sess, "Circles", "circles", webui.ShellAdmin)
	if err != nil {
		page.AddError("%s", "Circles could not be loaded from the chat server. "+apiErrorMessage(err))
		circles = nil
	} else {
		sortCircles(circles)
	}

	a.render(w, r, http.StatusOK, "admin_circles.html", adminCirclesPage{
		PageData: page,
		Circles:  circles,
	})
}

// sortCircles orders circles by name so the lists stay stable.
func sortCircles(circles []prosody.AdminGroupInfo) {
	slices.SortFunc(circles, func(x, y prosody.AdminGroupInfo) int {
		return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})
}

// handleCreateCircleForm shows the circle creation form.
func (a *App) handleCreateCircleForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	page := a.newPage(w, r, sess, "New circle", "circles", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_create_circle.html", pageOnly{PageData: page})
}

// handleCreateCircleSubmit creates a circle, optionally with a group chat.
func (a *App) handleCreateCircleSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		a.flashRedirect(w, r, sess, "A circle name is required.", "alert", "/admin/circle/-/new")
		return
	}

	circle, err := a.Prosody.CreateGroup(r.Context(), sess.Token(), name, r.FormValue("create_muc") != "")
	if err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/circle/-/new")
		return
	}
	a.recordAudit(r, sess, "circle.create", circle.ID, name)
	a.flashRedirect(w, r, sess, "Circle created.", "success",
		"/admin/circle/"+url.PathEscape(circle.ID))
}

// circleMember pairs a circle membership with the account behind it.
type circleMember struct {
	Localpart string
	User      *prosody.AdminUserInfo
}

// adminEditCirclePage is the data behind the circle editor.
type adminEditCirclePage struct {
	webui.PageData
	Circle     *prosody.AdminGroupInfo
	Members    []circleMember
	Candidates []string
}

// handleEditCircleForm shows the circle editor with its members and chats.
func (a *App) handleEditCircleForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	circle, err := a.Prosody.GetGroupByID(r.Context(), sess.Token(), id)
	if err != nil {
		if prosody.StatusOf(err) == http.StatusNotFound {
			a.flashRedirect(w, r, sess, "No such circle exists.", "alert", "/admin/circles")
			return
		}
		a.failAPI(w, r, err)
		return
	}

	users, err := a.Prosody.ListUsers(r.Context(), sess.Token())
	if err != nil {
		a.failAPI(w, r, err)
		return
	}

	byLocalpart := map[string]*prosody.AdminUserInfo{}
	for i := range users {
		byLocalpart[users[i].Localpart] = &users[i]
	}

	members := make([]circleMember, 0, len(circle.Members))
	for _, localpart := range circle.Members {
		members = append(members, circleMember{Localpart: localpart, User: byLocalpart[localpart]})
	}
	slices.SortFunc(members, func(x, y circleMember) int {
		return strings.Compare(x.Localpart, y.Localpart)
	})

	candidates := make([]string, 0, len(users))
	for _, user := range users {
		if !slices.Contains(circle.Members, user.Localpart) {
			candidates = append(candidates, user.Localpart)
		}
	}
	slices.Sort(candidates)

	page := a.newPage(w, r, sess, circle.Name, "circles", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_edit_circle.html", adminEditCirclePage{
		PageData:   page,
		Circle:     circle,
		Members:    members,
		Candidates: candidates,
	})
}

// handleEditCircleSubmit applies the circle editor actions.
func (a *App) handleEditCircleSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	target := "/admin/circle/" + url.PathEscape(id)

	var err error
	message := ""
	switch r.FormValue("action") {
	case "add_user":
		localpart := strings.TrimSpace(r.FormValue("user_to_add"))
		if localpart == "" {
			a.flashRedirect(w, r, sess, "Select a user to add.", "alert", target)
			return
		}
		err = a.Prosody.AddGroupMember(r.Context(), sess.Token(), id, localpart)
		message = "User added to circle."
	case "remove_user":
		localpart := strings.TrimSpace(r.FormValue("user"))
		if localpart == "" {
			a.flashRedirect(w, r, sess, "Select a user to remove.", "alert", target)
			return
		}
		err = a.Prosody.RemoveGroupMember(r.Context(), sess.Token(), id, localpart)
		message = "User removed from circle."
	case "remove_chat":
		chatID := strings.TrimSpace(r.FormValue("chat"))
		if chatID == "" {
			a.flashRedirect(w, r, sess, "Select a chat to remove.", "alert", target)
			return
		}
		err = a.Prosody.RemoveGroupChat(r.Context(), sess.Token(), id, chatID)
		message = "Chat removed from circle."
	default:
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			a.flashRedirect(w, r, sess, "A circle name is required.", "alert", target)
			return
		}
		err = a.Prosody.UpdateGroup(r.Context(), sess.Token(), id, name)
		message = "Circle updated."
	}

	if err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", target)
		return
	}
	a.flashRedirect(w, r, sess, message, "success", target)
}

// adminCirclePage is the data behind the circle pages that only display one
// circle.
type adminCirclePage struct {
	webui.PageData
	Circle *prosody.AdminGroupInfo
}

// handleDeleteCircleForm asks for confirmation before deleting a circle.
func (a *App) handleDeleteCircleForm(w http.ResponseWriter, r *http.Request) {
	sess, circle, ok := a.loadCircle(w, r)
	if !ok {
		return
	}
	page := a.newPage(w, r, sess, "Delete "+circle.Name, "circles", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_delete_circle.html", adminCirclePage{
		PageData: page,
		Circle:   circle,
	})
}

// handleDeleteCircleSubmit deletes a circle.
func (a *App) handleDeleteCircleSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	if r.FormValue("action") != "delete" {
		http.Redirect(w, r, "/admin/circles", http.StatusSeeOther)
		return
	}

	if err := a.Prosody.DeleteGroup(r.Context(), sess.Token(), id); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/circles")
		return
	}
	a.recordAudit(r, sess, "circle.delete", id, "")
	a.flashRedirect(w, r, sess, "Circle deleted.", "success", "/admin/circles")
}

// handleAddChatForm shows the group chat creation form of a circle.
func (a *App) handleAddChatForm(w http.ResponseWriter, r *http.Request) {
	sess, circle, ok := a.loadCircle(w, r)
	if !ok {
		return
	}
	page := a.newPage(w, r, sess, "New group chat", "circles", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_add_chat.html", adminCirclePage{
		PageData: page,
		Circle:   circle,
	})
}

// handleAddChatSubmit creates a group chat inside a circle.
func (a *App) handleAddChatSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	target := "/admin/circle/" + url.PathEscape(id)

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		a.flashRedirect(w, r, sess, "A group chat name is required.", "alert", target+"/add_chat")
		return
	}

	if err := a.Prosody.AddGroupChat(r.Context(), sess.Token(), id, name); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", target+"/add_chat")
		return
	}
	a.flashRedirect(w, r, sess, "New group chat added to circle.", "success", target)
}

// loadCircle resolves the circle named in the path for the pages that only
// need to display it.
func (a *App) loadCircle(w http.ResponseWriter, r *http.Request) (session.Data, *prosody.AdminGroupInfo, bool) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return nil, nil, false
	}

	circle, err := a.Prosody.GetGroupByID(r.Context(), sess.Token(), r.PathValue("id"))
	if err != nil {
		if prosody.StatusOf(err) == http.StatusNotFound {
			a.flashRedirect(w, r, sess, "No such circle exists.", "alert", "/admin/circles")
			return nil, nil, false
		}
		a.failAPI(w, r, err)
		return nil, nil, false
	}
	return sess, circle, true
}

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
	if r.FormValue("action") == "post_all" {
		recipients = "all"
		if r.FormValue("online_only") != "" {
			recipients = "online"
		}
	}

	if err := a.Prosody.PostAnnouncement(r.Context(), sess.Token(), body, recipients, sess.JID()); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/system/")
		return
	}

	if recipients == "self" {
		a.recordAudit(r, sess, "announcement.preview", "", "")
		a.flashRedirect(w, r, sess, "Preview sent to your own account.", "success", "/admin/system/")
		return
	}
	a.recordAudit(r, sess, "announcement.send", recipients, "")
	a.flashRedirect(w, r, sess, "Announcement sent.", "success", "/admin/system/")
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
		health.ProbeHTTPS(r.Context(), a.Cfg.Domain),
		a.probeStorage(),
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
func (a *App) probeStorage() health.Component {
	stats := hostmetrics.Collect()
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

// auditEventView is one row on the audit log page.
type auditEventView struct {
	When      time.Time
	Source    string
	Actor     string
	Action    string
	Target    string
	Detail    string
	IP        string
	UserAgent string
	Request   string
}

// adminAuditPage is the data behind the audit log.
type adminAuditPage struct {
	webui.PageData
	Query  string
	Events []auditEventView
	Note   string
}

// handleAuditLog shows searchable portal and Prosody audit events.
func (a *App) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	events := make([]auditEventView, 0, 128)

	if a.Audit != nil {
		for _, ev := range a.Audit.Recent(200, query) {
			events = append(events, auditEventView{
				When:      ev.When,
				Source:    ev.Source,
				Actor:     ev.Actor,
				Action:    ev.Action,
				Target:    ev.Target,
				Detail:    ev.Detail,
				IP:        ev.IP,
				UserAgent: ev.UserAgent,
				Request:   ev.RequestID,
			})
		}
	}

	note := ""
	if prosodyEvents, err := a.Prosody.ListAuditEvents(r.Context(), sess.Token(), 200, query); err != nil {
		note = "Chat server audit feed unavailable: " + apiErrorMessage(err)
	} else {
		for _, ev := range prosodyEvents {
			when := time.Unix(ev.When, 0).UTC()
			events = append(events, auditEventView{
				When:   when,
				Source: stringOr(ev.Source, "prosody"),
				Actor:  ev.Actor,
				Action: ev.Action,
				Target: ev.Target,
				Detail: ev.Detail,
				IP:     ev.IP,
			})
		}
	}

	slices.SortFunc(events, func(x, y auditEventView) int {
		return y.When.Compare(x.When)
	})
	if len(events) > 300 {
		events = events[:300]
	}

	page := a.newPage(w, r, sess, "Audit log", "audit", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_audit.html", adminAuditPage{
		PageData: page,
		Query:    query,
		Events:   events,
		Note:     note,
	})
}

func stringOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// adminMUCsPage is the data behind the group chat list.
type adminMUCsPage struct {
	webui.PageData
	Rooms []prosody.MUCRoom
	Host  string
	Query string
	Note  string
}

// handleMUCs lists group chats from the MUC component.
func (a *App) handleMUCs(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	rooms, host, err := a.Prosody.ListMUCRooms(r.Context(), sess.Token(), query)
	page := a.newPage(w, r, sess, "Group chats", "mucs", webui.ShellAdmin)
	note := ""
	if err != nil {
		note = "Group chats could not be loaded: " + apiErrorMessage(err)
		rooms = nil
	}
	a.render(w, r, http.StatusOK, "admin_mucs.html", adminMUCsPage{
		PageData: page,
		Rooms:    rooms,
		Host:     stringOr(host, "groups."+a.Cfg.Domain),
		Query:    query,
		Note:     note,
	})
}

// handleMUCsSubmit destroys a selected room from the list page.
func (a *App) handleMUCsSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	localpart := strings.TrimSpace(r.FormValue("destroy"))
	if localpart == "" {
		http.Redirect(w, r, "/admin/mucs", http.StatusSeeOther)
		return
	}
	if err := a.Prosody.DestroyMUCRoom(r.Context(), sess.Token(), localpart); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/mucs")
		return
	}
	a.recordAudit(r, sess, "muc.destroy", localpart, "")
	a.flashRedirect(w, r, sess, "Group chat destroyed.", "success", "/admin/mucs")
}

// handleCreateMUCForm shows the room creation form.
func (a *App) handleCreateMUCForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	page := a.newPage(w, r, sess, "New group chat", "mucs", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_create_muc.html", pageOnly{PageData: page})
}

// handleCreateMUCSubmit creates a group chat room.
func (a *App) handleCreateMUCSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		a.flashRedirect(w, r, sess, "A room name is required.", "alert", "/admin/muc/-/new")
		return
	}
	localpart := strings.TrimSpace(r.FormValue("localpart"))
	description := strings.TrimSpace(r.FormValue("description"))
	public := r.FormValue("public") != ""
	room, err := a.Prosody.CreateMUCRoom(r.Context(), sess.Token(), name, localpart, description, public)
	if err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/muc/-/new")
		return
	}
	a.recordAudit(r, sess, "muc.create", room.Localpart, name)
	a.flashRedirect(w, r, sess, "Group chat created.", "success", "/admin/muc/"+url.PathEscape(room.Localpart))
}

// adminMUCDetailPage is the data behind a single room page.
type adminMUCDetailPage struct {
	webui.PageData
	Room *prosody.MUCRoom
}

// handleMUCDetail shows one room and its occupants.
func (a *App) handleMUCDetail(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	localpart := r.PathValue("localpart")
	room, err := a.Prosody.GetMUCRoom(r.Context(), sess.Token(), localpart)
	if err != nil {
		if prosody.StatusOf(err) == http.StatusNotFound {
			a.flashRedirect(w, r, sess, "No such group chat exists.", "alert", "/admin/mucs")
			return
		}
		a.failAPI(w, r, err)
		return
	}
	page := a.newPage(w, r, sess, room.Name, "mucs", webui.ShellAdmin)
	a.render(w, r, http.StatusOK, "admin_muc_detail.html", adminMUCDetailPage{
		PageData: page,
		Room:     room,
	})
}

// handleMUCDetailSubmit destroys a room from the detail page.
func (a *App) handleMUCDetailSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	localpart := r.PathValue("localpart")
	if r.FormValue("action") != "destroy" {
		http.Redirect(w, r, "/admin/muc/"+url.PathEscape(localpart), http.StatusSeeOther)
		return
	}
	if err := a.Prosody.DestroyMUCRoom(r.Context(), sess.Token(), localpart); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/muc/"+url.PathEscape(localpart))
		return
	}
	a.recordAudit(r, sess, "muc.destroy", localpart, "")
	a.flashRedirect(w, r, sess, "Group chat destroyed.", "success", "/admin/mucs")
}
