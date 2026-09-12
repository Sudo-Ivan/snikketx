package handlers

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

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

	name := strings.TrimSpace(r.FormValue(fieldName))
	if name == "" {
		a.flashRedirect(w, r, sess, msgCircleNameRequired, "alert", pathAdminCircleNew)
		return
	}

	circle, err := a.Prosody.CreateGroup(r.Context(), sess.Token(), name, r.FormValue("create_muc") != "")
	if err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminCircleNew)
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

	id := r.PathValue(paramID)
	circle, err := a.Prosody.GetGroupByID(r.Context(), sess.Token(), id)
	if err != nil {
		if prosody.StatusOf(err) == http.StatusNotFound {
			a.flashRedirect(w, r, sess, msgNoSuchCircle, "alert", pathAdminCircles)
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

	id := r.PathValue(paramID)
	target := "/admin/circle/" + url.PathEscape(id)

	var err error
	message := ""
	switch r.FormValue(fieldAction) {
	case "add_user":
		localpart := strings.TrimSpace(r.FormValue("user_to_add"))
		if localpart == "" {
			a.flashRedirect(w, r, sess, "Select a user to add.", "alert", target)
			return
		}
		err = a.Prosody.AddGroupMember(r.Context(), sess.Token(), id, localpart)
		message = "User added to circle."
	case "remove_user":
		localpart := strings.TrimSpace(r.FormValue(fieldUser))
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
		name := strings.TrimSpace(r.FormValue(fieldName))
		if name == "" {
			a.flashRedirect(w, r, sess, msgCircleNameRequired, "alert", target)
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

	id := r.PathValue(paramID)
	if r.FormValue(fieldAction) != "delete" {
		http.Redirect(w, r, pathAdminCircles, http.StatusSeeOther)
		return
	}

	if err := a.Prosody.DeleteGroup(r.Context(), sess.Token(), id); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminCircles)
		return
	}
	a.recordAudit(r, sess, "circle.delete", id, "")
	a.flashRedirect(w, r, sess, "Circle deleted.", "success", pathAdminCircles)
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

	id := r.PathValue(paramID)
	target := "/admin/circle/" + url.PathEscape(id)

	name := strings.TrimSpace(r.FormValue(fieldName))
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

	circle, err := a.Prosody.GetGroupByID(r.Context(), sess.Token(), r.PathValue(paramID))
	if err != nil {
		if prosody.StatusOf(err) == http.StatusNotFound {
			a.flashRedirect(w, r, sess, msgNoSuchCircle, "alert", pathAdminCircles)
			return nil, nil, false
		}
		a.failAPI(w, r, err)
		return nil, nil, false
	}
	return sess, circle, true
}
