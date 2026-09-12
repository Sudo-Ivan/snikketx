#!/bin/bash
# Shared helpers for SnikketX compose wrappers.
# shellcheck shell=bash

if [[ -n "${SNIKKETX_COMPOSE_LIB:-}" ]]; then
	return 0 2>/dev/null || true
fi
SNIKKETX_COMPOSE_LIB=1

# Shared stack constants. Used by the scripts that source this file.
export SNIKKETX_CONTAINER_SERVER="snikket"
export SNIKKETX_CONTAINER_CERTS="snikket-certs"
export SNIKKETX_CONTAINER_PORTAL="snikket-portal"
export SNIKKETX_CONTAINER_UPDATER="snikket-updater"
export SNIKKETX_CONTAINER_BACKUP="snikket-backup"
export SNIKKETX_CONTAINER_RAVENGUARD="snikketx-ravenguard"

export SNIKKETX_VOL_SNIKKET_DATA="snikketx_snikket_data"
export SNIKKETX_VOL_PORTAL_DATA="snikketx_portal_data"
export SNIKKETX_VOL_RAVENGUARD_DATA="snikketx_ravenguard_data"
export SNIKKETX_VOL_UPDATER_DATA="snikketx_updater_data"
export SNIKKETX_VOL_BACKUP_DATA="snikketx_backup_data"
export SNIKKETX_VOL_PORTAL_DATA_CLASSIC="snikket_portal_data"

export SNIKKETX_PORTAL_ENDPOINT="http://snikket_portal:5765"
export SNIKKETX_UPDATER_ENDPOINT="http://snikket_updater:9191"
export SNIKKETX_BACKUP_ENDPOINT="http://snikket_backup:9292"
export SNIKKETX_BACKUP_ENDPOINT_LOCAL="http://127.0.0.1:9292"

# Fallback tokens used when .env does not set one. init.sh writes random values.
export SNIKKETX_UPDATER_TOKEN_DEFAULT="snikket-updater-local"
export SNIKKETX_BACKUP_TOKEN_DEFAULT="snikket-backup-local"

# Helper image for volume copy/tar jobs (must match compose bootstrap digest).
export SNIKKETX_HELPER_IMAGE="alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"

snikketx_root() {
	local here
	here="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	printf '%s' "$here"
}

snikketx_cd_root() {
	cd "$(snikketx_root)" || return
}

# Sets COMPOSE_MODE (prod|dev) and COMPOSE_ARGS from args or COMPOSE_MODE env.
# Consumes leading --dev|dev| --prod|prod from "$@"; remaining args stay in COMPOSE_EXTRA_ARGS.
snikketx_parse_mode() {
	COMPOSE_MODE="${COMPOSE_MODE:-prod}"
	COMPOSE_EXTRA_ARGS=()
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--dev | dev)
			COMPOSE_MODE=dev
			shift
			;;
		--prod | prod)
			COMPOSE_MODE=prod
			shift
			;;
		*)
			COMPOSE_EXTRA_ARGS+=("$1")
			shift
			;;
		esac
	done
	COMPOSE_ARGS=(-f docker-compose.yml)
	if [[ "$COMPOSE_MODE" == "dev" ]]; then
		COMPOSE_ARGS+=(-f docker-compose.dev.yml)
	fi
	if [[ -f deploy/migrate/docker-compose.migrate.yml ]]; then
		COMPOSE_ARGS+=(-f deploy/migrate/docker-compose.migrate.yml)
	fi
}

snikketx_conf_get() {
	local key="$1"
	local file="${2:-snikket.conf}"
	if [[ ! -f "$file" ]]; then
		return 1
	fi
	grep -E "^${key}=" "$file" | head -n1 | cut -d= -f2- | tr -d '"' | tr -d "'"
}

snikketx_random() {
	local n="${1:-32}"
	head -c "$n" /dev/urandom | base64 | tr -d '\n=/+' | head -c "$n"
}

# Ensure .env exists for compose interpolation.
snikketx_ensure_env() {
	local domain email secret admin_pw
	if [[ ! -f .env ]]; then
		domain="$(snikketx_conf_get SNIKKET_DOMAIN || true)"
		email="$(snikketx_conf_get SNIKKET_ADMIN_EMAIL || true)"
		secret="$(snikketx_random 32)"
		admin_pw="$(snikketx_random 24)"
		cat >.env <<EOF
SNIKKET_DOMAIN=${domain}
SNIKKET_ADMIN_EMAIL=${email}
RG_CHALLENGE_SECRET=${secret}
RG_ADMIN_BOOTSTRAP_PASSWORD=${admin_pw}
SNIKKET_UPDATER_TOKEN=$(snikketx_random 32)
SNIKKET_BACKUP_TOKEN=$(snikketx_random 32)
EOF
		echo "Wrote .env from snikket.conf"
	fi
	if ! grep -q '^RG_ADMIN_BOOTSTRAP_PASSWORD=' .env 2>/dev/null; then
		admin_pw="$(snikketx_random 24)"
		printf 'RG_ADMIN_BOOTSTRAP_PASSWORD=%s\n' "$admin_pw" >>.env
		echo "Appended RG_ADMIN_BOOTSTRAP_PASSWORD to .env"
	fi
	if ! grep -q '^SNIKKET_UPDATER_TOKEN=' .env 2>/dev/null; then
		printf 'SNIKKET_UPDATER_TOKEN=%s\n' "$(snikketx_random 32)" >>.env
	fi
	if ! grep -q '^SNIKKET_BACKUP_TOKEN=' .env 2>/dev/null; then
		printf 'SNIKKET_BACKUP_TOKEN=%s\n' "$(snikketx_random 32)" >>.env
	fi
}

snikketx_export_build_meta() {
	export VCS_REF="${VCS_REF:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
	export BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
}

snikketx_compose() {
	docker compose "${COMPOSE_ARGS[@]}" "$@"
}

snikketx_require_conf() {
	if [[ ! -f docker-compose.yml ]]; then
		echo "docker-compose.yml is missing from this checkout." >&2
		exit 1
	fi
	if [[ ! -f snikket.conf ]]; then
		echo "Almost there! Run ./scripts/init.sh or ./scripts/init.sh --dev first." >&2
		exit 1
	fi
}

snikketx_domain() {
	snikketx_conf_get SNIKKET_DOMAIN || snikketx_conf_get SNIKKET_DOMAIN .env || true
}

snikketx_http_base() {
	local domain
	domain="$(snikketx_domain)"
	if [[ "${COMPOSE_MODE:-prod}" == "dev" ]]; then
		printf 'http://%s:8080' "$domain"
	else
		printf 'https://%s' "$domain"
	fi
}
