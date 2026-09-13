#!/bin/bash
# Audit and fix the host firewall for SnikketX.
#
#   ./scripts/firewall.sh [--yes]
#
# Creates or updates a ufw application profile that covers every port the
# stack needs, then offers to allow it. Also reviews fail2ban jails for
# banned addresses and offers to lift bans. Only runs on the host that
# runs ufw and fail2ban, not inside containers.

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

ASSUME_YES=0
for arg in "$@"; do
	case "$arg" in
	--yes | -y) ASSUME_YES=1 ;;
	*) echo "unknown argument: $arg" >&2; exit 2 ;;
	esac
done

TURN_MIN="${SNIKKET_TURN_MIN_PORT:-$(snikketx_conf_get SNIKKET_TURN_MIN_PORT .env || true)}"
TURN_MAX="${SNIKKET_TURN_MAX_PORT:-$(snikketx_conf_get SNIKKET_TURN_MAX_PORT .env || true)}"
TURN_MIN="${TURN_MIN:-49152}"
TURN_MAX="${TURN_MAX:-49251}"

# Every port the stack needs. Bare entries in the profile open both
# TCP and UDP, which is what STUN and TURN require.
PROFILE_PORTS="80/tcp|443/tcp|443/udp|5222/tcp|5223/tcp|5269/tcp|5000/tcp|3478|3479|5349|5350|${TURN_MIN}:${TURN_MAX}/udp"

ask() {
	# ask <question> -> 0 for yes
	if [[ "$ASSUME_YES" -eq 1 ]]; then
		echo "$1 yes (--yes)"
		return 0
	fi
	local ans
	read -rp "$1 [y/N] " ans
	case "$ans" in
	y | Y | yes | YES) return 0 ;;
	*) return 1 ;;
	esac
}

echo "== SnikketX firewall setup =="

if ! command -v ufw >/dev/null 2>&1; then
	echo "ufw is not installed on this host, nothing to do"
elif ! ufw status 2>/dev/null | grep -q "Status: active"; then
	cat <<'EOF'
ufw is installed but not active.
Before enabling it, make sure your SSH port is allowed or you can
lock yourself out:

  ufw allow ssh
  ufw enable

Then re-run this script.
EOF
else
	echo "ufw is active"
	profile=/etc/ufw/applications.d/snikketx
	need_write=1
	if [[ -f "$profile" ]] && grep -q "ports=${PROFILE_PORTS}" "$profile"; then
		need_write=0
	fi
	if [[ "$need_write" -eq 1 ]]; then
		echo "writing ufw application profile ${profile}"
		cat >"$profile" <<EOF
[SnikketX]
title=SnikketX Server
description=SnikketX XMPP server ports
ports=${PROFILE_PORTS}
EOF
		ufw app update SnikketX >/dev/null 2>&1 || true
	else
		echo "ufw application profile already up to date"
	fi

	missing=()
	ufw_status=$(ufw status 2>/dev/null)
	check_port() {
		# check_port <port-or-range> <proto> -> 0 when a rule covers it
		local port="$1" proto="$2"
		if printf '%s' "$ufw_status" \
			| grep -qE "(^|[[:space:]])${port}(/${proto})?[[:space:]]+ALLOW"; then
			return 0
		fi
		if printf '%s' "$ufw_status" | grep -qiE "^SnikketX[[:space:]]+ALLOW"; then
			if ufw app info SnikketX 2>/dev/null \
				| grep -qE "(^|[|[:space:]])${port}(/${proto})?([|[:space:]]|$)"; then
				return 0
			fi
		fi
		return 1
	}

	for spec in "80 tcp" "443 tcp" "443 udp" "5222 tcp" "5223 tcp" \
		"5269 tcp" "5000 tcp" "3478 tcp" "3478 udp" "3479 tcp" "3479 udp" \
		"5349 tcp" "5349 udp" "5350 tcp" "5350 udp" \
		"${TURN_MIN}:${TURN_MAX} udp"; do
		set -- $spec
		if check_port "$1" "$2"; then
			echo "  PASS  ufw allows $1/$2"
		else
			echo "  FAIL  ufw does not allow $1/$2"
			missing+=("$1/$2")
		fi
	done

	if [[ "${#missing[@]}" -eq 0 ]]; then
		echo "all SnikketX ports are already allowed"
	elif ask "Open the missing ports with ufw allow SnikketX?"; then
		ufw allow SnikketX
		ufw reload >/dev/null 2>&1 || true
		echo "rule added:"
		ufw status | grep -i snikket || true
	else
		echo "skipped. Manual equivalent:"
		for m in "${missing[@]}"; do
			echo "  ufw allow $m"
		done
		echo "  or: ufw allow SnikketX"
	fi
fi

echo ""
echo "== fail2ban =="

if ! command -v fail2ban-client >/dev/null 2>&1; then
	echo "fail2ban is not installed, nothing to do"
elif ! fail2ban-client ping >/dev/null 2>&1; then
	echo "fail2ban is installed but not running, nothing to do"
else
	jails=$(fail2ban-client status 2>/dev/null \
		| sed -n 's/.*Jail list:[[:space:]]*//p' | tr ',' ' ')
	if [[ -z "$jails" ]]; then
		echo "fail2ban is running with no active jails"
	else
		for j in $jails; do
			banned=$(fail2ban-client status "$j" 2>/dev/null \
				| sed -n 's/.*Currently banned:[[:space:]]*//p' | tr -d ' ')
			if [[ "${banned:-0}" -eq 0 ]]; then
				echo "  jail ${j}: no banned IPs"
				continue
			fi
			ips=$(fail2ban-client status "$j" 2>/dev/null \
				| sed -n 's/.*Banned IP list:[[:space:]]*//p')
			echo "  jail ${j}: ${banned} banned (${ips})"
			if ask "  lift the ban(s) in jail ${j}?"; then
				for ip in $ips; do
					fail2ban-client set "$j" unbanip "$ip" >/dev/null 2>&1 \
						&& echo "    unbanned ${ip}"
				done
			fi
		done
	fi
fi
