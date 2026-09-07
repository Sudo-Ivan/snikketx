#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root
snikketx_parse_mode "$@"

FIX=0
for arg in "${COMPOSE_EXTRA_ARGS[@]}"; do
	case "$arg" in
	--fix) FIX=1 ;;
	esac
done

PASS=0
WARN=0
FAIL=0

ok() { echo "PASS  $*"; PASS=$((PASS + 1)); }
warn() { echo "WARN  $*"; WARN=$((WARN + 1)); }
fail() { echo "FAIL  $*"; FAIL=$((FAIL + 1)); }

echo "== SnikketX preflight (${COMPOSE_MODE}) =="

if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
	ok "Docker daemon reachable"
else
	fail "Docker daemon not reachable"
fi

if docker help compose >/dev/null 2>&1; then
	ok "Docker Compose plugin available"
else
	fail "Docker Compose plugin missing"
fi

domain=""
if [[ -f snikket.conf ]]; then
	domain="$(snikketx_conf_get SNIKKET_DOMAIN || true)"
	ok "snikket.conf present (domain=${domain:-unset})"
else
	fail "snikket.conf missing (run ./scripts/init.sh or ./scripts/init.sh --dev)"
fi

if [[ -f .env ]]; then
	ok ".env present"
else
	warn ".env missing (start.sh can recreate it from snikket.conf)"
fi

if [[ -z "$domain" ]]; then
	fail "SNIKKET_DOMAIN is empty"
else
	case "$domain" in
	*.localhost|localhost)
		ok "Local domain ${domain} (DNS checks skipped)"
		;;
	*)
		for host in "$domain" "share.${domain}" "groups.${domain}"; do
			if getent hosts "$host" >/dev/null 2>&1 || getent ahosts "$host" >/dev/null 2>&1; then
				ok "DNS resolves ${host}"
			else
				fail "DNS does not resolve ${host}"
			fi
		done
		if [[ "$FIX" -eq 1 ]]; then
			for host in "$domain" "share.${domain}"; do
				ip=$(getent ahosts "$host" 2>/dev/null | awk '/STREAM/{print $1; exit}')
				if [[ -n "$ip" ]]; then
					if timeout 3 bash -c "echo >/dev/tcp/${ip}/443" 2>/dev/null; then
						ok "TCP ${ip}:443 reachable for ${host}"
					else
						warn "TCP ${ip}:443 not reachable for ${host} (expected before first start)"
					fi
				fi
			done
		fi
		;;
	esac
fi

port_in_use() {
	local port="$1"
	if command -v ss >/dev/null; then
		ss -ltn "( sport = :$port )" 2>/dev/null | tail -n +2 | grep -q .
		return $?
	fi
	if command -v lsof >/dev/null; then
		lsof -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
		return $?
	fi
	return 1
}

check_port() {
	local port="$1"
	local label="$2"
	if port_in_use "$port"; then
		# Allow if our containers already own it
		if docker ps --format '{{.Names}} {{.Ports}}' 2>/dev/null | grep -E "snikket|ravenguard" | grep -q ":${port}->\|:${port}-"; then
			ok "Port ${port} (${label}) owned by SnikketX containers"
		else
			warn "Port ${port} (${label}) already listening (may conflict)"
		fi
	else
		ok "Port ${port} (${label}) free"
	fi
}

if [[ "$COMPOSE_MODE" == "dev" ]]; then
	check_port 8080 "HTTP edge"
else
	check_port 80 "HTTP"
	check_port 443 "HTTPS"
fi
check_port 5222 "XMPP c2s"
check_port 5269 "XMPP s2s"
check_port 3478 "STUN/TURN"
check_port 5349 "TURN TLS"

echo ""
echo "== Firewall hints =="
if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -qi 'Status: active'; then
	warn "ufw is active. Suggested allows:"
	if [[ "$COMPOSE_MODE" == "dev" ]]; then
		echo "       ufw allow 8080/tcp"
	else
		echo "       ufw allow 80/tcp"
		echo "       ufw allow 443/tcp"
	fi
	echo "       ufw allow 5222/tcp"
	echo "       ufw allow 5269/tcp"
	echo "       ufw allow 3478/tcp"
	echo "       ufw allow 3478/udp"
	echo "       ufw allow 5349/tcp"
elif command -v firewall-cmd >/dev/null && firewall-cmd --state 2>/dev/null | grep -qi running; then
	warn "firewalld is active. Suggested:"
	if [[ "$COMPOSE_MODE" == "dev" ]]; then
		echo "       firewall-cmd --permanent --add-port=8080/tcp"
	else
		echo "       firewall-cmd --permanent --add-service=http"
		echo "       firewall-cmd --permanent --add-service=https"
	fi
	echo "       firewall-cmd --permanent --add-port=5222/tcp"
	echo "       firewall-cmd --permanent --add-port=5269/tcp"
	echo "       firewall-cmd --permanent --add-port=3478/tcp"
	echo "       firewall-cmd --permanent --add-port=3478/udp"
	echo "       firewall-cmd --permanent --add-port=5349/tcp"
	echo "       firewall-cmd --reload"
else
	ok "No active ufw/firewalld detected (or inactive)"
fi

echo ""
echo "Summary: ${PASS} pass, ${WARN} warn, ${FAIL} fail"
if [[ "$FAIL" -gt 0 ]]; then
	exit 1
fi
exit 0
