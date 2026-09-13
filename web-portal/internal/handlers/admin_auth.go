package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

// mountAdminAuth registers the authentication overview page. It reports
// the effective server side auth setup (internal, LDAP, OAuth) plus the
// portal single sign-on state, and can generate a snikket.conf snippet
// the operator pastes into the deployment config.
func (a *App) mountAdminAuth(mux *http.ServeMux) {
	mux.HandleFunc("GET "+pathAdminAuth, a.handleAdminAuthPage)
	mux.HandleFunc("POST "+pathAdminAuth, a.handleAdminAuthSnippet)
}

// adminAuthData is the auth overview plus an optional generated snippet.
type adminAuthData struct {
	webui.PageData
	Server  *prosody.AuthConfig
	OIDCOn  bool
	Issuer  string
	Client  string
	Claim   string
	Scopes  string
	Snippet string
}

func (a *App) renderAdminAuth(w http.ResponseWriter, r *http.Request, sess session.Data, snippet string, status int) {
	data := adminAuthData{Snippet: snippet}
	if cfg, err := a.Prosody.GetAuthConfig(r.Context(), sess.Token()); err == nil {
		data.Server = cfg
	} else {
		data.Errors = append(data.Errors, "The server did not report its authentication setup. Is mod_snikket_ops_api enabled?")
	}
	if o := a.Cfg.OIDC; o != nil {
		data.OIDCOn = true
		data.Issuer = o.Issuer
		data.Client = o.ClientID
		data.Claim = o.UsernameClaim
		data.Scopes = strings.Join(o.Scopes, " ")
	}
	data.PageData = a.newPage(w, r, sess, "Authentication", "auth", webui.ShellAdmin)
	a.render(w, r, status, "admin_auth.html", data)
}

func (a *App) handleAdminAuthPage(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	a.renderAdminAuth(w, r, sess, "", http.StatusOK)
}

// handleAdminAuthSnippet builds a ready to paste snikket.conf block for
// the requested mode. The values come from the form, secrets stay out.
func (a *App) handleAdminAuthSnippet(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	snippet := buildAuthSnippet(r)
	if snippet == "" {
		page := a.newPage(w, r, sess, "Authentication", "auth", webui.ShellAdmin)
		page.AddError("%s", "Fill in the required fields for the chosen mode.")
		data := adminAuthData{PageData: page}
		if cfg, err := a.Prosody.GetAuthConfig(r.Context(), sess.Token()); err == nil {
			data.Server = cfg
		}
		a.render(w, r, http.StatusBadRequest, "admin_auth.html", data)
		return
	}
	a.recordAudit(r, sess, "auth.snippet", r.FormValue("mode"), "")
	a.renderAdminAuth(w, r, sess, snippet, http.StatusOK)
}

// buildAuthSnippet renders the snikket.conf block for the form values.
// The mode selects LDAP, OAuth (server side XEP-0493), or portal OIDC.
func buildAuthSnippet(r *http.Request) string {
	switch r.FormValue("mode") {
	case "ldap":
		host := strings.TrimSpace(r.FormValue("ldap_host"))
		base := strings.TrimSpace(r.FormValue("ldap_base_dn"))
		if host == "" || base == "" {
			return ""
		}
		var b strings.Builder
		b.WriteString("# LDAP authentication for snikket.conf\n")
		b.WriteString("SNIKKET_TWEAK_LDAP=1\n")
		fmt.Fprintf(&b, "SNIKKET_LDAP_HOST=%s\n", host)
		if v := strings.TrimSpace(r.FormValue("ldap_port")); v != "" {
			fmt.Fprintf(&b, "SNIKKET_LDAP_PORT=%s\n", v)
		}
		if v := r.FormValue("ldap_tls"); v != "" {
			fmt.Fprintf(&b, "SNIKKET_LDAP_USE_TLS=%s\n", v)
		}
		fmt.Fprintf(&b, "SNIKKET_LDAP_USER_BASE_DN=%s\n", base)
		if v := strings.TrimSpace(r.FormValue("ldap_bind_dn")); v != "" {
			fmt.Fprintf(&b, "SNIKKET_LDAP_BIND_DN=%s\n", v)
			b.WriteString("SNIKKET_LDAP_BIND_PASSWORD=change-me\n")
		}
		if v := strings.TrimSpace(r.FormValue("ldap_username_field")); v != "" {
			fmt.Fprintf(&b, "SNIKKET_LDAP_USERNAME_FIELD=%s\n", v)
		}
		if v := strings.TrimSpace(r.FormValue("ldap_user_filter")); v != "" {
			fmt.Fprintf(&b, "SNIKKET_LDAP_USER_FILTER=%s\n", v)
		}
		return b.String()
	case "oauth":
		discovery := strings.TrimSpace(r.FormValue("oauth_discovery"))
		endpoint := strings.TrimSpace(r.FormValue("oauth_endpoint"))
		if discovery == "" && endpoint == "" {
			return ""
		}
		var b strings.Builder
		b.WriteString("# OAuth bearer authentication for snikket.conf\n")
		b.WriteString("SNIKKET_TWEAK_OAUTH=1\n")
		if discovery != "" {
			fmt.Fprintf(&b, "SNIKKET_OAUTH_DISCOVERY_URL=%s\n", discovery)
		}
		if endpoint != "" {
			fmt.Fprintf(&b, "SNIKKET_OAUTH_VALIDATION_ENDPOINT=%s\n", endpoint)
		}
		if v := strings.TrimSpace(r.FormValue("oauth_username_field")); v != "" {
			fmt.Fprintf(&b, "SNIKKET_OAUTH_USERNAME_FIELD=%s\n", v)
		}
		if v := strings.TrimSpace(r.FormValue("oauth_scope")); v != "" {
			fmt.Fprintf(&b, "SNIKKET_OAUTH_SCOPE=%s\n", v)
		}
		return b.String()
	case "oidc":
		issuer := strings.TrimSpace(r.FormValue("oidc_issuer"))
		client := strings.TrimSpace(r.FormValue("oidc_client_id"))
		if issuer == "" || client == "" {
			return ""
		}
		var b strings.Builder
		b.WriteString("# Portal single sign-on for snikket.conf\n")
		fmt.Fprintf(&b, "SNIKKET_WEB_OIDC_ISSUER=%s\n", issuer)
		fmt.Fprintf(&b, "SNIKKET_WEB_OIDC_CLIENT_ID=%s\n", client)
		b.WriteString("SNIKKET_WEB_OIDC_CLIENT_SECRET=change-me\n")
		if v := strings.TrimSpace(r.FormValue("oidc_claim")); v != "" {
			fmt.Fprintf(&b, "SNIKKET_WEB_OIDC_USERNAME_CLAIM=%s\n", v)
		}
		if v := strings.TrimSpace(r.FormValue("oidc_scopes")); v != "" {
			fmt.Fprintf(&b, "SNIKKET_WEB_OIDC_SCOPES=%s\n", v)
		}
		return b.String()
	}
	return ""
}
