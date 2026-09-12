package handlers

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

// accessModelChoice is one profile visibility option offered to the user.
type accessModelChoice struct {
	Value string
	Label string
	Hint  string
}

// accessModelChoices are the profile visibility options, from the most
// restrictive to the most open.
var accessModelChoices = []accessModelChoice{
	{Value: prosody.AccessModelWhitelist, Label: "Nobody", Hint: "Only you can see your profile."},
	{Value: prosody.AccessModelPresence, Label: "Contacts only", Hint: "Only people on your contact list can see your profile."},
	{Value: prosody.AccessModelOpen, Label: "Everyone", Hint: "Anyone who knows your address can see your profile."},
}

// mountUser registers the routes of the signed in account area.
func (a *App) mountUser(mux *http.ServeMux) {
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, pathUserHome, http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /user/{$}", a.handleUserHome)
	mux.HandleFunc("GET "+pathUserPasswd, a.handlePasswordForm)
	mux.HandleFunc("POST "+pathUserPasswd, a.handlePasswordSubmit)
	mux.HandleFunc("GET "+pathUserProfile, a.handleProfileForm)
	mux.HandleFunc("POST "+pathUserProfile, a.handleProfileSubmit)
	mux.HandleFunc("POST /user/profile/sessions", a.handleProfileSessions)
	mux.HandleFunc("GET /user/manage_data", a.handleManageDataForm)
	mux.HandleFunc("POST /user/manage_data", a.handleManageDataSubmit)
	mux.HandleFunc("GET /user/logout", a.handleLogoutForm)
	mux.HandleFunc("POST /user/logout", a.handleLogoutSubmit)
}

// userHomePage is the data behind the account overview.
type userHomePage struct {
	webui.PageData
	Metrics serverMetrics
}

// handleUserHome shows the account overview with the profile summary and, when
// the account is allowed to see them, the server counters.
func (a *App) handleUserHome(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}

	info, err := a.Prosody.GetUserInfo(r.Context(), sess.Token(), sess.JID(), sess.IsAdmin())
	if err != nil {
		a.failAPI(w, r, err)
		return
	}

	raw, err := a.Prosody.GetSystemMetrics(r.Context(), sess.Token())
	if err != nil {
		switch prosody.StatusOf(err) {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			raw = nil
		case 0:
			a.backendError(w, r, err)
			return
		default:
			raw = nil
		}
	}

	page := a.newPage(w, r, sess, "Home", "home", webui.ShellApp)
	applyUserInfo(&page, info)
	a.render(w, r, http.StatusOK, "user_home.html", userHomePage{
		PageData: page,
		Metrics:  parseServerMetrics(raw),
	})
}

// pageOnly wraps the base page data for templates that need nothing else.
type pageOnly struct {
	webui.PageData
}

// handlePasswordForm shows the password change form.
func (a *App) handlePasswordForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	page := a.newPage(w, r, sess, "Change password", "password", webui.ShellApp)
	a.render(w, r, http.StatusOK, "user_passwd.html", pageOnly{PageData: page})
}

// handlePasswordSubmit changes the account password and replaces the session
// token with the one issued during the change.
func (a *App) handlePasswordSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}

	current := r.FormValue("current_password")
	next := r.FormValue("new_password")
	confirm := r.FormValue("new_password_confirm")

	fail := func(message string) {
		page := a.newPage(w, r, sess, "Change password", "password", webui.ShellApp)
		page.AddError("%s", message)
		a.render(w, r, http.StatusBadRequest, "user_passwd.html", pageOnly{PageData: page})
	}

	switch {
	case current == "" || next == "":
		fail("Fill in your current and your new password.")
		return
	case len(next) < minPasswordLength:
		fail("The new password must be at least 10 characters long.")
		return
	case next != confirm:
		fail("The new passwords must match.")
		return
	}

	tokenInfo, err := a.Prosody.ChangePassword(r.Context(), sess.JID(), current, next)
	if err != nil {
		if errors.Is(err, prosody.ErrInvalidCredentials) {
			fail(msgIncorrectPassword)
			return
		}
		switch prosody.StatusOf(err) {
		case http.StatusUnauthorized, http.StatusForbidden:
			fail(msgIncorrectPassword)
			return
		}
		a.failAPI(w, r, err)
		return
	}

	sess.SetAuth(tokenInfo.Token, strings.Join(tokenInfo.Scopes, " "), sess.JID())
	a.flashRedirect(w, r, sess, "Password changed.", "success", pathUserPasswd)
}

// profilePage is the data behind the profile form.
type profilePage struct {
	webui.PageData
	JID              string
	Nickname         string
	AccessModel      string
	AccessModelLabel string
	AccessModels     []accessModelChoice
	MaxAvatarSize    int64
	ServerVersion    string
	Sessions         []sessionView
	Note             string
}

