package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultStateDir is the base directory for the secret key, the audit
	// log, the OAuth client registration and the Android APK cache.
	defaultStateDir = "/var/lib/snikket-web-portal"
	// #nosec G101 -- filesystem file name for the secret key, not a credential
	secretKeyFileName = "secret_key"
	defaultAvatarTTL  = 1800
	defaultMaxAvatar  = 1024 * 1024
	defaultAppleStore = "https://apps.apple.com/us/app/snikket/id1544535398"
	defaultAndroidApp = "org.snikket.android"
	defaultPlayStore  = "https://play.google.com/store/apps/details?id="
	minSecretLen      = 32
)

type Config struct {
	SecretKey       []byte
	ProsodyEndpoint string
	Domain          string
	SiteName        string
	AvatarCacheTTL  time.Duration
	AppleStoreURL   string
	MaxAvatarSize   int64
	ShowMetrics     bool
	TOSURI          string
	PrivacyURI      string
	AbuseEmail      string
	SecurityEmail   string
	ListenAddr      string
	MetricsToken    string
	UpdaterEndpoint string
	UpdaterToken    string
	BackupEndpoint  string
	BackupToken     string
	Version         string
	BuildCommit     string
	BuildDate       string
	StateDir        string
	LogLevel        slog.Level
	InsecureCookies bool

	// OIDC carries the external identity provider settings. Nil disables
	// single sign-on; it stays nil unless SNIKKET_WEB_OIDC_ISSUER is set.
	OIDC *OIDCConfig

	// ServiceAddress and ServicePassword are the credentials of a dedicated
	// Prosody account the portal uses for operator level calls that no
	// signed in user can authorise, like creating the XMPP account of a
	// single sign-on user and minting its app bootstrap invitation. The
	// account needs the prosody:admin role. Optional, but OIDC account
	// provisioning and app bootstrap do not work without it.
	ServiceAddress  string
	ServicePassword string
	// LinkPreviewEnabled gates the authenticated /api/link-preview
	// endpoints used by the Android app.
	LinkPreviewEnabled bool

	// Android host defaults seed portal_data/android/settings.json on first boot.
	AndroidHostEnabled       bool
	AndroidAPKSource         string
	AndroidPackageID         string
	PlayStoreURL             string
	FDroidURL                string
	AndroidDownloadLimitHour int
	AndroidRefreshHours      int
	// AndroidCertSHA256 is the SHA-256 fingerprint of the APK signing
	// certificate, published next to the download so installs can be
	// verified with tools like AppVerifier.
	AndroidCertSHA256 string
}

