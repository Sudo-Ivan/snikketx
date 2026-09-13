package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/sudo-ivan/snikketx/web-portal/internal/bots"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

const pathUserBots = "/user/bots"

// mountBots registers the user facing bot management pages. They are only
// reachable when the server runs mod_snikketx_bots and the portal holds a
// management token.
func (a *App) mountBots(mux *http.ServeMux) {
	if a.Cfg == nil || !a.Cfg.BotsEnabled() {
		return
	}
	if a.Bots == nil {
		a.Bots = bots.New(a.Cfg.BotsEndpoint, a.Cfg.BotsHost, a.Cfg.BotsAdminToken)
	}
	mux.HandleFunc("GET "+pathUserBots, a.handleBotsPage)
	mux.HandleFunc("POST "+pathUserBots, a.handleBotsCreate)
	mux.HandleFunc("POST "+pathUserBots+"/{name}/delete", a.handleBotsDelete)
	mux.HandleFunc("POST "+pathUserBots+"/{name}/toggle", a.handleBotsToggle)
	mux.HandleFunc("POST "+pathUserBots+"/{name}/tokens", a.handleBotsTokenMint)
	mux.HandleFunc("POST "+pathUserBots+"/{name}/tokens/{tid}/delete", a.handleBotsTokenRevoke)
}

// botsPageData carries the rendered bot list plus a freshly minted token
// when one was just created. Token secrets are only ever shown once.
type botsPageData struct {
	webui.PageData
	Bots     []bots.Bot
	Tokens   map[string][]bots.Token
	NewToken string
	NewBot   string
	MaxBots  int
}

// renderBotsPage lists the caller's bots with their token metadata.
func (a *App) renderBotsPage(w http.ResponseWriter, r *http.Request, sess session.Data, newToken, newBot string) {
	jid := sess.JID()
	data := botsPageData{MaxBots: 10}

	list, err := a.Bots.List(r.Context(), jid)
	if err != nil {
		a.recordError(r, err)
		data.Errors = append(data.Errors, "The bot service could not be reached. Try again later.")
	} else {
		data.Bots = list
		data.Tokens = make(map[string][]bots.Token, len(list))
		for i := range list {
			if toks, terr := a.Bots.ListTokens(r.Context(), list[i].Name); terr == nil {
				data.Tokens[list[i].Name] = toks
			}
		}
	}
	data.NewToken, data.NewBot = newToken, newBot

	page := a.newPage(w, r, sess, "Bots", "bots", webui.ShellApp)
	data.PageData = page
	a.render(w, r, http.StatusOK, "bots.html", data)
}

// handleBotsPage shows the bot list.
func (a *App) handleBotsPage(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	a.renderBotsPage(w, r, sess, "", "")
}

// handleBotsCreate registers a new bot for the caller.
func (a *App) handleBotsCreate(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.FormValue("name")))
	label := strings.TrimSpace(r.FormValue("label"))
	if len(label) > 128 {
		label = label[:128]
	}
	if !bots.ValidName(name) {
		a.flashRedirect(w, r, sess,
			"Bot names must be 1-48 characters of lowercase letters, digits, dots, underscores or hyphens.",
			"alert", pathUserBots)
		return
	}

	bot, err := a.Bots.Create(r.Context(), name, sess.JID(), label)
	if err != nil {
		a.flashRedirect(w, r, sess, botErrorMessage(err), "alert", pathUserBots)
		return
	}
	a.recordAudit(r, sess, "bots.create", name, "self-service")
	a.renderBotsPage(w, r, sess, bot.Token, bot.Name)
}

// handleBotsDelete removes a bot the caller owns.
func (a *App) handleBotsDelete(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	if _, ok := a.botOwnedBy(w, r, sess, name); !ok {
		return
	}
	if err := a.Bots.Delete(r.Context(), name); err != nil {
		a.flashRedirect(w, r, sess, botErrorMessage(err), "alert", pathUserBots)
		return
	}
	a.recordAudit(r, sess, "bots.delete", name, "self-service")
	a.flashRedirect(w, r, sess, "Bot "+name+" deleted.", "success", pathUserBots)
}

