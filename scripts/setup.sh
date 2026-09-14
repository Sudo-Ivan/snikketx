#!/bin/bash

set -eo pipefail

# Unified SnikketX installer. Walks through every step that used to be a
# separate script: config, auth (OIDC/LDAP), migration, DNS, certificates,
# firewall, start and verification. Each optional step asks first, and
# --dry-run prints the whole plan without touching anything.

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

DRY_RUN=0
INIT_ARGS=()
NONINTERACTIVE=0

usage() {
	cat <<'EOF'
Usage: ./scripts/setup.sh [options]

One-shot SnikketX setup wizard. Runs init, optional auth configuration
(OIDC portal SSO, XMPP OAUTHBEARER, LDAP), optional migration, DNS record
setup, DNS-01 certificates, preflight, start, firewall and postcheck.

Options:
  --dry-run, -n      Print every step and command without executing
  --dev              Local dev defaults (chat.localhost)
  --domain NAME      Domain for noninteractive init
  --email ADDR       Admin email for noninteractive init
  --noninteractive   No prompts, defaults for all optional steps
  --force            Overwrite existing snikket.conf/.env
  -h, --help         This help
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--dry-run | -n)
		DRY_RUN=1
		shift
		;;
	--dev | --force | --noninteractive)
		INIT_ARGS+=("$1")
		[[ "$1" == "--noninteractive" ]] && NONINTERACTIVE=1
		shift
		;;
	--domain | --email)
		INIT_ARGS+=("$1" "$2")
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "Unknown option: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

if [[ "$DRY_RUN" -eq 1 ]]; then
	echo "== SnikketX setup (dry run, nothing will change) =="
	echo ""
fi

# run CMD... executes or prints the command in dry-run mode.
run() {
	if [[ "$DRY_RUN" -eq 1 ]]; then
		echo "  [dry-run] $*"
		return 0
	fi
	"$@"
}

# ask VAR PROMPT reads into VAR, or echoes "[dry-run]" in dry-run mode.
# shellcheck disable=SC2154
ask() {
	local __var="$1" __prompt="$2" __default="${3:-}"
	if [[ "$DRY_RUN" -eq 1 || "$NONINTERACTIVE" -eq 1 ]]; then
		printf -v "$__var" '%s' "$__default"
		return 0
	fi
	local __hint="" __input
	[[ -n "$__default" ]] && __hint=" [$__default]"
	read -r -p "$__prompt$__hint: " __input
	printf -v "$__var" '%s' "${__input:-$__default}"
}

# ask_secret VAR PROMPT reads without echo.
ask_secret() {
	local __var="$1" __prompt="$2"
	if [[ "$DRY_RUN" -eq 1 || "$NONINTERACTIVE" -eq 1 ]]; then
		printf -v "$__var" '%s' ""
		return 0
	fi
	local __input
	read -r -s -p "$__prompt: " __input
	echo ""
	printf -v "$__var" '%s' "$__input"
}

# confirm PROMPT returns 0 for yes. Default answer comes from $2 (y|n).
confirm() {
	local __prompt="$1" __default="${2:-n}" __ans
	if [[ "$DRY_RUN" -eq 1 ]]; then
		echo "  [dry-run] would ask: $__prompt (default $__default)"
		return 1
	fi
	if [[ "$NONINTERACTIVE" -eq 1 ]]; then
		[[ "$__default" == "y" ]]
		return
	fi
	read -r -p "$__prompt [$__default] " __ans
	__ans="${__ans:-$__default}"
	[[ "$__ans" =~ ^[Yy] ]]
}

step() {
	echo ""
	echo "== $1 =="
}

# conf_set KEY VALUE writes to snikket.conf (or prints in dry-run).
conf_set() {
	if [[ "$DRY_RUN" -eq 1 ]]; then
		echo "  [dry-run] snikket.conf: $1=$2"
		return 0
	fi
	snikketx_env_set "$1" "$2" snikket.conf
}

## Step 1: base config ##

step "1/9  Base configuration"
if [[ -f snikket.conf ]]; then
	echo "snikket.conf already exists, keeping it (use --force via init.sh to overwrite)."
	domain="$(snikketx_domain)"