func Load(version, commit, buildDate string) (*Config, error) {
	bridgeSnikketEnv()

	domain := os.Getenv("SNIKKET_WEB_DOMAIN")
	if domain == "" {
		return nil, fmt.Errorf("SNIKKET_WEB_DOMAIN is required")
	}
	endpoint := strings.TrimRight(os.Getenv("SNIKKET_WEB_PROSODY_ENDPOINT"), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("SNIKKET_WEB_PROSODY_ENDPOINT is required")
	}

	stateDir := envOr("SNIKKET_WEB_STATE_DIR", defaultStateDir)

	logLevel := slog.LevelInfo
	switch strings.ToLower(os.Getenv("SNIKKET_WEB_LOG_LEVEL")) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	insecureCookies := false
	switch strings.ToLower(os.Getenv("SNIKKET_WEB_INSECURE_COOKIES")) {
	case "1", "true", "yes":
		insecureCookies = true
	}

	secret, err := loadOrCreateSecret(stateDir)
	if err != nil {
		return nil, err
	}
	if len(secret) < minSecretLen {
		return nil, fmt.Errorf("SNIKKET_WEB_SECRET_KEY must be at least %d bytes", minSecretLen)
	}

	siteName := os.Getenv("SNIKKET_WEB_SITE_NAME")
	if siteName == "" {
		siteName = domain
	}

	avatarTTL := defaultAvatarTTL
	if v := os.Getenv("SNIKKET_WEB_AVATAR_CACHE_TTL"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("SNIKKET_WEB_AVATAR_CACHE_TTL: %w", err)
		}
		avatarTTL = n
	}

	maxAvatar := int64(defaultMaxAvatar)
	if v := os.Getenv("SNIKKET_WEB_MAX_AVATAR_SIZE"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("SNIKKET_WEB_MAX_AVATAR_SIZE: %w", err)
		}
		maxAvatar = n
	}

	showMetrics := true
	if v := os.Getenv("SNIKKET_WEB_SHOW_METRICS"); v != "" {
		showMetrics, err = strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("SNIKKET_WEB_SHOW_METRICS: %w", err)
		}
	}

	metricsToken := os.Getenv("SNIKKET_WEB_METRICS_TOKEN")

	linkPreview := true
	if v := os.Getenv("SNIKKET_WEB_LINK_PREVIEW"); v != "" {
		linkPreview, err = strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("SNIKKET_WEB_LINK_PREVIEW: %w", err)
		}
	}

	apple := os.Getenv("SNIKKET_WEB_APPLE_STORE_URL")
	if apple == "" {
		apple = defaultAppleStore
	}
	if err := validateHTTPURL("SNIKKET_WEB_APPLE_STORE_URL", apple); err != nil {
		return nil, err
	}

	androidEnabled := false
	if v := os.Getenv("SNIKKET_WEB_ANDROID_HOST_ENABLED"); v != "" {
		androidEnabled, err = strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("SNIKKET_WEB_ANDROID_HOST_ENABLED: %w", err)
		}
	}
	androidSource := strings.TrimSpace(os.Getenv("SNIKKET_WEB_ANDROID_APK_SOURCE"))
	if androidSource != "" && !strings.HasPrefix(androidSource, "github:") {
		if err := validateHTTPURL("SNIKKET_WEB_ANDROID_APK_SOURCE", androidSource); err != nil {
			return nil, err
		}
	}
	androidPackage := envOr("SNIKKET_WEB_ANDROID_PACKAGE", defaultAndroidApp)
	playURL := strings.TrimSpace(os.Getenv("SNIKKET_WEB_PLAY_STORE_URL"))
	if playURL == "" {
		playURL = defaultPlayStore + url.QueryEscape(androidPackage)
	} else if err := validateHTTPURL("SNIKKET_WEB_PLAY_STORE_URL", playURL); err != nil {
		return nil, err
	}
	fdroidURL := strings.TrimSpace(os.Getenv("SNIKKET_WEB_FDROID_URL"))
	androidLimit := 6
	if v := os.Getenv("SNIKKET_WEB_ANDROID_DOWNLOAD_LIMIT"); v != "" {
		androidLimit, err = strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("SNIKKET_WEB_ANDROID_DOWNLOAD_LIMIT: %w", err)
		}
	}
	androidRefresh := 12
	if v := os.Getenv("SNIKKET_WEB_ANDROID_REFRESH_HOURS"); v != "" {
		androidRefresh, err = strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("SNIKKET_WEB_ANDROID_REFRESH_HOURS: %w", err)
		}
	}
	androidCertSHA256 := ""
	if raw := strings.TrimSpace(os.Getenv("SNIKKET_WEB_ANDROID_CERT_SHA256")); raw != "" {
		androidCertSHA256 = normalizeHexDigest(raw)
		if len(androidCertSHA256) != 64 {
			return nil, fmt.Errorf("SNIKKET_WEB_ANDROID_CERT_SHA256: must be a 64 character hex SHA-256 fingerprint")
		}
	}

	tos := os.Getenv("SNIKKET_WEB_TOS_URI")
	privacy := os.Getenv("SNIKKET_WEB_PRIVACY_URI")
	if tos != "" {
		if err := validateHTTPURL("SNIKKET_WEB_TOS_URI", tos); err != nil {
			return nil, err
		}
	}
	if privacy != "" {
		if err := validateHTTPURL("SNIKKET_WEB_PRIVACY_URI", privacy); err != nil {
			return nil, err
		}
	}

	iface := envOr("SNIKKET_TWEAK_PORTAL_INTERNAL_HTTP_INTERFACE", "0.0.0.0")
	port := envOr("SNIKKET_TWEAK_PORTAL_INTERNAL_HTTP_PORT", "5765")

	oidcCfg, err := loadOIDC(domain)
	if err != nil {
		return nil, err
	}

	serviceAddress := strings.TrimSpace(os.Getenv("SNIKKET_WEB_SERVICE_ADDRESS"))
	servicePassword := os.Getenv("SNIKKET_WEB_SERVICE_PASSWORD")
	if (serviceAddress == "") != (servicePassword == "") {
		return nil, fmt.Errorf("SNIKKET_WEB_SERVICE_ADDRESS and SNIKKET_WEB_SERVICE_PASSWORD must be set together")
	}

	if commit == "" {
		commit = "unknown"
	}
	if buildDate == "" {
		buildDate = "unknown"
	}

	return &Config{
		SecretKey:          secret,
		ProsodyEndpoint:    endpoint,
		Domain:             domain,
		SiteName:           siteName,
		AvatarCacheTTL:     time.Duration(avatarTTL) * time.Second,
		AppleStoreURL:      apple,
		MaxAvatarSize:      maxAvatar,
		ShowMetrics:        showMetrics,
		TOSURI:             tos,
		PrivacyURI:         privacy,
		AbuseEmail:         os.Getenv("SNIKKET_WEB_ABUSE_EMAIL"),
		SecurityEmail:      os.Getenv("SNIKKET_WEB_SECURITY_EMAIL"),
		ListenAddr:         iface + ":" + port,
		MetricsToken:       metricsToken,
		UpdaterEndpoint:    strings.TrimRight(envOr("SNIKKET_WEB_UPDATER_ENDPOINT", "http://snikket_updater:9191"), "/"),
		UpdaterToken:       envOr("SNIKKET_WEB_UPDATER_TOKEN", envOr("SNIKKET_UPDATER_TOKEN", "snikket-updater-local")),
		BackupEndpoint:     strings.TrimRight(envOr("SNIKKET_WEB_BACKUP_ENDPOINT", "http://snikket_backup:9292"), "/"),
		BackupToken:        envOr("SNIKKET_WEB_BACKUP_TOKEN", envOr("SNIKKET_BACKUP_TOKEN", "snikket-backup-local")),
		Version:            version,
		BuildCommit:        commit,
		BuildDate:          buildDate,
		StateDir:           stateDir,
		LogLevel:           logLevel,
		InsecureCookies:    insecureCookies,
		OIDC:               oidcCfg,
		ServiceAddress:     serviceAddress,
		ServicePassword:    servicePassword,
		LinkPreviewEnabled: linkPreview,

		AndroidHostEnabled:       androidEnabled,
		AndroidAPKSource:         androidSource,
		AndroidPackageID:         androidPackage,
		PlayStoreURL:             playURL,
		FDroidURL:                fdroidURL,
		AndroidDownloadLimitHour: androidLimit,
		AndroidRefreshHours:      androidRefresh,
		AndroidCertSHA256:        androidCertSHA256,
	}, nil
}

