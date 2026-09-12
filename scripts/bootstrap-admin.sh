#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

PASSWORD="admin"
LOCALPART="admin"

snikketx_parse_mode "$@"
set -- "${COMPOSE_EXTRA_ARGS[@]}"

while [[ $# -gt 0 ]]; do
	case "$1" in
	--password)
		PASSWORD="$2"
		shift 2
		;;
	--user)
		LOCALPART="$2"
		shift 2
		;;
	-h | --help)
		echo "Usage: ./scripts/bootstrap-admin.sh [--dev] [--user localpart] [--password SECRET]"
		exit 0
		;;
	*)
		echo "Unknown argument: $1" >&2
		exit 1
		;;
	esac
done

snikketx_require_conf
domain="$(snikketx_domain)"
if [[ -z "$domain" ]]; then
	echo "SNIKKET_DOMAIN is not set." >&2
	exit 1
fi

if ! docker container inspect "$SNIKKETX_CONTAINER_SERVER" >/dev/null 2>&1; then
	echo "Container '${SNIKKETX_CONTAINER_SERVER}' is not running. Start the stack first." >&2
	exit 1
fi

jid="${LOCALPART}@${domain}"
echo "Ensuring admin account ${jid} ..."

if docker exec "$SNIKKETX_CONTAINER_SERVER" prosodyctl shell user create "$jid" "$PASSWORD" "prosody:admin" 2>/dev/null; then
	echo "Created ${jid}"
else
	docker exec "$SNIKKETX_CONTAINER_SERVER" prosodyctl shell user password "$jid" "$PASSWORD" >/dev/null
	docker exec "$SNIKKETX_CONTAINER_SERVER" prosodyctl shell user set_role "$jid" "$domain" "prosody:admin" >/dev/null || true
	echo "Updated password and admin role for ${jid}"
fi

base="$(snikketx_http_base)"
echo "Login: ${base}/login"
if [[ "$COMPOSE_MODE" == "dev" ]]; then
	echo "Credentials: ${jid} / ${PASSWORD}"
fi