// handleBotsToggle flips the disabled flag of a caller owned bot.
func (a *App) handleBotsToggle(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	bot, ok := a.botOwnedBy(w, r, sess, name)
	if !ok {
		return
	}
	if err := a.Bots.SetDisabled(r.Context(), name, !bot.Disabled); err != nil {
		a.flashRedirect(w, r, sess, botErrorMessage(err), "alert", pathUserBots)
		return
	}
	action := "enabled"
	if !bot.Disabled {
		action = "disabled"
	}
	a.recordAudit(r, sess, "bots."+action, name, "self-service")
	a.flashRedirect(w, r, sess, "Bot "+name+" "+action+".", "success", pathUserBots)
}

// handleBotsTokenMint creates a scoped token for a caller owned bot.
func (a *App) handleBotsTokenMint(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	if _, ok := a.botOwnedBy(w, r, sess, name); !ok {
		return
	}
	tokenName := strings.TrimSpace(r.FormValue("token_name"))
	if len(tokenName) > 64 {
		tokenName = tokenName[:64]
	}
	var ttl int64
	if raw := strings.TrimSpace(r.FormValue("ttl_days")); raw != "" {
		if days, err := strconv.Atoi(raw); err == nil && days > 0 && days <= 365 {
			ttl = int64(days) * int64(86400)
		}
	}
	token, _, err := a.Bots.MintToken(r.Context(), name, tokenName, nil, ttl)
	if err != nil {
		a.flashRedirect(w, r, sess, botErrorMessage(err), "alert", pathUserBots)
		return
	}
	a.recordAudit(r, sess, "bots.token_mint", name, "self-service")
	a.renderBotsPage(w, r, sess, token, name)
}

// handleBotsTokenRevoke deletes one token of a caller owned bot.
func (a *App) handleBotsTokenRevoke(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return
	}
	name, tid := r.PathValue("name"), r.PathValue("tid")
	if _, ok := a.botOwnedBy(w, r, sess, name); !ok {
		return
	}
	if err := a.Bots.RevokeToken(r.Context(), name, tid); err != nil {
		a.flashRedirect(w, r, sess, botErrorMessage(err), "alert", pathUserBots)
		return
	}
	a.recordAudit(r, sess, "bots.token_revoke", name, "self-service")
	a.flashRedirect(w, r, sess, "Token revoked.", "success", pathUserBots)
}

// botOwnedBy verifies the caller owns the named bot. It returns the bot
// record on success and has already written the response on failure.
func (a *App) botOwnedBy(w http.ResponseWriter, r *http.Request, sess session.Data, name string) (*bots.Bot, bool) {
	list, err := a.Bots.List(r.Context(), sess.JID())
	if err != nil {
		a.recordError(r, err)
		a.flashRedirect(w, r, sess, "The bot service could not be reached. Try again later.", "alert", pathUserBots)
		return nil, false
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i], true
		}
	}
	a.flashRedirect(w, r, sess, "Unknown bot.", "alert", pathUserBots)
	return nil, false
}

// botErrorMessage turns a management API error into a readable message.
func botErrorMessage(err error) string {
	var apiErr *bots.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Err {
		case "invalid-name":
			return "That bot name is not usable."
		case "conflict":
			return "A bot with that name already exists."
		case "owner-quota":
			return "You have reached the maximum number of bots."
		case "global-quota":
			return "The bot service is at capacity. Contact the operator."
		case "owner-unknown", "owner-not-local", "invalid-owner":
			return "Your account is not known to the bot service."
		case "token-quota":
			return "This bot has too many tokens. Revoke one first."
		case "not-found":
			return "Unknown bot."
		case "rate-limited":
			return "Too many requests. Try again in a moment."
		}
		return "The bot service refused the request: " + apiErr.Err
	}
	return "The bot service could not be reached. Try again later."
}
