// Package webui parses and renders the HTML templates of the web portal. The
// templates are supplied as an fs.FS so the caller decides whether they come
// from an embedded filesystem or from disk.
package webui

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/colour"
	"github.com/sudo-ivan/snikketx/web-portal/internal/csrf"
	"github.com/sudo-ivan/snikketx/web-portal/internal/i18n"
)

// Shell selects the page frame the base layout wraps the page content in.
const (
	// ShellApp is the signed in frame with the top bar and the footer.
	ShellApp = ""
	// ShellAdmin is the administration frame with the sidebar.
	ShellAdmin = "admin"
	// ShellBare is the centred single card frame used by the login, invite
	// and error pages.
	ShellBare = "bare"
)

// rootTemplate is the entry point every HTML page is rendered through.
const rootTemplate = "layout"

// sharedFiles are parsed into every page template set. They carry the layouts
// and the reusable fragments and never define a page of their own.
var sharedFiles = []string{"layout.html", "admin_layout.html", "partials.html"}

// Options tune the renderer.
type Options struct {
	// Sprite is an SVG sprite inlined into every page by the sprite
	// template function, so icons resolve as same document references.
	Sprite []byte
}

// Renderer holds one parsed template set per page.
type Renderer struct {
	pages map[string]*template.Template
	plain map[string]*template.Template
}

// New parses every page found in fsys. Files listed in sharedFiles are treated
// as layouts and parsed into each page set. Files that do not end in .html are
// parsed on their own and rendered through RenderPlain.
func New(fsys fs.FS, opts Options) (*Renderer, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("webui: read templates: %w", err)
	}

	funcs := funcMap(opts)
	renderer := &Renderer{
		pages: map[string]*template.Template{},
		plain: map[string]*template.Template{},
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if isShared(name) {
			continue
		}

		if !strings.HasSuffix(name, ".html") {
			set, err := template.New(name).Funcs(funcs).ParseFS(fsys, name)
			if err != nil {
				return nil, fmt.Errorf("webui: parse %s: %w", name, err)
			}
			renderer.plain[name] = set
			continue
		}

		files := make([]string, 0, len(sharedFiles)+1)
		files = append(files, sharedFiles...)
		files = append(files, name)

		set, err := template.New(name).Funcs(funcs).ParseFS(fsys, files...)
		if err != nil {
			return nil, fmt.Errorf("webui: parse %s: %w", name, err)
		}
		if set.Lookup(rootTemplate) == nil {
			return nil, fmt.Errorf("webui: %s: no %q template", name, rootTemplate)
		}
		renderer.pages[name] = set
	}

	if len(renderer.pages) == 0 {
		return nil, errors.New("webui: no page templates found")
	}
	return renderer, nil
}

// isShared reports whether name is a layout or fragment file.
func isShared(name string) bool {
	return slices.Contains(sharedFiles, name)
}

// Render writes the named page with a 200 status.
func (r *Renderer) Render(w http.ResponseWriter, name string, data any) error {
	return r.RenderStatus(w, http.StatusOK, name, data)
}

