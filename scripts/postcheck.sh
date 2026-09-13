#!/bin/bash
# Post-deploy verification for SnikketX.
#
#   1. Required containers exist and are running (none stuck restarting)
#   2. Traefik edge config and ping
#   3. Portal and Prosody health, with diagnostics on failure
#   4. Host firewall checks: ufw and fail2ban when installed
#   5. External DNS/TLS/DANE/port verification (prod only, via
#      scripts/verify-domain.py. Skip with --skip-external)
#
# Usage: ./scripts/postcheck.sh [--dev|--prod] [--skip-external]
#
# All output is also written to post-check.log for easy copy and paste.

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root
snikketx_parse_mode "$@"

exec > >(tee post-check.log) 2>&1

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
hint() { echo "  hint: $*"; }

echo "== SnikketX postcheck (${COMPOSE_MODE}) =="
echo "time: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "log:  $(pwd)/post-check.log"

echo ""
echo "== Containers =="

required=(snikket snikket-portal snikketx-traefik)
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
		echo "  --- last log lines for ${c} ---"
		docker logs "$c" --tail 15 2>&1 | sed 's/^/  /' || true
		echo "  --- end ${c} logs ---"
	fi
	if [[ "$restarts" =~ ^[0-9]+$ ]] && [[ "$restarts" -gt 5 ]]; then
		warn "container ${c} has ${restarts} restarts (possible crash loop)"
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
echo "== Traefik edge =="

if docker container inspect snikketx-traefik >/dev/null 2>&1; then
	if grep -q "Host(\`${domain}\`)" deploy/traefik/dynamic.yml 2>/dev/null; then
		ok "dynamic routes rendered for ${domain}"
	else
		fail "deploy/traefik/dynamic.yml does not match ${domain}"
		hint "re-run: ./scripts/render-edge.sh ${COMPOSE_MODE}"
	fi
	if docker exec snikketx-traefik traefik healthcheck --ping >/dev/null 2>&1; then
		ok "traefik ping endpoint healthy"
	else
		fail "traefik ping endpoint not answering"
		hint "check: docker logs snikketx-traefik"
	fi
else
	fail "traefik container missing, skipping edge checks"
fi

echo ""
echo "== HTTP edge =="
code="000"
start_ts=$(date +%s)
for i in $(seq 1 30); do
	code=$(curl -sS -o /dev/null -w '%{http_code}' -H "Host: ${domain}" \
		--connect-timeout 3 "${base}/login" 2>/dev/null || echo "000")
	[[ "$code" =~ ^(200|302|303)$ ]] && break
	elapsed=$(( $(date +%s) - start_ts ))
	echo "  waiting for edge... attempt ${i}/30, ${elapsed}s elapsed, last=${code}"
	sleep 3
done
if [[ "$code" =~ ^(200|302|303)$ ]]; then
	ok "GET ${base}/login -> ${code}"
elif [[ "$code" == "502" || "$code" == "503" ]]; then
	fail "GET ${base}/login -> ${code} (edge up but portal upstream not answering)"
	hint "check: docker logs snikket-portal"
	echo "  --- last log lines for snikket-portal ---"
	docker logs snikket-portal --tail 15 2>&1 | sed 's/^/  /' || true
	echo "  --- end snikket-portal logs ---"
else
	fail "GET ${base}/login -> ${code} (edge not ready)"
	hint "first ACME issuance can take a minute. check: docker logs snikketx-traefik"
	echo "  --- last log lines for snikketx-traefik ---"
	docker logs snikketx-traefik --tail 15 2>&1 | sed 's/^/  /' || true
	echo "  --- end snikketx-traefik logs ---"
fi

echo ""
echo "== Portal and Prosody =="

# Portal has a Docker healthcheck when the image defines one.
portal_health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' \
	snikket-portal 2>/dev/null || echo "")
if [[ "$portal_health" == "healthy" ]]; then
	ok "portal healthcheck reports healthy"
