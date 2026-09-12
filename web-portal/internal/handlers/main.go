package handlers

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
	"github.com/sudo-ivan/snikketx/web-portal/internal/csrf"
	"github.com/sudo-ivan/snikketx/web-portal/internal/health"
	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

// mountMain registers the routes that are not specific to a signed in user.
func (a *App) mountMain(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", a.handleIndex)
	mux.HandleFunc("GET /-", a.handleIndex)
	mux.HandleFunc("GET "+pathLogin, a.handleLoginForm)
	mux.HandleFunc("POST "+pathLogin, a.handleLoginSubmit)
	mux.HandleFunc("GET /meta/about.html", a.handleAbout)
	mux.HandleFunc("GET /policies/", a.handlePolicies)
	mux.HandleFunc("GET /terms", a.handleTerms)
	mux.HandleFunc("GET /privacy", a.handlePrivacy)
	mux.HandleFunc("GET /.well-known/security.txt", a.handleSecurityTxt)
	mux.HandleFunc("GET /site.webmanifest", a.handleWebManifest)
	mux.HandleFunc("GET /avatar/{from}/{code}", a.handleAvatar)
	mux.HandleFunc("HEAD /avatar/{from}/{code}", a.handleAvatar)
	mux.HandleFunc("GET /_health", a.handleHealth)
	mux.HandleFunc("GET /_health/ready", a.handleReady)
	mux.HandleFunc("GET /download/android.apk", a.handleAndroidAPK)
	mux.HandleFunc("HEAD /download/android.apk", a.handleAndroidAPK)
}

// handleIndex sends the caller to their home page or to the login page.
func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if a.Sessions.Get(r).HasSession() {
		http.Redirect(w, r, pathUserHome, http.StatusSeeOther)
		return
	}
	a.redirectToLogin(w, r)
}

// loginPage is the data behind the login form.
type loginPage struct {
	webui.PageData
	Address string
}

// handleLoginForm shows the login form, or skips it when the caller already
// holds a working session.
func (a *App) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.Get(r)
	if sess.HasSession() {
		ok, err := a.Prosody.TestSession(r.Context(), sess.Token(), sess.JID())
		if err == nil && ok {
			http.Redirect(w, r, pathUserHome, http.StatusSeeOther)
			return
		}
		sess.ClearAuth()
	}

	page := a.newPage(w, r, sess, "Sign in", "", webui.ShellBare)
	a.render(w, r, http.StatusOK, "login.html", loginPage{PageData: page})
}

// handleLoginSubmit exchanges the submitted credentials for a bearer token.
// Only accounts on the configured domain are accepted, so a password meant for
// another server is never forwarded. Failed attempts are rate limited and take
// a fixed minimum latency so timing does not reveal which check failed.
func (a *App) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	sess := a.Sessions.Get(r)
	address := strings.TrimSpace(r.FormValue("address"))
	password := r.FormValue(fieldPassword)
	ip := authlimit.ClientIP(r)

	localpart, domain, _ := xmpp.SplitJID(address)
	if localpart == "" {
		localpart, domain = domain, a.Cfg.Domain
	}
	localpart = strings.ToLower(strings.TrimSpace(localpart))

	gate := a.LoginGate
	if gate == nil {
		gate = authlimit.New()
		a.LoginGate = gate
	}

	fail := func(message string, status int) {
		a.padLogin(started, gate.MinLatency())
		page := a.newPage(w, r, sess, "Sign in", "", webui.ShellBare)
		page.AddError("%s", message)
		a.render(w, r, status, "login.html", loginPage{
			PageData: page,
			Address:  address,
		})
	}

	decision := gate.Allow(ip, localpart)
	if !decision.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())+1))
		fail("Too many sign-in attempts. Try again later.", http.StatusTooManyRequests)
		return
	}

	switch {
	case localpart == "" || password == "":
		gate.Failure(ip, localpart)
		fail("Enter your username and your password.", http.StatusUnauthorized)
		return
	case len(localpart) > gate.MaxLocalpartLen() || len(password) > gate.MaxPasswordLen():
		gate.Failure(ip, localpart)
		fail(errCredentials, http.StatusUnauthorized)
		return
	case !strings.EqualFold(domain, a.Cfg.Domain):
		gate.Failure(ip, localpart)
		fail(errCredentials, http.StatusUnauthorized)
		return
	}

	jid := localpart + "@" + a.Cfg.Domain
	tokenInfo, err := a.Prosody.Login(r.Context(), jid, password)
	if err != nil {
		gate.Failure(ip, localpart)
		if errors.Is(err, prosody.ErrInvalidCredentials) || prosody.StatusOf(err) == http.StatusUnauthorized {
			fail(errCredentials, http.StatusUnauthorized)
			return
		}
		a.padLogin(started, gate.MinLatency())
		a.failAPI(w, r, err)
		return
	}

	gate.Success(ip, localpart)
	sess.RotateAuthSurface()
	sess.SetAuth(tokenInfo.Token, strings.Join(tokenInfo.Scopes, " "), jid)
	_ = csrf.Rotate(sess)
	a.padLogin(started, gate.MinLatency())
	a.recordAudit(r, sess, "auth.login", jid, "")
	a.flashRedirect(w, r, sess, "Login successful.", "success", pathUserHome)
}