// RenderStatus writes the named page with the given status. The page is
// rendered into a buffer first so a template failure does not leave a half
// written response behind.
func (r *Renderer) RenderStatus(w http.ResponseWriter, status int, name string, data any) error {
	set, ok := r.pages[name]
	if !ok {
		return fmt.Errorf("webui: unknown page %q", name)
	}

	var buf bytes.Buffer
	if err := set.ExecuteTemplate(&buf, rootTemplate, data); err != nil {
		return fmt.Errorf("webui: render %s: %w", name, err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// RenderPlain writes a non HTML template such as security.txt.
func (r *Renderer) RenderPlain(w http.ResponseWriter, name, contentType string, data any) error {
	set, ok := r.plain[name]
	if !ok {
		return fmt.Errorf("webui: unknown plain template %q", name)
	}

	var buf bytes.Buffer
	if err := set.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("webui: render %s: %w", name, err)
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, err := buf.WriteTo(w)
	return err
}

// Flash is a one shot message carried across a redirect.
type Flash struct {
	Message  string
	Category string
}

// UserSummary is the profile summary the layout needs for the top bar.
type UserSummary struct {
	Address     string
	Username    string
	Nickname    string
	DisplayName string
	AvatarHash  string
	IsAdmin     bool
}

// PageData is the base data every page template can rely on. Page specific
// structs embed it so its fields stay reachable as top level template fields.
type PageData struct {
	Title               string
	SiteName            string
	Domain              string
	Lang                string
	Shell               string
	Nav                 string
	Theme               string
	CSRF                string
	RequestID           string
	Version             string
	BuildCommit         string
	BuildDate           string
	Uptime              string
	HealthStatus        string
	HealthLabel         string
	HasSession          bool
	IsAdmin             bool
	ShowMetrics         bool
	TOSURI              string
	PrivacyURI          string
	AbuseEmail          string
	SecurityEmail       string
	AppleStoreURL       string
	PlayStoreURL        string
	FDroidURL           string
	AndroidAPKReady     bool
	AndroidDownloadURL  string
	AndroidAPKVersion   string
	AndroidAPKSizeLabel string
	Flash               *Flash
	User                *UserSummary
	Errors              []string
	Now                 time.Time
}

// AddError appends a form or validation error shown at the top of the page.
func (p *PageData) AddError(format string, args ...any) {
	p.Errors = append(p.Errors, fmt.Sprintf(format, args...))
}

// HasErrors reports whether any validation error was recorded.
func (p PageData) HasErrors() bool {
	return len(p.Errors) > 0
}

// funcMap builds the function map shared by every template set.
func funcMap(opts Options) template.FuncMap {
	// #nosec G203 -- sprite is a build-time Lucide SVG asset, never user input
	sprite := template.HTML(opts.Sprite)

	return template.FuncMap{
		"t":                i18n.T,
		"csrf":             csrfField,
		"icon":             Icon,
		"sprite":           func() template.HTML { return sprite },
		"colour":           colour.TextToCSS,
		"formatBytes":      FormatBytes,
		"formatPercent":    FormatPercent,
		"formatFloat":      FormatFloat,
		"formatTime":       FormatTime,
		"formatUnix":       FormatUnix,
		"formatUnixInt":    FormatUnixInt,
		"formatAgoUnix":    FormatAgoUnix,
		"formatAgo":        FormatAgo,
		"formatRFC3339Ago": FormatRFC3339Ago,
		"formatRFC3339":    FormatRFC3339Time,
		"formatLeft":       FormatLeft,
		"safeURL":          safeURL,
		"dict":             dict,
		"list":             list,
		"add":              func(a, b int) int { return a + b },
		"join":             strings.Join,
		"hasPrefix":        strings.HasPrefix,
		"lower":            strings.ToLower,
		"contains":         contains,
		"initials":         Initials,
		"avatarURL":        AvatarURL,
		"roleLabel":        RoleLabel,
		"queryEscape":      url.QueryEscape,
		"orDefault":        orDefault,
	}
}

// csrfField renders the hidden form field carrying the CSRF token.
func csrfField(token string) template.HTML {
	var b strings.Builder
	b.WriteString(`<input type="hidden" name="`)
	b.WriteString(template.HTMLEscapeString(csrf.FieldName))
	b.WriteString(`" value="`)
	b.WriteString(template.HTMLEscapeString(token))
	b.WriteString(`">`)
	// #nosec G203 -- field name and token are HTML-escaped above
	return template.HTML(b.String())
}

// Icon renders a reference to a symbol of the inlined SVG sprite.
func Icon(name string) template.HTML {
	id := iconID(name)
	if id == "" {
		return ""
	}
	// #nosec G203 -- id is reduced to [a-z0-9-] only by iconID
	return template.HTML(`<svg class="icon" aria-hidden="true" focusable="false"><use href="#icon-` +
		id + `"></use></svg>`)
}

// iconID reduces a symbol name to the characters a sprite id may contain.
func iconID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == '_', r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// FormatBytes renders a byte count with a binary unit suffix.
func FormatBytes(value any) string {
	n, ok := toFloat(value)
	if !ok {
		return "n/a"
	}
	negative := n < 0
	if negative {
		n = -n
	}

	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	index := 0
	for n >= 1024 && index < len(units)-1 {
		n /= 1024
		index++
	}

	sign := ""
	if negative {
		sign = "-"
	}
	if index == 0 {
		return fmt.Sprintf("%s%.0f %s", sign, n, units[index])
	}
	return fmt.Sprintf("%s%.1f %s", sign, n, units[index])
}

// FormatPercent renders a ratio between zero and one as a percentage.
func FormatPercent(value any) string {
	n, ok := toFloat(value)
	if !ok {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", n*100)
}

// FormatFloat renders a floating point value with two decimal places.
func FormatFloat(value any) string {
	n, ok := toFloat(value)
	if !ok {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", n)
}

// FormatTime renders a timestamp in UTC, or a dash when it carries no value.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// FormatUnix renders an optional Unix timestamp.
func FormatUnix(seconds *int64) string {
	if seconds == nil || *seconds == 0 {
		return "never"
	}
	return FormatTime(time.Unix(*seconds, 0))
}

// FormatUnixInt renders a Unix timestamp seconds value.
func FormatUnixInt(seconds int64) string {
	if seconds == 0 {
		return "never"
	}
	return FormatTime(time.Unix(seconds, 0))
}

// FormatAgoUnix renders how long ago an optional Unix timestamp was.
func FormatAgoUnix(seconds *int64) string {
	if seconds == nil || *seconds == 0 {
		return "never"
	}
	return FormatAgo(time.Unix(*seconds, 0))
}

// FormatAgo renders how long ago a timestamp was.
func FormatAgo(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return humanDuration(time.Since(t)) + " ago"
}

// FormatRFC3339Ago renders an RFC3339 timestamp as a relative phrase.
func FormatRFC3339Ago(value string) string {
	t, ok := parseRFC3339(value)
	if !ok {
		return "never"
	}
	return FormatAgo(t)
}

// FormatRFC3339Time renders an RFC3339 timestamp in UTC, or a dash.
func FormatRFC3339Time(value string) string {
	t, ok := parseRFC3339(value)
	if !ok {
		return "never"
	}
	return FormatTime(t)
}

func parseRFC3339(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// FormatLeft renders how long is left until a timestamp.
func FormatLeft(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	remaining := time.Until(t)
	if remaining <= 0 {
		return "expired"
	}
	return humanDuration(remaining) + " left"
}

// humanDuration renders a duration with a single coarse unit.
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		return plural(int(d.Hours()), "hour")
	default:
		return plural(int(d.Hours()/24), "day")
	}
}

// plural renders a count with a naively pluralised unit.
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// Initials returns up to two upper case letters standing in for an avatar.
func Initials(name string) string {
	fields := strings.Fields(strings.TrimSpace(name))
	if len(fields) == 0 {
		return "?"
	}
	first := []rune(fields[0])
	out := strings.ToUpper(string(first[0]))
	if len(fields) > 1 {
		second := []rune(fields[1])
		out += strings.ToUpper(string(second[0]))
	} else if len(first) > 1 {
		out += strings.ToLower(string(first[1]))
	}
	return out
}

// AvatarURL builds the portal URL serving the avatar of an address. It returns
// an empty string when the entity publishes no avatar.
func AvatarURL(address, avatarHash string) string {
	if address == "" || avatarHash == "" {
		return ""
	}
	raw, err := hex.DecodeString(avatarHash)
	if err != nil {
		return ""
	}
	if len(raw) > 8 {
		raw = raw[:8]
	}
	from := base64.RawURLEncoding.EncodeToString([]byte(address))
	code := base64.RawURLEncoding.EncodeToString(raw)
	return "/avatar/" + from + "/" + code
}

// RoleLabel turns a Prosody role name into the wording used by the portal.
func RoleLabel(role string) string {
	switch role {
	case "prosody:restricted":
		return "Limited"
	case "prosody:registered":
		return "Normal user"
	case "prosody:admin":
		return "Administrator"
	case "":
		return "Normal user"
	default:
		return role
	}
}

// safeURL marks a URL built by the portal as trusted when the scheme is one
// of the allowlisted values used by the invite and store links.
func safeURL(raw string) template.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "xmpp", "market", "mailto":
		// #nosec G203 -- scheme is allowlisted and the value is portal-built
		return template.URL(raw)
	default:
		return ""
	}
}

// dict builds a map from alternating key and value arguments so a fragment can
// be called with more than one parameter.
func dict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, errors.New("dict: odd number of arguments")
	}
	out := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, errors.New("dict: keys must be strings")
		}
		out[key] = values[i+1]
	}
	return out, nil
}

// list collects its arguments into a slice.
func list(values ...any) []any {
	return values
}

// contains reports whether needle is one of the values in haystack.
func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

// orDefault returns fallback when value is empty.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// toFloat converts the numeric shapes that reach the templates from JSON.
func toFloat(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case *int64:
		if n == nil {
			return 0, false
		}
		return float64(*n), true
	case *float64:
		if n == nil {
			return 0, false
		}
		return *n, true
	default:
		return 0, false
	}
}