elif [[ -n "$portal_health" ]]; then
	warn "portal healthcheck reports ${portal_health}"
else
	warn "portal has no Docker healthcheck, relying on edge probe"
fi

# Prosody diagnostics: running state, certs, recent errors.
if docker exec snikket prosodyctl status >/dev/null 2>&1; then
	ok "prosodyctl status: running"
else
	fail "prosodyctl status failed in container snikket"
	hint "check: docker logs snikket"
fi

if docker exec snikket test -f "/snikket/letsencrypt/live/${domain}/fullchain.pem" 2>/dev/null; then
	ok "XMPP cert exists for ${domain} (/snikket/letsencrypt/live)"
else
	warn "no letsencrypt cert for ${domain} inside container snikket"
	hint "cert-manager issues these. check: docker logs snikket-certs"
fi

prosody_errors=$(docker logs snikket --since 15m 2>&1 \
	| grep -icE 'error|crit' || true)
if [[ "$prosody_errors" -gt 0 ]]; then
	warn "prosody logged ${prosody_errors} error lines in the last 15m"
	docker logs snikket --since 15m 2>&1 \
		| grep -iE 'error|crit' | tail -10 | sed 's/^/  /'
else
	ok "no prosody errors in the last 15m"
fi

if docker logs snikket --since 60m 2>&1 | grep -q "turndb"; then
	warn "prosody TURN database error seen in the last hour (turndb)"
	hint "check /snikket/turnserver/turndb permissions inside container snikket"
fi

echo ""
echo "== Calls, STUN and TURN =="

# These are configuration checks. A real media test still needs two
# XMPP clients to place a call, ideally one behind a restrictive NAT.
if docker exec snikket pidof turnserver >/dev/null 2>&1; then
	ok "coturn (turnserver) is running inside container snikket"
else
	fail "turnserver is not running inside container snikket"
	hint "check: docker logs snikket | grep -i turn"
fi

if docker exec snikket test -f /snikket/turnserver/turndb 2>/dev/null; then
	ok "TURN credential database exists (/snikket/turnserver/turndb)"
else
	warn "TURN credential database missing"
	hint "turnserver creates it on first credential. check: docker logs snikket | grep -i turn"
fi

turn_min="${SNIKKET_TURN_MIN_PORT:-49152}"
turn_max="${SNIKKET_TURN_MAX_PORT:-49251}"
# coturn gets the range as --min-port/--max-port command line args.
turn_args=$(docker exec snikket sh -c \
	'tr "\0" " " < /proc/$(pidof turnserver | tr " " "\n" | head -1)/cmdline' \
	2>/dev/null || true)
conf_min=$(printf '%s' "$turn_args" | sed -n 's/.*--min-port \([0-9]*\).*/\1/p')
conf_max=$(printf '%s' "$turn_args" | sed -n 's/.*--max-port \([0-9]*\).*/\1/p')
if [[ -n "$conf_min" && -n "$conf_max" ]]; then
	if [[ "$conf_min" == "$turn_min" && "$conf_max" == "$turn_max" ]]; then
		ok "coturn relay range ${conf_min}-${conf_max} matches published range"
	else
		fail "coturn relay range ${conf_min}-${conf_max} differs from published ${turn_min}-${turn_max}"
		hint "set SNIKKET_TURN_MIN_PORT/SNIKKET_TURN_MAX_PORT in .env or fix turnserver.conf"
	fi
else
	warn "could not read --min-port/--max-port from the turnserver process"
fi

# Port ranges may be published as a range or expanded per port by
# Compose, so the range endpoints are checked instead.
for spec in "3478/udp" "3478/tcp" "5349/tcp" "5349/udp" \
	"${turn_min}/udp" "${turn_max}/udp"; do
	if docker inspect snikket --format '{{json .HostConfig.PortBindings}}' 2>/dev/null \
		| grep -q "${spec}"; then
		ok "container publishes ${spec}"
	else
		warn "container does not publish ${spec}"
		hint "check the ports section of the server service in docker-compose.yml"
	fi
