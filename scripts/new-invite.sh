#!/bin/bash

set -euo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

if [ ! -f snikket.conf ]; then
	echo "snikket.conf is missing. Run ./scripts/init.sh first."
	exit 1
fi

SNIKKET_DOMAIN=$(snikketx_domain)
if [[ -z "$SNIKKET_DOMAIN" ]]; then
	echo "Failed to read SNIKKET_DOMAIN from snikket.conf, unable to continue"
	exit 1
fi

if ! docker container inspect "$SNIKKETX_CONTAINER_SERVER" >/dev/null 2>&1; then
	echo "Container '${SNIKKETX_CONTAINER_SERVER}' is not running. Start the stack first."
	exit 1
fi

rewrite_local_url() {
	local url="$1"
	if [[ "$url" == https://${SNIKKET_DOMAIN}/* ]] || [[ "$url" == https://${SNIKKET_DOMAIN}/ ]]; then
		printf 'http://%s:8080/%s' "$SNIKKET_DOMAIN" "${url#https://${SNIKKET_DOMAIN}/}"
		return
	fi
	printf '%s' "$url"
}

# In-image helper already uses `prosodyctl shell invite`.
if docker exec "$SNIKKETX_CONTAINER_SERVER" test -x /usr/local/bin/create-invite; then
	output=$(docker exec -i -e "SNIKKET_DOMAIN=$SNIKKET_DOMAIN" "$SNIKKETX_CONTAINER_SERVER" create-invite "$@" || true)
	if [[ -z "$output" ]]; then
		echo "Failed to create invite"
		exit 1
	fi
	while IFS= read -r line; do
		case "$line" in
		"Your invite link: "*)
			url=${line#Your invite link: }
			echo "Your invite link: $(rewrite_local_url "$url")"
			;;
		*)
			printf '%s\n' "$line"
			;;
		esac
	done <<<"$output"
	exit 0
fi

SHOW_QR=0
if [ "${1-}" = "--qr" ]; then
	SHOW_QR=1
	shift
fi

if [ "${1-}" = "--reset" ]; then
	shift
	jid="${1:?usage: $0 --reset <localpart-or-jid>}"
	if [[ "$jid" != *@* ]]; then
		jid="${jid}@${SNIKKET_DOMAIN}"
	fi
	URL=$(docker exec -i "$SNIKKETX_CONTAINER_SERVER" prosodyctl shell invite create_reset "$jid" | sed -n 's/^OK: //;T;p')
else
	URL=$(docker exec -i "$SNIKKETX_CONTAINER_SERVER" prosodyctl shell invite create_account "$@" "$SNIKKET_DOMAIN" | sed -n 's/^OK: //;T;p')
fi

if [[ -z "$URL" ]]; then
	echo "Failed to create invite"
	exit 1
fi

URL=$(rewrite_local_url "$URL")

echo ""
echo "Your invite link: $URL"
echo ""
if [ "$SHOW_QR" = "1" ]; then
	if command -v qrencode >/dev/null 2>&1; then
		echo "QR code for scanning:"
		echo ""
		echo "$URL" | qrencode -t ansi
		echo ""
	else
		echo "qrencode is not installed on the host. Install it to print a QR code."
	fi
fi
