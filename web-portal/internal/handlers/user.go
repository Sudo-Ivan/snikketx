package handlers

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
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
		http.Redirect(w, r, "/user/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /user/{$}", a.handleUserHome)
	mux.HandleFunc("GET /user/passwd", a.handlePasswordForm)
	mux.HandleFunc("POST /user/passwd", a.handlePasswordSubmit)
	mux.HandleFunc("GET /user/profile", a.handleProfileForm)
	mux.HandleFunc("POST /user/profile", a.handleProfileSubmit)
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
			fail("Incorrect password.")
			return
		}
		switch prosody.StatusOf(err) {
		case http.StatusUnauthorized, http.StatusForbidden:
			fail("Incorrect password.")
			return
		}
		a.failAPI(w, r, err)
		return
	}

	sess.SetAuth(tokenInfo.Token, strings.Join(tokenInfo.Scopes, " "), sess.JID())
	a.flashRedirect(w, r, sess, "Password changed.", "success", "/user/passwd")
}

// profilePage is the data behind the profile form.
type profilePage struct {
	webui.PageData
	Nickname      string
	AccessModel   string
	AccessModels  []accessModelChoice
	MaxAvatarSize int64
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

	page := a.newPage(w, r, sess, "Profile", "profile", webui.ShellApp)
	applyUserInfo(&page, info)
	a.render(w, r, http.StatusOK, "user_profile.html", profilePage{
		PageData:      page,
		Nickname:      info.Nickname,
		AccessModel:   accessModel,
		AccessModels:  accessModelChoices,
		MaxAvatarSize: a.Cfg.MaxAvatarSize,
	})
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
		page := a.newPage(w, r, sess, "Profile", "profile", webui.ShellApp)
		page.AddError("%s", message)
		a.render(w, r, http.StatusBadRequest, "user_profile.html", profilePage{
			PageData:      page,
			Nickname:      nickname,
			AccessModel:   accessModel,
			AccessModels:  accessModelChoices,
			MaxAvatarSize: a.Cfg.MaxAvatarSize,
		})
	}

	if file, header, err := r.FormFile("avatar"); err == nil {
		defer func() { _ = file.Close() }()

		if header.Size > a.Cfg.MaxAvatarSize {
			fail("The chosen avatar is too big. To upload larger avatars, use the Snikket app.")
			return
		}
		data, err := io.ReadAll(io.LimitReader(file, a.Cfg.MaxAvatarSize+1))
		if err != nil {
			fail("The avatar could not be read.")
			return
		}
		if int64(len(data)) > a.Cfg.MaxAvatarSize {
			fail("The chosen avatar is too big. To upload larger avatars, use the Snikket app.")
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

	a.flashRedirect(w, r, sess, "Profile updated.", "success", "/user/profile")
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
	_, _ = io.WriteString(w, document)
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
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
