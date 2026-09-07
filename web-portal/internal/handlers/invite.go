package handlers

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

// playStoreCampaign is the referrer campaign id appended to the Play Store link
// so the app can pick the invitation up after installation.
const playStoreCampaign = "pcampaignidMKT-Other-global-all-co-prtnr-py-PartBadge-Mar2515-1"

// fDroidURL opens the app listing in the F-Droid client.
const fDroidURL = "market://details?id=org.snikket.android"

// importTypes are the content types accepted for an account data import.
var importTypes = []string{"application/xml", "text/xml"}

// mountInvite registers the unauthenticated invitation routes.
func (a *App) mountInvite(mux *http.ServeMux) {
	mux.HandleFunc("GET /invite/-", a.handleIndex)
	mux.HandleFunc("GET /invite/success", a.handleInviteSuccessForm)
	mux.HandleFunc("POST /invite/success", a.handleInviteSuccessSubmit)
	mux.HandleFunc("GET /invite/success/reset", a.handleResetSuccess)
	mux.HandleFunc("GET /invite/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/invite/"+url.PathEscape(r.PathValue("id"))+"/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /invite/{id}/{$}", a.handleInviteView)
	mux.HandleFunc("GET /invite/{id}/register", a.handleRegisterForm)
	mux.HandleFunc("POST /invite/{id}/register", a.handleRegisterSubmit)
	mux.HandleFunc("GET /invite/{id}/reset", a.handleInviteResetForm)
	mux.HandleFunc("POST /invite/{id}/reset", a.handleInviteResetSubmit)
}

// invitePage is the data behind the invitation landing pages.
type invitePage struct {
	webui.PageData
	InviteID      string
	Inviter       string
	XMPPURI       string
	AccountJID    string
	Localpart     string
	PlayStoreURL  string
	FDroidURL     string
	PlayBadge     string
	AppleBadge    string
	InvitePageURL string
}

// handleInviteView shows the landing page of an invitation. A password reset
// invitation gets its own page because it does not create an account.
func (a *App) handleInviteView(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	invite, err := a.Prosody.GetPublicInviteByID(r.Context(), id)
	if err != nil {
		a.renderInviteInvalid(w, r)
		return
	}

	sess := a.Sessions.Get(r)

	if invite.ResetLocalpart != "" {
		page := a.newPage(w, r, sess, "Reset your password", "", webui.ShellBare)
		a.render(w, r, http.StatusOK, "invite_reset_view.html", invitePage{
			PageData:   page,
			InviteID:   id,
			Localpart:  invite.ResetLocalpart,
			AccountJID: invite.ResetLocalpart + "@" + invite.Domain,
		})
		return
	}

	query := url.Values{}
	query.Set("id", "org.snikket.android")
	query.Set("referrer", invite.XMPPURI)
	query.Set("pcampaignid", playStoreCampaign)

	page := a.newPage(w, r, sess, "You are invited", "", webui.ShellBare)
	data := invitePage{
		PageData:      page,
		InviteID:      id,
		Inviter:       invite.Inviter,
		XMPPURI:       invite.XMPPURI,
		PlayStoreURL:  "https://play.google.com/store/apps/details?" + query.Encode(),
		FDroidURL:     fDroidURL,
		PlayBadge:     "/static/img/google/en_badge_web_generic.png",
		AppleBadge:    "/static/img/apple/en.svg",
		InvitePageURL: "https://" + a.Cfg.Domain + "/invite/" + url.PathEscape(id) + "/",
	}

	header := w.Header()
	header.Set("Link", "<"+invite.XMPPURI+">; rel=\"alternate\"")
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("Access-Control-Expose-Headers", "Link")

	a.render(w, r, http.StatusOK, "invite_view.html", data)
}

// renderInviteInvalid shows the page for an invitation that expired or never
// existed.
func (a *App) renderInviteInvalid(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.Get(r)
	page := a.newPage(w, r, sess, "Invitation not valid", "", webui.ShellBare)
	a.render(w, r, http.StatusNotFound, "invite_invalid.html", pageOnly{PageData: page})
}