// OIDCConfig is the external identity provider configuration.
type OIDCConfig struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	UsernameClaim string
	Scopes        []string
}

// loadOIDC reads the single sign-on environment. It returns nil when the
// feature is off.
func loadOIDC(domain string) (*OIDCConfig, error) {
	issuer := strings.TrimRight(strings.TrimSpace(os.Getenv("SNIKKET_WEB_OIDC_ISSUER")), "/")
	if issuer == "" {
		return nil, nil
	}
	if err := validateHTTPURL("SNIKKET_WEB_OIDC_ISSUER", issuer); err != nil {
		return nil, err
	}

	clientID := strings.TrimSpace(os.Getenv("SNIKKET_WEB_OIDC_CLIENT_ID"))
	if clientID == "" {
		return nil, fmt.Errorf("SNIKKET_WEB_OIDC_CLIENT_ID is required when SNIKKET_WEB_OIDC_ISSUER is set")
	}
	clientSecret := os.Getenv("SNIKKET_WEB_OIDC_CLIENT_SECRET")
	if clientSecret == "" {
		return nil, fmt.Errorf("SNIKKET_WEB_OIDC_CLIENT_SECRET is required when SNIKKET_WEB_OIDC_ISSUER is set")
	}

	redirect := strings.TrimSpace(os.Getenv("SNIKKET_WEB_OIDC_REDIRECT_URL"))
	if redirect == "" {
		redirect = "https://" + domain + "/auth/oidc/callback"
	}
	if err := validateHTTPURL("SNIKKET_WEB_OIDC_REDIRECT_URL", redirect); err != nil {
		return nil, err
	}

	claim := envOr("SNIKKET_WEB_OIDC_USERNAME_CLAIM", "preferred_username")
	scopes := strings.Fields(envOr("SNIKKET_WEB_OIDC_SCOPES", "openid profile email"))
	if len(scopes) == 0 {
		scopes = []string{"openid"}
	}
	foundOpenID := false
	for _, scope := range scopes {
		if scope == "openid" {
			foundOpenID = true
		}
	}
	if !foundOpenID {
		scopes = append([]string{"openid"}, scopes...)
	}

	return &OIDCConfig{
		Issuer:        issuer,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		RedirectURL:   redirect,
		UsernameClaim: claim,
		Scopes:        scopes,
	}, nil
}

