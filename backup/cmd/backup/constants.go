package main

import "time"

// Environment variable names.
const (
	envListenAddr     = "SNIKKET_BACKUP_LISTEN"
	envToken          = "SNIKKET_BACKUP_TOKEN"
	envStateDir       = "SNIKKET_BACKUP_STATE_DIR"
	envArchiveDir     = "SNIKKET_BACKUP_ARCHIVE_DIR"
	envComposeDir     = "SNIKKET_BACKUP_COMPOSE_DIR"
	envAlpineImage    = "SNIKKET_BACKUP_ALPINE_IMAGE"
	envResticBin      = "SNIKKET_BACKUP_RESTIC_BIN"
	envResticRepo     = "RESTIC_REPOSITORY"
	envResticPass     = "RESTIC_PASSWORD"
	envResticPassFile = "RESTIC_PASSWORD_FILE"
)

// Defaults for the environment above.
const (
	defaultListenAddr  = "0.0.0.0:9292"
	defaultStateDir    = "/var/lib/snikket-backup"
	defaultComposeDir  = "/work"
	defaultAlpineImage = "alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"
	defaultResticBin   = "restic"

	// defaultToken is a well-known development fallback only. Set a random
	// SNIKKET_BACKUP_TOKEN in .env for any reachable deployment.
	defaultToken = "snikket-backup-local"
)

// Default settings applied before settings.json is loaded.
const (
	defaultIntervalHours   = 24
	defaultScope           = scopeFull
	defaultKeepCount       = 7
	defaultKeepDays        = 30
	defaultResticKeepLast  = 7
	defaultResticKeepDaily = 14
)

// Settings bounds enforced by normalizeSettings.
const (
	scopeFull          = "full"
	scopeDataOnly      = "data-only"
	minIntervalHours   = 1
	maxIntervalHours   = 168
	minKeepCount       = 1
	maxKeepCount       = 365
	minKeepDays        = 1
	maxKeepDays        = 3650
	minResticKeepLast  = 1
	minResticKeepDaily = 0
)

// State directory layout and permissions.
const (
	archivesDirName  = "archives"
	stateFileName    = "state.json"
	settingsFileName = "settings.json"
	dirMode          = 0o750
	fileMode         = 0o600
)

// HTTP server limits.
const (
	httpReadHeaderTimeout = 10 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpWriteTimeout      = 10 * time.Minute
	httpIdleTimeout       = 90 * time.Second
	httpMaxHeaderBytes    = 16 << 10
)

// Job runner limits.
const (
	jobTimeout     = 2 * time.Hour
	jobLogMaxLines = 200
)

// recentArchivesMax caps how many archives snapshot reports.
const recentArchivesMax = 5

// Archive directory naming.
const (
	archivePrefix       = "snikketx-backup-"
	archiveStampFormat  = "2006-01-02-150405"
	dataArchivePrefix   = "snikket-data-"
	manifestFileName    = "MANIFEST.txt"
	snikketConfFileName = "snikket.conf"
	envFileName         = ".env"
	envArchiveFileName  = "env"
)

// Backup mode labels written into the manifest.
const (
	modeClassic  = "classic"
	modeSnikketX = "snikketx"
)

// Docker resources touched by backups.
const (
	snikketContainer  = "snikket"
	snikketDataPath   = "/snikket"
	helperBackupMount = "/backup"
	helperDataMount   = "/data"
)

// Docker volumes archived in full scope.
const (
	volPortalData     = "snikketx_portal_data"
	volRavenguardData = "snikketx_ravenguard_data"
	volUpdaterData    = "snikketx_updater_data"
	volBackupData     = "snikketx_backup_data"
	volPortalClassic  = "snikket_portal_data"
	volACMEChallenges = "snikketx_acme_challenges"
)

// File name prefixes for per-volume archives inside a backup directory.
const (
	labelPortalData     = "portal-data"
	labelRavenguardData = "ravenguard-data"
	labelUpdaterData    = "updater-data"
	labelBackupData     = "backup-data"
	labelPortalClassic  = "portal-data-classic"
	labelACMEChallenges = "acme-challenges"
)