// padLogin waits until the login attempt has taken at least min so early
// rejects do not become a timing oracle.
func (a *App) padLogin(started time.Time, min time.Duration) {
	if min <= 0 {
		return
	}
	if left := min - time.Since(started); left > 0 {
		timer := time.NewTimer(left)
		<-timer.C
	}
}

// aboutPage lists the software versions of the deployment.
type aboutPage struct {
	webui.PageData
	Versions []versionEntry
}

// versionEntry is one named component version.
type versionEntry struct {
	Name    string
	Version string
}

// handleAbout shows the about page. Component versions are only disclosed to
// administrators.
func (a *App) handleAbout(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.Get(r)
	page := a.newPage(w, r, sess, "About this service", "", webui.ShellBare)

	data := aboutPage{PageData: page}
	if sess.IsAdmin() {
		data.Versions = append(data.Versions, versionEntry{Name: "Web portal", Version: a.Cfg.Version})
		version, err := a.Prosody.GetServerVersion(r.Context(), sess.Token(), sess.JID())
		if err != nil {
			version = "unknown"
		}
		data.Versions = append(data.Versions, versionEntry{Name: "Prosody", Version: version})
	}
	a.render(w, r, http.StatusOK, "about.html", data)
}

// handlePolicies shows the service policy overview linked from the apps.
func (a *App) handlePolicies(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.Get(r)
	page := a.newPage(w, r, sess, "Policies", "", webui.ShellBare)
	a.render(w, r, http.StatusOK, "policies.html", page)
}

// handleTerms redirects to the configured terms of service document.
func (a *App) handleTerms(w http.ResponseWriter, r *http.Request) {
	a.redirectToPolicy(w, r, a.Cfg.TOSURI)
}

// handlePrivacy redirects to the configured privacy policy document.
func (a *App) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	a.redirectToPolicy(w, r, a.Cfg.PrivacyURI)
}