// handleRegisterForm shows the manual account registration form.
func (a *App) handleRegisterForm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	invite, err := a.Prosody.GetPublicInviteByID(r.Context(), id)
	if err != nil {
		http.Redirect(w, r, "/invite/"+url.PathEscape(id)+"/", http.StatusSeeOther)
		return
	}
	if invite.ResetLocalpart != "" {
		http.Redirect(w, r, "/invite/"+url.PathEscape(id)+"/reset", http.StatusSeeOther)
		return
	}

	sess := a.Sessions.Get(r)
	page := a.newPage(w, r, sess, "Create your account", "", webui.ShellBare)
	a.render(w, r, http.StatusOK, "invite_register.html", invitePage{
		PageData: page,
		InviteID: id,
		XMPPURI:  invite.XMPPURI,
	})
}

// handleRegisterSubmit creates an account from an invitation token and signs the
// new account in.
func (a *App) handleRegisterSubmit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := a.Sessions.Get(r)

	localpart := strings.TrimSpace(r.FormValue("localpart"))
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")

	fail := func(message string) {
		page := a.newPage(w, r, sess, "Create your account", "", webui.ShellBare)
		page.AddError("%s", message)
		a.render(w, r, http.StatusBadRequest, "invite_register.html", invitePage{
			PageData:  page,
			InviteID:  id,
			Localpart: localpart,
		})
	}

	switch {
	case localpart == "":
		fail("Choose a username.")
		return
	case len(password) < minPasswordLength:
		fail("The password must be at least 10 characters long.")
		return
	case password != confirm:
		fail("The passwords must match.")
		return
	}

	jid, err := a.Prosody.RegisterWithToken(r.Context(), id, localpart, password)
	if err != nil {
		switch prosody.StatusOf(err) {
		case http.StatusConflict:
			fail("That username is already taken.")
		case http.StatusForbidden:
			fail("Registration was declined.")
		case http.StatusBadRequest:
			fail("That username is not valid.")
		case http.StatusNotFound:
			http.Redirect(w, r, "/invite/"+url.PathEscape(id)+"/", http.StatusSeeOther)
		default:
			a.failAPI(w, r, err)
		}
		return
	}

	sess[session.KeyInvite] = jid
	if tokenInfo, err := a.Prosody.Login(r.Context(), jid, password); err == nil {
		sess.SetAuth(tokenInfo.Token, strings.Join(tokenInfo.Scopes, " "), jid)
	}
	_ = a.Sessions.Save(w, sess)
	http.Redirect(w, r, "/invite/success", http.StatusSeeOther)
}

// handleInviteResetForm shows the password reset form of a reset invitation.
func (a *App) handleInviteResetForm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	invite, err := a.Prosody.GetPublicInviteByID(r.Context(), id)
	if err != nil {
		http.Redirect(w, r, "/invite/"+url.PathEscape(id)+"/", http.StatusSeeOther)
		return
	}
	if invite.ResetLocalpart == "" {
		http.Redirect(w, r, "/invite/"+url.PathEscape(id)+"/register", http.StatusSeeOther)
		return
	}

	sess := a.Sessions.Get(r)
	page := a.newPage(w, r, sess, "Set a new password", "", webui.ShellBare)
	a.render(w, r, http.StatusOK, "invite_reset.html", invitePage{
		PageData:   page,
		InviteID:   id,
		Localpart:  invite.ResetLocalpart,
		AccountJID: invite.ResetLocalpart + "@" + invite.Domain,
	})
}

// handleInviteResetSubmit sets a new password from a reset invitation.
func (a *App) handleInviteResetSubmit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := a.Sessions.Get(r)

	invite, err := a.Prosody.GetPublicInviteByID(r.Context(), id)
	if err != nil {
		http.Redirect(w, r, "/invite/"+url.PathEscape(id)+"/", http.StatusSeeOther)
		return
	}
	if invite.ResetLocalpart == "" {
		http.Redirect(w, r, "/invite/"+url.PathEscape(id)+"/register", http.StatusSeeOther)
		return
	}

	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")

	fail := func(message string) {
		page := a.newPage(w, r, sess, "Set a new password", "", webui.ShellBare)
		page.AddError("%s", message)
		a.render(w, r, http.StatusBadRequest, "invite_reset.html", invitePage{
			PageData:   page,
			InviteID:   id,
			Localpart:  invite.ResetLocalpart,
			AccountJID: invite.ResetLocalpart + "@" + invite.Domain,
		})
	}

	switch {
	case len(password) < minPasswordLength:
		fail("The password must be at least 10 characters long.")
		return
	case password != confirm:
		fail("The passwords must match.")
		return
	}

	jid, err := a.Prosody.RegisterWithToken(r.Context(), id, invite.ResetLocalpart, password)
	if err != nil {
		switch prosody.StatusOf(err) {
		case http.StatusForbidden:
			fail("The password change was declined.")
		case http.StatusNotFound:
			http.Redirect(w, r, "/invite/"+url.PathEscape(id)+"/", http.StatusSeeOther)
		default:
			a.failAPI(w, r, err)
		}
		return
	}

	sess[session.KeyInvite] = jid
	_ = a.Sessions.Save(w, sess)
	http.Redirect(w, r, "/invite/success/reset", http.StatusSeeOther)
}

