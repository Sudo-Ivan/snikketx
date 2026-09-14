#!/bin/bash
# Switch HTTPS certificate issuance from TLS-ALPN-01 to Cloudflare DNS-01.
#
#   ./scripts/enable-dns-acme.sh [--token CF_TOKEN] [--yes]
#
# The token needs Zone:Read and DNS:Edit on the zone that owns SNIKKET_DOMAIN.
# Create a scoped token at https://dash.cloudflare.com/profile/api-tokens.
#
# What it does:
#   1. Verifies the token against the Cloudflare API
#   2. Locates the zone that owns the XMPP domain
#   3. Stores CF_DNS_API_TOKEN in .env
#   4. Activates deploy/acme-dns/docker-compose.acme-dns.yml
#   5. Restarts Traefik and checks that it comes up
#
# To switch back: remove deploy/acme-dns/docker-compose.acme-dns.yml,
# remove CF_DNS_API_TOKEN from .env, then re-run ./scripts/update.sh.

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root
snikketx_require_conf

TOKEN=""
ASSUME_YES=0
while [[ $# -gt 0 ]]; do
	case "$1" in
	--token) TOKEN="$2"; shift 2 ;;
	--token=*) TOKEN="${1#*=}"; shift ;;
	--yes | -y) ASSUME_YES=1; shift ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

domain="$(snikketx_domain)"
if [[ -z "$domain" ]]; then
	echo "SNIKKET_DOMAIN is not set in snikket.conf" >&2
	exit 1
fi

TOKEN="${TOKEN:-$(snikketx_conf_get CF_DNS_API_TOKEN .env)}"
TOKEN="${TOKEN:-${CF_DNS_API_TOKEN:-}}"
if [[ -z "$TOKEN" ]]; then
	echo "No Cloudflare token given."
	echo "Create one at https://dash.cloudflare.com/profile/api-tokens with:"
	echo "  Permissions: Zone:Read, DNS:Edit"
	echo "  Zone Resources: Include -> Specific zone -> your zone"
	echo ""
	read -srp "Paste token (or Ctrl-C to abort): " TOKEN
	echo ""
	[[ -z "$TOKEN" ]] && exit 1
fi

echo "== 1/5 Verify token =="
verify=$(curl -fsS -H "Authorization: Bearer ${TOKEN}" \
	"https://api.cloudflare.com/client/v4/user/tokens/verify" 2>/dev/null || true)
if ! printf '%s' "$verify" | grep -q '"success":true'; then
	echo "Token verification failed. Check the token and its permissions." >&2
	exit 1
fi
echo "token is valid"

echo "== 2/5 Locate zone =="
zone=""
labels="$domain"
while [[ "$labels" == *.* ]]; do
	res=$(curl -fsS -H "Authorization: Bearer ${TOKEN}" \
		"https://api.cloudflare.com/client/v4/zones?name=${labels}" 2>/dev/null || true)
	if printf '%s' "$res" | grep -q '"count":[1-9]'; then
		zone="$labels"
		break
	fi
	labels="${labels#*.}"
done
if [[ -z "$zone" ]]; then
	echo "No zone covering ${domain} is visible to this token." >&2
	exit 1
fi
echo "zone: ${zone}"

if [[ "$ASSUME_YES" -ne 1 ]]; then
	read -rp "Enable DNS-01 ACME via Cloudflare for ${domain} (zone ${zone})? [y/N] " ans
	case "$ans" in
	y | Y | yes | YES) ;;
	*) echo "aborted"; exit 0 ;;
	esac
fi

echo "== 3/5 Store token in .env =="
touch .env
snikketx_env_set CF_DNS_API_TOKEN "$TOKEN"
echo "CF_DNS_API_TOKEN written to .env"

echo "== 4/5 Activate compose override =="
mkdir -p deploy/acme-dns
cp -a deploy/acme-dns/docker-compose.acme-dns.yml.example \
	deploy/acme-dns/docker-compose.acme-dns.yml
echo "wrote deploy/acme-dns/docker-compose.acme-dns.yml"

echo "== 5/5 Restart Traefik and cert-manager =="
snikketx_parse_mode prod
snikketx_compose up -d traefik snikket_certs
sleep 3
if docker exec snikketx-traefik traefik healthcheck --ping >/dev/null 2>&1; then
	echo "traefik is up with the DNS-01 resolver"
else
	echo "traefik did not come up cleanly, check: docker logs snikketx-traefik" >&2
	exit 1
fi

cat <<EOF

DNS-01 is active for the edge and for XMPP certificates. Existing
certificates stay valid and renew through Cloudflare from now on.
Watch the next issuance with:

  docker logs -f snikketx-traefik
  docker logs -f snikketx-certs

If the DNS records for ${domain} still need to be created, run:

  ./scripts/dns-setup.py --domain ${domain}

Then verify the whole deployment:

  ./scripts/postcheck.sh prod
EOF