// redirectToPolicy forwards to an external policy document, or reports 404
// when the operator configured none.
func (a *App) redirectToPolicy(w http.ResponseWriter, r *http.Request, target string) {
	if target == "" {
		a.notFound(w, r)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleSecurityTxt serves the security contact document.
func (a *App) handleSecurityTxt(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Domain        string
		SecurityEmail string
	}{Domain: a.Cfg.Domain, SecurityEmail: a.Cfg.SecurityEmail}

	if err := a.Templates.RenderPlain(w, "security.txt", "text/plain; charset=utf-8", data); err != nil {
		a.recordError(r, err)
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}
}

// handleWebManifest serves the progressive web app manifest that carries the
// installable icons.
func (a *App) handleWebManifest(w http.ResponseWriter, r *http.Request) {
	type icon struct {
		Src   string `json:"src"`
		Sizes string `json:"sizes"`
		Type  string `json:"type"`
	}

	manifest := struct {
		Name            string `json:"name"`
		ShortName       string `json:"short_name"`
		Icons           []icon `json:"icons"`
		ThemeColor      string `json:"theme_color"`
		BackgroundColor string `json:"background_color"`
		Display         string `json:"display"`
		StartURL        string `json:"start_url"`
	}{
		Name:      "SnikketX",
		ShortName: "SnikketX",
		Icons: []icon{
			{Src: "/static/img/android-chrome-192x192.png", Sizes: "192x192", Type: "image/png"},
			{Src: "/static/img/android-chrome-256x256.png", Sizes: "256x256", Type: "image/png"},
			{Src: "/static/img/android-chrome-512x512.png", Sizes: "512x512", Type: "image/png"},
		},
		ThemeColor:      "#09090b",
		BackgroundColor: "#f5f8ff",
		Display:         "standalone",
		StartURL:        "/",
	}

	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if err := json.NewEncoder(w).Encode(manifest); err != nil {
		a.recordError(r, err)
	}
}

// handleAvatar serves the avatar published by an entity. The path carries the
// address and a truncated hash so a changed avatar yields a fresh URL.
func (a *App) handleAvatar(w http.ResponseWriter, r *http.Request) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(r.PathValue("from"), "="))
	if err != nil {
		a.notFound(w, r)
		return
	}
	address := string(raw)

	sess := a.Sessions.Get(r)
	info, err := a.Prosody.GetAvatar(r.Context(), sess.Token(), address, true)
	if err != nil {
		if errors.Is(err, prosody.ErrNoAvatar) || prosody.StatusOf(err) == http.StatusNotFound {
			a.notFound(w, r)
			return
		}
		a.failAPI(w, r, err)
		return
	}

	digest, err := hex.DecodeString(info.SHA1)
	if err != nil {
		a.notFound(w, r)
		return
	}
	etag := `"` + base64.RawURLEncoding.EncodeToString(digest) + `"`

	header := w.Header()
	header.Set("Content-Type", info.Type)
	header.Set("ETag", etag)
	header.Set("Cache-Control", "private, max-age="+strconv.Itoa(int(a.Cfg.AvatarCacheTTL.Seconds())))
	header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")

	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, strings.Trim(etag, `"`)) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	data, err := a.Prosody.GetAvatarData(r.Context(), sess.Token(), address, info.SHA1)
	if err != nil {
		a.failAPI(w, r, err)
		return
	}
	if len(data) == 0 {
		a.notFound(w, r)
		return
	}

	header.Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data) // #nosec G705 -- avatar bytes served with Content-Type from Prosody metadata
}

// handleHealth is the liveness probe. It never touches the chat server so a
// backend outage does not restart the portal.
func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("STATUS OK"))
}

// handleReady is the readiness probe. It reports 503 while the chat server is
// unreachable, so a proxy stops sending traffic that cannot be served.
func (a *App) handleReady(w http.ResponseWriter, r *http.Request) {
	component := a.probeProsody(r.Context())

	status := http.StatusOK
	body := "STATUS READY"
	if !component.OK {
		status = http.StatusServiceUnavailable
		body = "STATUS UNAVAILABLE"
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\nprosody: " + component.Detail + "\n"))
}

// probeProsody checks that the chat server answers. Any HTTP status counts as
// reachable, including a refusal, because that still proves the service is
// listening and routing requests.
func (a *App) probeProsody(ctx context.Context) health.Component {
	start := time.Now()
	component := health.Component{Name: "prosody"}

	var err error
	if a.Prosody.IsClientRegistered() {
		_, err = a.Prosody.GetSystemMetrics(ctx, "")
	} else {
		err = a.Prosody.RegisterClient(ctx)
	}

	switch {
	case err == nil:
		component.OK = true
		component.Detail = "reachable"
	case prosody.StatusOf(err) > 0:
		component.OK = true
		component.Detail = "reachable, answered with status " + strconv.Itoa(prosody.StatusOf(err))
	default:
		component.Detail = err.Error()
	}

	component.Latency = time.Since(start).Round(time.Millisecond).String()
	return component
}
