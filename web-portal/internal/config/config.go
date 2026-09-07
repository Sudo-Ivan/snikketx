package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSecretKeyFile = "/var/lib/snikket-web-portal/secret_key"
	defaultAvatarTTL     = 1800
	defaultMaxAvatar     = 1024 * 1024
	defaultAppleStore    = "https://apps.apple.com/us/app/snikket/id1544535398"
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
	Version         string
}

func Load(version string) (*Config, error) {
	bridgeSnikketEnv()

	domain := os.Getenv("SNIKKET_WEB_DOMAIN")
	if domain == "" {
		return nil, fmt.Errorf("SNIKKET_WEB_DOMAIN is required")
	}
	endpoint := strings.TrimRight(os.Getenv("SNIKKET_WEB_PROSODY_ENDPOINT"), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("SNIKKET_WEB_PROSODY_ENDPOINT is required")
	}

	secret, err := loadOrCreateSecret()
	if err != nil {
		return nil, err
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

	apple := os.Getenv("SNIKKET_WEB_APPLE_STORE_URL")
	if apple == "" {
		apple = defaultAppleStore
	}

	iface := envOr("SNIKKET_TWEAK_PORTAL_INTERNAL_HTTP_INTERFACE", "0.0.0.0")
	port := envOr("SNIKKET_TWEAK_PORTAL_INTERNAL_HTTP_PORT", "5765")

	return &Config{
		SecretKey:       secret,
		ProsodyEndpoint: endpoint,
		Domain:          domain,
		SiteName:        siteName,
		AvatarCacheTTL:  time.Duration(avatarTTL) * time.Second,
		AppleStoreURL:   apple,
		MaxAvatarSize:   maxAvatar,
		ShowMetrics:     showMetrics,
		TOSURI:          os.Getenv("SNIKKET_WEB_TOS_URI"),
		PrivacyURI:      os.Getenv("SNIKKET_WEB_PRIVACY_URI"),
		AbuseEmail:      os.Getenv("SNIKKET_WEB_ABUSE_EMAIL"),
		SecurityEmail:   os.Getenv("SNIKKET_WEB_SECURITY_EMAIL"),
		ListenAddr:      iface + ":" + port,
		MetricsToken:    os.Getenv("SNIKKET_WEB_METRICS_TOKEN"),
		Version:         version,
	}, nil
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

func loadOrCreateSecret() ([]byte, error) {
	if v := os.Getenv("SNIKKET_WEB_SECRET_KEY"); v != "" {
		return []byte(v), nil
	}
	path := envOr("SNIKKET_WEB_SECRET_KEY_FILE", defaultSecretKeyFile)
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
	if err := os.MkdirAll(dirOf(path), 0o750); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("create secret dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write secret key: %w (set SNIKKET_WEB_SECRET_KEY)", err)
	}
	return []byte(secret), nil
}

func dirOf(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "."
	}
	return path[:i]
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
