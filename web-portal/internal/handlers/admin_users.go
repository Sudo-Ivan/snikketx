package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

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

	localpart := r.PathValue(paramLocalpart)
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

	localpart := r.PathValue(paramLocalpart)
	target := "/admin/user/" + url.PathEscape(localpart) + "/"

	switch r.FormValue(fieldAction) {
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
		a.flashRedirect(w, r, sess, "User account unlocked.", "success", pathAdminUsers)
		return

	case "disable":
		if err := a.Prosody.DisableUserAccount(r.Context(), sess.Token(), localpart); err != nil {
			a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", target)
			return
		}
		a.recordAudit(r, sess, "user.lock", localpart, "")
		a.flashRedirect(w, r, sess, "User account locked.", "success", pathAdminUsers)
		return
	}

	displayName := strings.TrimSpace(r.FormValue("display_name"))
	role := r.FormValue(fieldRole)
	if !validRole(role) {
		role = prosody.ScopeDefault
	}

	update := prosody.UserUpdate{DisplayName: &displayName, Role: &role}
	if err := a.Prosody.UpdateUser(r.Context(), sess.Token(), localpart, update); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", target)
		return
	}
	a.recordAudit(r, sess, "user.update", localpart, role)
	a.flashRedirect(w, r, sess, "User information updated.", "success", pathAdminUsers)
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

	localpart := r.PathValue(paramLocalpart)
	target, err := a.Prosody.GetUserByLocalpart(r.Context(), sess.Token(), localpart)
	if err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminUsers)
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

	localpart := r.PathValue(paramLocalpart)
	if r.FormValue(fieldAction) != "delete" {
		http.Redirect(w, r, pathAdminUsers, http.StatusSeeOther)
		return
	}

	if err := a.Prosody.DeleteUserByLocalpart(r.Context(), sess.Token(), localpart); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminUsers)
		return
	}
	a.recordAudit(r, sess, "user.delete", localpart, "")
	a.flashRedirect(w, r, sess, "User deleted.", "success", pathAdminUsers)
}

// handleDebugUser shows the raw diagnostic document of an account.
func (a *App) handleDebugUser(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	localpart := r.PathValue(paramLocalpart)
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

	id := r.PathValue(paramID)
	invite, err := a.Prosody.GetInviteByID(r.Context(), sess.Token(), id)
	if err != nil {
		a.flashRedirect(w, r, sess, msgResetLinkNotFound, "alert", pathAdminUsers)
		return
	}
	if invite.JID == "" {
		a.flashRedirect(w, r, sess, msgResetLinkNotFound, "alert", pathAdminUsers)
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

	id := r.PathValue(paramID)
	if r.FormValue(fieldAction) != "revoke" {
		http.Redirect(w, r, pathAdminUsers, http.StatusSeeOther)
		return
	}

	if err := a.Prosody.DeleteInvite(r.Context(), sess.Token(), id); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminUsers)
		return
	}
	a.flashRedirect(w, r, sess, "Password reset link deleted.", "success", pathAdminUsers)
}