done

# UDP relay range must also be open on the host firewall for calls to
# get relay candidates when peers cannot connect directly.
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
	if ufw status 2>/dev/null | grep -qE "${turn_min}:${turn_max}/udp"; then
		ok "ufw allows TURN relay range ${turn_min}:${turn_max}/udp"
	else
		fail "ufw does not allow TURN relay range ${turn_min}:${turn_max}/udp"
		hint "allow it: ufw allow ${turn_min}:${turn_max}/udp"
	fi
fi

echo ""
echo "== Host firewall =="

required_ports_tcp="80 443 5222 5223 5269 3478 5349"
required_ports_udp="443 3478 5349"

if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
	echo "ufw is active, checking required ports"
	for p in $required_ports_tcp; do
		if ufw status 2>/dev/null | grep -qE "(^|[[:space:]])${p}(/tcp)?[[:space:]]"; then
			ok "ufw allows ${p}/tcp"
		else
			fail "ufw does not list ${p}/tcp as allowed"
			hint "allow it: ufw allow ${p}/tcp"
		fi
	done
	for p in $required_ports_udp; do
		if ufw status 2>/dev/null | grep -qE "(^|[[:space:]])${p}/udp[[:space:]]"; then
			ok "ufw allows ${p}/udp"
		else
			fail "ufw does not list ${p}/udp as allowed"
			hint "allow it: ufw allow ${p}/udp"
		fi
	done
elif command -v ufw >/dev/null 2>&1; then
	echo "ufw installed but inactive, skipping"
else
	echo "ufw not installed, skipping"
fi

if command -v fail2ban-client >/dev/null 2>&1 \
	&& fail2ban-client ping >/dev/null 2>&1; then
	echo "fail2ban is running, checking jails"
	jails=$(fail2ban-client status 2>/dev/null \
		| sed -n 's/.*Jail list:[[:space:]]*//p' | tr ',' ' ')
	if [[ -z "$jails" ]]; then
		ok "fail2ban running with no active jails"
	else
		for j in $jails; do
			banned=$(fail2ban-client status "$j" 2>/dev/null \
				| sed -n 's/.*Currently banned:[[:space:]]*//p' | tr -d ' ')
			if [[ "${banned:-0}" -gt 0 ]]; then
				warn "fail2ban jail ${j} has ${banned} banned IPs"
				fail2ban-client status "$j" 2>/dev/null \
					| grep "Banned IP" | sed 's/^/  /'
			else
				ok "fail2ban jail ${j}: no banned IPs"
			fi
		done
	fi
elif command -v fail2ban-client >/dev/null 2>&1; then
	echo "fail2ban installed but not running, skipping"
else
	echo "fail2ban not installed, skipping"
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
		cat <<EOF

DNS setup guidance for ${domain}
================================
Required records (point at this server's public IPs):
  A      ${domain}
  A      share.${domain}
  A      groups.${domain}
  AAAA   same three names, if the server has IPv6

Recommended SRV records:
  _xmpp-client._tcp.${domain}  0 5 5222 ${domain}
  _xmpp-server._tcp.${domain}  0 5 5269 ${domain}

Optional records:
  CAA    ${domain}  0 issue "letsencrypt.org"
  TLSA   _443._tcp.${domain}   (DANE, needs a stable cert)
  TLSA   _5222._tcp.${domain}
  TLSA   _5269._tcp.${domain}

Missing A/AAAA or wrong IPs block everything else.
Missing SRV records degrade federation and client discovery but the
service still works on the direct ports.
EOF
	fi
else
	warn "python3 missing, skipping verify-domain.py"
fi

echo ""
echo "Summary: ${PASS} pass, ${FAIL} fail"
echo "Full log: $(pwd)/post-check.log"
[[ "$FAIL" -eq 0 ]]