else
	run ./scripts/init.sh "${INIT_ARGS[@]}"
	domain="$(snikketx_conf_get SNIKKET_DOMAIN || echo 'chat.example.com')"
fi

## Step 2: optional migration ##

step "2/9  Migration from classic Snikket"
if confirm "Migrate existing data from a classic Snikket install?" n; then
	run ./scripts/migrate-from-snikket.sh
else
	echo "Skipping migration."
fi

## Step 3: authentication ##

step "3/9  Authentication (optional OIDC / LDAP)"
oidc_portal=0
oidc_issuer=""

if confirm "Configure external authentication (OIDC/LDAP)?" n; then
	if confirm "  Enable OIDC portal sign-on (Pocket ID, Keycloak, ...)?" n; then
		oidc_portal=1
		oidc_client_id="" oidc_client_secret="" oauth_userinfo=""
		ask oidc_issuer "  OIDC issuer URL" "https://auth.example.com"
		oidc_issuer="${oidc_issuer%/}"
		ask oidc_client_id "  OIDC client ID" "snikket-portal"
		ask_secret oidc_client_secret "  OIDC client secret"
		conf_set SNIKKET_WEB_OIDC_ISSUER "$oidc_issuer"
		conf_set SNIKKET_WEB_OIDC_CLIENT_ID "$oidc_client_id"
		conf_set SNIKKET_WEB_OIDC_CLIENT_SECRET "$oidc_client_secret"
		conf_set SNIKKET_WEB_OIDC_REDIRECT_URL "https://${domain}/auth/oidc/callback"
		conf_set SNIKKET_WEB_OIDC_USERNAME_CLAIM "preferred_username"
		conf_set SNIKKET_WEB_OIDC_SCOPES "openid profile email"
		svc_pass="$(snikketx_random 32)"
		conf_set SNIKKET_WEB_SERVICE_ADDRESS "portal-service@${domain}"
		conf_set SNIKKET_WEB_SERVICE_PASSWORD "$svc_pass"
		echo "  Provisioning service account will run after first start."

		if confirm "  Also enable XMPP OAuth sign-in (OAUTHBEARER) for clients like Badinage?" n; then
			conf_set SNIKKET_TWEAK_OAUTH "1"
			conf_set SNIKKET_OAUTH_DISCOVERY_URL "${oidc_issuer}/.well-known/openid-configuration"
			ask oauth_userinfo "  Token validation (userinfo) endpoint" "${oidc_issuer}/api/oidc/userinfo"
			conf_set SNIKKET_OAUTH_VALIDATION_ENDPOINT "$oauth_userinfo"
			conf_set SNIKKET_OAUTH_USERNAME_FIELD "preferred_username"
			conf_set SNIKKET_OAUTH_SCOPE "openid profile"
			echo "  Note: Badinage needs dynamic client registration (RFC 7591) or a"
			echo "  client-id-metadata document at the IdP. Pocket ID supports the"
			echo "  metadata-document style, enable its allowlist if OAuth sign-in fails."
		fi
	fi

	if confirm "  Enable LDAP directory authentication?" n; then
		ldap_host="" ldap_port="" ldap_bind_dn="" ldap_bind_pw=""
		ldap_user_base="" ldap_user_field="" ldap_name_field=""
		ldap_filter="" ldap_group_base="" ldap_group_member=""
		conf_set SNIKKET_TWEAK_LDAP "1"
		ask ldap_host "  LDAP host" "ldap.example.com"
		ask ldap_port "  LDAP port" "389"
		if confirm "  Use StartTLS?" y; then
			conf_set SNIKKET_LDAP_USE_TLS "1"
		fi
		ask ldap_bind_dn "  Bind DN (read-only account)" "cn=prosody,dc=example,dc=com"
		ask_secret ldap_bind_pw "  Bind password"
		ask ldap_user_base "  User base DN" "ou=users,dc=example,dc=com"
		ask ldap_user_field "  Username attribute" "uid"
		ask ldap_name_field "  Display name attribute" "cn"
		ask ldap_filter "  User filter" "(objectClass=inetOrgPerson)"
		conf_set SNIKKET_LDAP_HOST "$ldap_host"
		conf_set SNIKKET_LDAP_PORT "$ldap_port"
		conf_set SNIKKET_LDAP_BIND_DN "$ldap_bind_dn"
		conf_set SNIKKET_LDAP_BIND_PASSWORD "$ldap_bind_pw"
		conf_set SNIKKET_LDAP_USER_BASE_DN "$ldap_user_base"
		conf_set SNIKKET_LDAP_USERNAME_FIELD "$ldap_user_field"
		conf_set SNIKKET_LDAP_NAME_FIELD "$ldap_name_field"
		conf_set SNIKKET_LDAP_USER_FILTER "$ldap_filter"
		if confirm "  Restrict to an LDAP group?" n; then
			ask ldap_group_base "  Group base DN" "ou=groups,dc=example,dc=com"
			ask ldap_group_member "  Group member attribute" "member"
			conf_set SNIKKET_LDAP_GROUP_BASE_DN "$ldap_group_base"
			conf_set SNIKKET_LDAP_GROUP_MEMBER_FIELD "$ldap_group_member"
		fi
		echo "  Note: LDAP auth uses a simple bind. Passwords reach the server"
		echo "  in plaintext form protected only by TLS. Registration/invites are"
		echo "  disabled because accounts come from the directory."
	fi