// inviteSuccessPage is the data behind the pages shown after an account was
// created or a password was reset.
type inviteSuccessPage struct {
	webui.PageData
	JID              string
	MigrationDone    bool
	MaxImportSize    int64
	CanImport        bool
	PlayBadge        string
	AppleBadge       string
	FDroidURL        string
	InviteDownloadOK bool
}

// handleInviteSuccessForm shows the page a newly registered account lands on,
// offering an account data import.
func (a *App) handleInviteSuccessForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	a.renderInviteSuccess(w, r, sess, false, "", http.StatusOK)
}

// handleInviteSuccessSubmit imports an uploaded XEP-0227 account data document.
func (a *App) handleInviteSuccessSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}

	file, header, err := r.FormFile("account_data_file")
	if err != nil {
		a.renderInviteSuccess(w, r, sess, false, "Choose a file to import.", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > maxImportSize {
		a.renderInviteSuccess(w, r, sess, false,
			"The account data you tried to import is too large to upload. Contact your SnikketX operator.",
			http.StatusBadRequest)
		return
	}

	mimetype := strings.TrimSpace(strings.Split(header.Header.Get("Content-Type"), ";")[0])
	if !acceptedImportType(mimetype) {
		a.renderInviteSuccess(w, r, sess, false,
			"The account data you tried to import is in an unknown format. Upload an XML file in XEP-0227 format.",
			http.StatusBadRequest)
		return
	}

	data, err := io.ReadAll(io.LimitReader(file, maxImportSize+1))
	if err != nil || int64(len(data)) > maxImportSize {
		a.renderInviteSuccess(w, r, sess, false,
			"The account data you tried to import is too large to upload. Contact your SnikketX operator.",
			http.StatusBadRequest)
		return
	}
	if len(data) == 0 {
		a.renderInviteSuccess(w, r, sess, false, "The uploaded file was empty.", http.StatusBadRequest)
		return
	}

	if err := a.Prosody.ImportAccountData(r.Context(), sess.Token(), string(data)); err != nil {
		a.failAPI(w, r, err)
		return
	}
	a.renderInviteSuccess(w, r, sess, true, "", http.StatusOK)
}

// renderInviteSuccess draws the post registration page.
func (a *App) renderInviteSuccess(w http.ResponseWriter, r *http.Request, sess session.Data, migrated bool, problem string, status int) {
	jid := sess[session.KeyInvite]
	if jid == "" {
		jid = sess.JID()
	}

	page := a.newPage(w, r, sess, "Welcome", "", webui.ShellBare)
	if problem != "" {
		page.AddError("%s", problem)
	}

	a.render(w, r, status, "invite_success.html", inviteSuccessPage{
		PageData:      page,
		JID:           jid,
		MigrationDone: migrated,
		MaxImportSize: maxImportSize,
		CanImport:     !migrated,
		PlayBadge:     "/static/img/google/en_badge_web_generic.png",
		AppleBadge:    "/static/img/apple/en.svg",
		FDroidURL:     fDroidURL,
	})
}

// handleResetSuccess confirms that a password was changed through an invitation.
func (a *App) handleResetSuccess(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.Get(r)
	jid := sess[session.KeyInvite]

	page := a.newPage(w, r, sess, "Password changed", "", webui.ShellBare)
	a.render(w, r, http.StatusOK, "invite_reset_success.html", inviteSuccessPage{
		PageData:   page,
		JID:        jid,
		PlayBadge:  "/static/img/google/en_badge_web_generic.png",
		AppleBadge: "/static/img/apple/en.svg",
		FDroidURL:  fDroidURL,
	})
}

// acceptedImportType reports whether a content type may be imported.
func acceptedImportType(mimetype string) bool {
	return slices.Contains(importTypes, mimetype)
}
