#!/bin/bash
# Post-deploy verification for SnikketX.
#
#   1. Required containers exist and are running (none stuck restarting)
#   2. HTTP edge answers the portal login page (retries while ACME issues)
#   3. External DNS/TLS/DANE/port verification (prod only, via
#      scripts/verify-domain.py; skip with --skip-external)
#
# Usage: ./scripts/postcheck.sh [--dev|--prod] [--skip-external]

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root
snikketx_parse_mode "$@"

SKIP_EXTERNAL=0
for arg in "${COMPOSE_EXTRA_ARGS[@]}"; do
	case "$arg" in
	--skip-external) SKIP_EXTERNAL=1 ;;
	esac
done

PASS=0
FAIL=0
ok()   { echo "PASS  $*"; PASS=$((PASS + 1)); }
fail() { echo "FAIL  $*"; FAIL=$((FAIL + 1)); }
warn() { echo "WARN  $*"; }

echo "== SnikketX postcheck (${COMPOSE_MODE}) =="

required=(snikket snikket-portal snikketx-ravenguard)
if [[ "$COMPOSE_MODE" == "prod" ]]; then
	required+=(snikket-certs snikket-updater snikket-backup)
fi

for c in "${required[@]}"; do
	if ! docker container inspect "$c" >/dev/null 2>&1; then
		fail "container ${c} missing"
		continue
	fi
	st=$(docker inspect -f '{{.State.Status}}' "$c" 2>/dev/null || echo unknown)
	restarts=$(docker inspect -f '{{.RestartCount}}' "$c" 2>/dev/null || echo 0)
	if [[ "$st" == "running" ]]; then
		ok "container ${c} running"
	else
		fail "container ${c} state ${st}"
	fi
	if [[ "$restarts" =~ ^[0-9]+$ ]] && [[ "$restarts" -gt 5 ]]; then
		warn "container ${c} has ${restarts} restarts (crash loop?)"
	fi
done

# Catch any other project container stuck restarting.
while read -r name status; do
	[[ -z "$name" ]] && continue
	case "$status" in
	*Restarting*) fail "container ${name} is restarting" ;;
	esac
done < <(docker ps -a --format '{{.Names}} {{.Status}}' \
	| grep -E '^snikket' || true)

domain="$(snikketx_domain)"
base="$(snikketx_http_base)"

echo ""
echo "== HTTP edge =="
code="000"
for _ in $(seq 1 30); do
	code=$(curl -sS -o /dev/null -w '%{http_code}' -H "Host: ${domain}" \
		--connect-timeout 3 "${base}/login" 2>/dev/null || echo "000")
	[[ "$code" =~ ^(200|302|303)$ ]] && break
	sleep 3
done
if [[ "$code" =~ ^(200|302|303)$ ]]; then
	ok "GET ${base}/login -> ${code}"
else
	fail "GET ${base}/login -> ${code} (edge not ready; first ACME issuance can take a minute)"
fi

echo ""
echo "== External verification =="
case "$domain" in
*.localhost | localhost | "") SKIP_EXTERNAL=1 ;;
esac
if [[ "$SKIP_EXTERNAL" -eq 1 ]]; then
	echo "skipped (local domain or --skip-external)"
elif command -v python3 >/dev/null 2>&1; then
	if ! python3 ./scripts/verify-domain.py --domain "$domain"; then
		FAIL=$((FAIL + 1))
	fi
else
	warn "python3 missing, skipping verify-domain.py"
fi

echo ""
echo "Summary: ${PASS} pass, ${FAIL} fail"
[[ "$FAIL" -eq 0 ]]