else
	echo "Keeping password auth (internal_hashed)."
fi

## Step 4: DNS records ##

step "4/9  DNS records"
if confirm "Set up DNS records via Cloudflare API (zone backup first)?" n; then
	run ./scripts/dns-setup.py --domain "$domain"
else
	echo "Skipping DNS automation. Required records are printed by postcheck."
fi

## Step 5: DNS-01 certificates ##

step "5/9  Certificate challenge"
if confirm "Switch ACME to DNS-01 (Cloudflare token, works with port 80 closed)?" n; then
	run ./scripts/enable-dns-acme.sh
else
	echo "Keeping HTTP-01/TLS-ALPN-01 (default)."
fi

## Step 6: preflight ##

step "6/9  Preflight"
run ./scripts/preflight.sh || echo "Preflight reported problems. Fix them and re-run setup or continue anyway."

## Step 7: start ##

step "7/9  Start the stack"
if confirm "Start SnikketX now?" y; then
	run ./scripts/start.sh
else
	echo "Skipped start. Run ./scripts/start.sh when ready."
fi

## Step 8: OIDC service account provisioning ##

step "8/9  OIDC service account"
if [[ "$oidc_portal" -eq 1 ]]; then
	if [[ "$DRY_RUN" -eq 1 ]]; then
		run docker exec "$SNIKKETX_CONTAINER_SERVER" prosodyctl shell user create "portal-service@${domain}" "<generated>" "prosody:admin"
	else
		svc_pass="$(snikketx_conf_get SNIKKET_WEB_SERVICE_PASSWORD || true)"
		if [[ -n "$svc_pass" ]]; then
			echo "Waiting for Prosody to accept commands..."
			for _ in $(seq 1 30); do
				if docker exec "$SNIKKETX_CONTAINER_SERVER" prosodyctl shell user create "portal-service@${domain}" "$svc_pass" "prosody:admin" 2>/dev/null; then
					echo "Provisioned portal-service@${domain}"
					break
				fi
				sleep 2
			done
			# If create failed because the account already exists, still
			# ensure the role is granted.
			docker exec "$SNIKKETX_CONTAINER_SERVER" prosodyctl shell user set_role "portal-service@${domain}" "$domain" "prosody:admin" 2>/dev/null || true
		fi
	fi
else
	echo "No OIDC portal sign-on configured, skipping."
fi

## Step 9: firewall + postcheck ##

step "9/9  Firewall and verification"
if confirm "Audit/open host firewall ports (ufw)?" y; then
	run ./scripts/firewall.sh
fi
run ./scripts/postcheck.sh

echo ""
if [[ "$DRY_RUN" -eq 1 ]]; then
	echo "Dry run complete. Re-run without --dry-run to apply."
else
	echo "Setup complete. Portal: https://${domain}/login"
fi