func accessModelLabel(value string) string {
	for _, choice := range accessModelChoices {
		if choice.Value == value {
			return choice.Label
		}
	}
	return value
}

func (a *App) loadProfilePage(w http.ResponseWriter, r *http.Request, sess session.Data, info *prosody.UserInfo, nickname, accessModel, note string) profilePage {
	if info == nil {
		info = &prosody.UserInfo{Address: sess.JID()}
	}
	if nickname == "" {
		nickname = info.Nickname
	}
	if accessModel == "" {
		accessModel = prosody.AccessModelOpen
	}
	devices, err := a.Prosody.ListMyClientDevices(r.Context(), sess.Token())
	sessionNote := note
	if err != nil {
		if sessionNote == "" {
			sessionNote = "Device list unavailable: " + apiErrorMessage(err)
		}
		devices = nil
	}
	portalIP := authlimit.ClientIP(r)
	sessions := make([]sessionView, 0, len(devices)+1)
	sessions = append(sessions, sessionView{
		ClientID:  "portal",
		Name:      "This portal session",
		UserAgent: r.UserAgent(),
		IP:        portalIP,
		MapURL:    ipMapURL(portalIP),
		LastSeen:  "now",
		Icon:      "circle-user",
		Kind:      "Web portal",
		IsPortal:  true,
		IsCurrent: true,
	})
	for _, device := range devices {
		icon, kind, label := classifyClient(device.Name, device.UserAgent, device.Resource)
		sessions = append(sessions, sessionView{
			ClientID:    device.ClientID,
			Name:        label,
			UserAgent:   device.UserAgent,
			Resource:    device.Resource,
			IP:          device.IP,
			MapURL:      ipMapURL(device.IP),
			LastSeen:    device.LastSeen,
			HasPush:     device.HasPush,
			PushService: device.PushService,
			Icon:        icon,
			Kind:        kind,
		})
	}
	serverVersion, _ := a.Prosody.GetServerVersion(r.Context(), sess.Token(), sess.JID())
	page := a.newPage(w, r, sess, "Profile", "profile", webui.ShellApp)
	applyUserInfo(&page, info)
	return profilePage{
		PageData:         page,
		JID:              sess.JID(),
		Nickname:         nickname,
		AccessModel:      accessModel,
		AccessModelLabel: accessModelLabel(accessModel),
		AccessModels:     accessModelChoices,
		MaxAvatarSize:    a.Cfg.MaxAvatarSize,
		ServerVersion:    serverVersion,
		Sessions:         sessions,
		Note:             sessionNote,
	}
}

// handleProfileForm shows the profile form with the published nickname and the
// current profile visibility.
func (a *App) handleProfileForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}

	info, err := a.Prosody.GetUserInfo(r.Context(), sess.Token(), sess.JID(), sess.IsAdmin())
	if err != nil {
		a.failAPI(w, r, err)
		return
	}

	accessModel, err := a.Prosody.GuessProfileAccessModel(r.Context(), sess.Token(), sess.JID())
	if err != nil {
		accessModel = prosody.AccessModelOpen
	}

	a.render(w, r, http.StatusOK, "user_profile.html", a.loadProfilePage(w, r, sess, info, info.Nickname, accessModel, ""))
}

// handleProfileSubmit publishes a new nickname, avatar and profile visibility.
func (a *App) handleProfileSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}

	nickname := strings.TrimSpace(r.FormValue("nickname"))
	accessModel := r.FormValue("profile_access_model")
	if !validAccessModel(accessModel) {
		accessModel = prosody.AccessModelPresence
	}

	fail := func(message string) {
		info, _ := a.Prosody.GetUserInfo(r.Context(), sess.Token(), sess.JID(), sess.IsAdmin())
		page := a.loadProfilePage(w, r, sess, info, nickname, accessModel, "")
		page.AddError("%s", message)
		a.render(w, r, http.StatusBadRequest, "user_profile.html", page)
	}

	if file, header, err := r.FormFile("avatar"); err == nil {
		defer func() { _ = file.Close() }()

		if header.Size > a.Cfg.MaxAvatarSize {
			fail(msgAvatarTooBig)
			return
		}
		data, err := io.ReadAll(io.LimitReader(file, a.Cfg.MaxAvatarSize+1))
		if err != nil {
			fail("The avatar could not be read.")
			return
		}
		if int64(len(data)) > a.Cfg.MaxAvatarSize {
			fail(msgAvatarTooBig)
			return
		}
		if len(data) > 0 {
			mimetype := header.Header.Get("Content-Type")
			if mimetype == "" {
				mimetype = "application/octet-stream"
			}
			if err := a.Prosody.SetUserAvatar(r.Context(), sess.Token(), sess.JID(), data, mimetype); err != nil {
				a.failAPI(w, r, err)
				return
			}
		}
	}

	info, err := a.Prosody.GetUserInfo(r.Context(), sess.Token(), sess.JID(), sess.IsAdmin())
	if err != nil {
		a.failAPI(w, r, err)
		return
	}
	if info.Nickname != nickname {
		if err := a.Prosody.SetUserNickname(r.Context(), sess.Token(), sess.JID(), nickname); err != nil {
			a.failAPI(w, r, err)
			return
		}
	}

	if err := a.Prosody.SetAvatarAccessModel(r.Context(), sess.Token(), sess.JID(), accessModel); err != nil {
		a.failAPI(w, r, err)
		return
	}
	if err := a.Prosody.SetNicknameAccessModel(r.Context(), sess.Token(), sess.JID(), accessModel); err != nil {
		a.failAPI(w, r, err)
		return
	}
	if err := a.Prosody.SetVCardAccessModel(r.Context(), sess.Token(), sess.JID(), accessModel); err != nil {
		a.failAPI(w, r, err)
		return
	}

	a.flashRedirect(w, r, sess, "Profile updated.", "success", pathUserProfile)
}

