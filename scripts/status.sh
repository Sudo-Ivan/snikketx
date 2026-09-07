#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root
snikketx_parse_mode "$@"
snikketx_require_conf
snikketx_ensure_env

echo "== compose ($COMPOSE_MODE) =="
snikketx_compose ps

domain="$(snikketx_domain)"
base="$(snikketx_http_base)"
echo ""
echo "== HTTP probe =="
code=$(curl -sS -o /dev/null -w '%{http_code}' -H "Host: ${domain}" --connect-timeout 3 "${base}/login" 2>/dev/null || echo "000")
echo "GET ${base}/login -> ${code}"
if [[ "$code" != "200" && "$code" != "302" && "$code" != "303" ]]; then
	echo "Portal login page not healthy yet." >&2
	exit 1
fi
echo "OK"
