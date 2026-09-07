#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

COMPOSE_MODE="${COMPOSE_MODE:-prod}"
PASSWORD="admin"
LOCALPART="admin"

while [[ $# -gt 0 ]]; do
	case "$1" in
	--dev|dev)
		COMPOSE_MODE=dev
		shift
		;;
	--prod|prod)
		COMPOSE_MODE=prod
		shift
		;;
	--password)
		PASSWORD="$2"
		shift 2
		;;
	--user)
		LOCALPART="$2"
		shift 2
		;;
	-h|--help)
		echo "Usage: ./scripts/bootstrap-admin.sh [--dev] [--user localpart] [--password SECRET]"
		exit 0
		;;
	*)
		echo "Unknown argument: $1" >&2
		exit 1
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

snikketx_require_conf
domain="$(snikketx_domain)"
if [[ -z "$domain" ]]; then
	echo "SNIKKET_DOMAIN is not set." >&2
	exit 1
fi

if ! docker container inspect snikket >/dev/null 2>&1; then
	echo "Container 'snikket' is not running. Start the stack first." >&2
	exit 1
fi

jid="${LOCALPART}@${domain}"
echo "Ensuring admin account ${jid} ..."

if docker exec snikket prosodyctl shell user create "$jid" "$PASSWORD" "prosody:admin" 2>/dev/null; then
	echo "Created ${jid}"
else
	docker exec snikket prosodyctl shell user password "$jid" "$PASSWORD" >/dev/null
	docker exec snikket prosodyctl shell user set_role "$jid" "$domain" "prosody:admin" >/dev/null || true
	echo "Updated password and admin role for ${jid}"
fi

base="$(snikketx_http_base)"
echo "Login: ${base}/login"
if [[ "$COMPOSE_MODE" == "dev" ]]; then
	echo "Credentials: ${jid} / ${PASSWORD}"
fi