// handleProfileSessions revokes one or every registered client for the caller.
func (a *App) handleProfileSessions(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	action := strings.TrimSpace(r.FormValue(fieldAction))
	switch action {
	case "revoke":
		clientID := strings.TrimSpace(r.FormValue("client_id"))
		if clientID == "" || clientID == "portal" {
			a.flashRedirect(w, r, sess, "Missing client id.", "alert", pathUserProfile)
			return
		}
		if err := a.Prosody.RevokeMyClientDevice(r.Context(), sess.Token(), clientID); err != nil {
			a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathUserProfile)
			return
		}
		a.flashRedirect(w, r, sess, "Device signed out.", "success", pathUserProfile)
	case "revoke_all":
		if err := a.Prosody.RevokeAllMyClientDevices(r.Context(), sess.Token()); err != nil {
			a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathUserProfile)
			return
		}
		a.flashRedirect(w, r, sess, "All chat devices signed out. This portal session is still active.", "success", pathUserProfile)
	case "revoke_others":
		if err := a.Prosody.RevokeAllMyClientDevices(r.Context(), sess.Token()); err != nil {
			a.flashRedirect(w, r, sess, apiErrorMessage(err), "alert", pathUserProfile)
			return
		}
		a.flashRedirect(w, r, sess, "Other chat devices signed out. This portal session stays signed in.", "success", pathUserProfile)
	default:
		a.flashRedirect(w, r, sess, "Unknown session action.", "alert", pathUserProfile)
	}
}

// validAccessModel reports whether value is one of the offered choices.
func validAccessModel(value string) bool {
	for _, choice := range accessModelChoices {
		if choice.Value == value {
			return true
		}
	}
	return false
}

// handleManageDataForm shows the account data export page.
func (a *App) handleManageDataForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	page := a.newPage(w, r, sess, "Account data", "data", webui.ShellApp)
	a.render(w, r, http.StatusOK, "user_manage_data.html", pageOnly{PageData: page})
}

// handleManageDataSubmit exports the account data as a XEP-0227 document.
func (a *App) handleManageDataSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}

	document, err := a.Prosody.ExportAccountData(r.Context(), sess.Token())
	if err != nil {
		a.failAPI(w, r, err)
		return
	}
	if document == "" {
		a.flashRedirect(w, r, sess, "You currently have no account data to export.", "alert", "/user/manage_data")
		return
	}

	filename := "account-data-" + url.PathEscape(sess.JID()) + ".xml"
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="account-data.xml"; filename*=UTF-8''`+filename)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, document) // #nosec G705 -- account export is application/xml from Prosody for the signed-in user
}

// handleLogoutForm shows the sign out confirmation.
func (a *App) handleLogoutForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	page := a.newPage(w, r, sess, "Sign out", "logout", webui.ShellApp)
	a.render(w, r, http.StatusOK, "user_logout.html", pageOnly{PageData: page})
}

// handleLogoutSubmit revokes the access token and clears the session. The local
// session is dropped even when revocation fails, because leaving the browser
// signed in would be worse than a token that outlives the session.
func (a *App) handleLogoutSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}

	if err := a.Prosody.Logout(r.Context(), sess.Token()); err != nil {
		a.recordError(r, err)
	}

	sess.ClearAuth()
	a.Sessions.Clear(w)
	http.Redirect(w, r, pathLogin, http.StatusSeeOther)
}
