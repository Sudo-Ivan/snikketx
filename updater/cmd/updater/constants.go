package main

import "time"

// Environment variable names.
const (
	envListenAddr        = "SNIKKET_UPDATER_LISTEN"
	envToken             = "SNIKKET_UPDATER_TOKEN" // #nosec G101 -- env var name, not a credential
	envComposeDir        = "SNIKKET_UPDATER_COMPOSE_DIR"
	envStateDir          = "SNIKKET_UPDATER_STATE_DIR"
	envImagePrefix       = "SNIKKET_UPDATER_IMAGE_PREFIX"
	envVerifySignatures  = "SNIKKET_UPDATER_VERIFY_SIGNATURES"
	envRequireSignatures = "SNIKKET_UPDATER_REQUIRE_SIGNATURES"
	envCosignIdentity    = "SNIKKET_UPDATER_COSIGN_IDENTITY_REGEXP"
	envCosignOIDCIssuer  = "SNIKKET_UPDATER_COSIGN_OIDC_ISSUER"
	envCosignBin         = "SNIKKET_UPDATER_COSIGN_BIN"
)

// Defaults for the environment above.
const (
	defaultListenAddr       = "0.0.0.0:9191"
	defaultComposeDir       = "/work"
	defaultStateDir         = "/var/lib/snikket-updater"
	defaultImagePrefix      = "ghcr.io/sudo-ivan/snikketx"
	defaultCosignIdentity   = "https://github.com/Sudo-Ivan/snikketx/.*"
	defaultCosignOIDCIssuer = "https://token.actions.githubusercontent.com"
	defaultCosignBin        = "cosign"

	// defaultToken is a well-known development fallback only. Set a random
	// SNIKKET_UPDATER_TOKEN in .env for any reachable deployment.
	defaultToken = "snikket-updater-local" // #nosec G101 -- documented local fallback, not a shipped credential

	defaultVerifySignatures = true
	// Production requires Cosign signatures. Set
	// SNIKKET_UPDATER_REQUIRE_SIGNATURES=false only to opt out in development.
	defaultRequireSignatures = true
)

// State directory contents and permissions.
const (
	settingsFileName = "settings.json"
	pinsFileName     = "pins.json"
	overrideFileName = "docker-compose.pins.yml"

	stateDirMode     = 0o755
	stateFileMode    = 0o600
	overrideFileMode = 0o644
)

// HTTP server limits.
const (
	httpReadHeaderTimeout = 10 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpWriteTimeout      = 2 * time.Minute
	httpIdleTimeout       = 90 * time.Second
	httpMaxHeaderBytes    = 16 << 10
)

// Job runner limits.
const (
	jobTimeout     = 30 * time.Minute
	jobLogMaxLines = 200
)

// Update check interval bounds in hours.
const (
	defaultIntervalHours = 24
	minIntervalHours     = 1
	maxIntervalHours     = 168
)

// Log fetching limits.
const (
	logFetchTimeout = 20 * time.Second
	logDefaultTail  = 200
	logMaxTail      = 500
	logMaxBytes     = 512 << 10
)

// allowedLogServices lists the compose services exposed through the logs API
// and the GET /services registry.
var allowedLogServices = []logService{
	{ID: "snikket_server", Label: "Chat server (Prosody)"},
	{ID: "snikket_portal", Label: "Web portal"},
	{ID: "ravenguard", Label: "Edge (RavenGuard)"},
	{ID: "snikket_updater", Label: "Updater"},
	{ID: "snikket_backup", Label: "Backup"},
	{ID: "snikket_certs", Label: "Cert manager"},
}