// normalizeHexDigest lowercases a hex digest and strips separators so a
// pasted fingerprint like "ab:cd:..." validates the same as "abcd...".
func normalizeHexDigest(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	v = strings.ReplaceAll(v, ":", "")
	v = strings.ReplaceAll(v, " ", "")
	for _, r := range v {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return v
}

func validateHTTPURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s: scheme must be http or https", name)
	}
	if u.Host == "" {
		return fmt.Errorf("%s: host is required", name)
	}
	return nil
}

func bridgeSnikketEnv() {
	setIfEmpty := func(dst, src string) {
		if os.Getenv(dst) == "" {
			if v := os.Getenv(src); v != "" {
				_ = os.Setenv(dst, v)
			}
		}
	}
	setIfEmpty("SNIKKET_WEB_DOMAIN", "SNIKKET_DOMAIN")
	setIfEmpty("SNIKKET_WEB_SITE_NAME", "SNIKKET_SITE_NAME")
	setIfEmpty("SNIKKET_WEB_TOS_URI", "SNIKKET_TOS_URI")
	setIfEmpty("SNIKKET_WEB_PRIVACY_URI", "SNIKKET_PRIVACY_URI")
	setIfEmpty("SNIKKET_WEB_ABUSE_EMAIL", "SNIKKET_ABUSE_EMAIL")
	setIfEmpty("SNIKKET_WEB_SECURITY_EMAIL", "SNIKKET_SECURITY_EMAIL")
}

func loadOrCreateSecret(stateDir string) ([]byte, error) {
	if v := os.Getenv("SNIKKET_WEB_SECRET_KEY"); v != "" {
		return []byte(v), nil
	}
	path := envOr("SNIKKET_WEB_SECRET_KEY_FILE", filepath.Join(stateDir, secretKeyFileName))
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("SNIKKET_WEB_SECRET_KEY_FILE must be absolute")
	}
	// #nosec G304 -- path is operator-configured absolute path for the secret file
	data, err := os.ReadFile(path)
	if err == nil {
		data = []byte(strings.TrimSpace(string(data)))
		if len(data) > 0 {
			return data, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	secret := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("create secret dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write secret key: %w (set SNIKKET_WEB_SECRET_KEY)", err)
	}
	return []byte(secret), nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
