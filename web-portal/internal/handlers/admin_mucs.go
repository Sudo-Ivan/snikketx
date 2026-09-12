package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

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
		http.Redirect(w, r, pathAdminMUCs, http.StatusSeeOther)
		return
	}
	if err := a.Prosody.DestroyMUCRoom(r.Context(), sess.Token(), localpart); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminMUCs)
		return
	}
	a.recordAudit(r, sess, "muc.destroy", localpart, "")
	a.flashRedirect(w, r, sess, msgGroupChatDestroyed, "success", pathAdminMUCs)
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
	name := strings.TrimSpace(r.FormValue(fieldName))
	if name == "" {
		a.flashRedirect(w, r, sess, "A room name is required.", "alert", pathAdminMUCNew)
		return
	}
	localpart := strings.TrimSpace(r.FormValue(fieldLocalpart))
	description := strings.TrimSpace(r.FormValue("description"))
	public := r.FormValue("public") != ""
	room, err := a.Prosody.CreateMUCRoom(r.Context(), sess.Token(), name, localpart, description, public)
	if err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathAdminMUCNew)
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
	localpart := r.PathValue(paramLocalpart)
	room, err := a.Prosody.GetMUCRoom(r.Context(), sess.Token(), localpart)
	if err != nil {
		if prosody.StatusOf(err) == http.StatusNotFound {
			a.flashRedirect(w, r, sess, "No such group chat exists.", "alert", pathAdminMUCs)
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
	localpart := r.PathValue(paramLocalpart)
	if r.FormValue(fieldAction) != "destroy" {
		http.Redirect(w, r, "/admin/muc/"+url.PathEscape(localpart), http.StatusSeeOther)
		return
	}
	if err := a.Prosody.DestroyMUCRoom(r.Context(), sess.Token(), localpart); err != nil {
		a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", "/admin/muc/"+url.PathEscape(localpart))
		return
	}
	a.recordAudit(r, sess, "muc.destroy", localpart, "")
	a.flashRedirect(w, r, sess, msgGroupChatDestroyed, "success", pathAdminMUCs)
}
