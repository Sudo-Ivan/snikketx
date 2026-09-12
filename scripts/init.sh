#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

## Platform detection ##
OS=$(awk '/DISTRIB_ID=/' /etc/*-release 2>/dev/null | sed 's/DISTRIB_ID=//' | tr '[:upper:]' '[:lower:]' || true)
if [ -z "$OS" ]; then
	OS=$(awk '{print $1}' /etc/*-release 2>/dev/null | tr '[:upper:]' '[:lower:]' || true)
fi

DEV_MODE=0
FORCE=0
DOMAIN_ARG=""
EMAIL_ARG=""

while [[ $# -gt 0 ]]; do
	case "$1" in
	--dev)
		DEV_MODE=1
		shift
		;;
	--force | -f)
		FORCE=1
		shift
		;;
	--domain)
		DOMAIN_ARG="$2"
		shift 2
		;;
	--email)
		EMAIL_ARG="$2"
		shift 2
		;;
	--noninteractive)
		# Alias used with --domain/--email or --dev
		shift
		;;
	-h | --help)
		cat <<'EOF'
Usage: ./scripts/init.sh [--dev] [--force] [--domain NAME] [--email ADDR]

  --dev     Noninteractive local defaults (chat.localhost)
  --force   Overwrite existing snikket.conf / .env
  --domain  Set domain noninteractively (implies noninteractive with --email)
  --email   Admin email for noninteractive prod init
EOF
		exit 0
		;;
	*)
		echo "Unknown option: $1" >&2
		exit 1
		;;
	esac
done

if ! command -v docker >/dev/null; then
	echo "Docker is required but not installed."
	case "$OS" in
	ubuntu | debian | fedora | centos)
		echo "Please follow the guide at https://docs.docker.com/install/linux/docker-ce/$OS/"
		;;
	*)
		echo "Please follow the installation guide at https://docs.docker.com/engine/install/"
		;;
	esac
	exit 1
fi

if ! docker help compose >/dev/null 2>&1; then
	echo "Docker Compose extension is required, but not installed."
	echo "Please follow the installation guide at https://docs.docker.com/compose/install/linux/#install-using-the-repository"
	exit 1
fi

if [ ! -f docker-compose.yml ]; then
	echo "docker-compose.yml is missing from this checkout."
	exit 1
fi

write_config() {
	local domain="$1"
	local email="$2"
	local tos="$3"
	local rg_secret rg_admin updater_token backup_token
	rg_secret=$(snikketx_random 32)
	if [ "${#rg_secret}" -lt 16 ]; then
		rg_secret="rg-replace-this-secret!!"
	fi
	rg_admin=$(snikketx_random 24)
	updater_token=$(snikketx_random 32)
	backup_token=$(snikketx_random 32)

	if [[ "$DEV_MODE" -eq 1 ]]; then
		rg_secret="rg-dev-local-secret!!"
		rg_admin="rg-dev-admin-password!!"
	fi

	sed \
		-e 's/^\(SNIKKET_DOMAIN\)=.*$/\1='"$domain"'/;' \
		-e 's/^\(SNIKKET_ADMIN_EMAIL\)=.*$/\1='"$email"'/;' \
		-e 's/^\(SNIKKET_LETSENCRYPT_TOS_AGREE\)=.*$/\1='"$tos"'/;' \
		snikket.conf.example >snikket.conf

	cat >.env <<EOF
SNIKKET_DOMAIN=${domain}
SNIKKET_ADMIN_EMAIL=${email}
RG_CHALLENGE_SECRET=${rg_secret}
RG_ADMIN_BOOTSTRAP_PASSWORD=${rg_admin}
SNIKKET_UPDATER_TOKEN=${updater_token}
SNIKKET_BACKUP_TOKEN=${backup_token}
EOF
}

if [[ "$DEV_MODE" -eq 1 ]]; then
	if [[ -f snikket.conf && "$FORCE" -ne 1 ]]; then
		echo "snikket.conf already exists. Re-run with --force to overwrite, or use make up-dev."
		exit 0
	fi
	write_config "chat.localhost" "admin@chat.localhost" "Y"
	echo ""
	echo "Dev config written to snikket.conf and .env (domain chat.localhost)."
	echo "Next:"
	echo "  make up-dev"
	echo "  open http://chat.localhost:8080/login  (admin@chat.localhost / admin after start)"
	echo "Add chat.localhost to /etc/hosts pointing at 127.0.0.1 if needed."
	exit 0
fi

if [[ -n "$DOMAIN_ARG" ]]; then
	if [[ -z "$EMAIL_ARG" ]]; then
		echo "--domain requires --email" >&2
		exit 1
	fi
	if [[ -f snikket.conf && "$FORCE" -ne 1 ]]; then
		echo "snikket.conf already exists. Re-run with --force to overwrite."
		exit 1
	fi
	write_config "$DOMAIN_ARG" "$EMAIL_ARG" "Y"
	echo "Config written for $DOMAIN_ARG."
	echo "Run ./scripts/preflight.sh then make up"
	exit 0
fi

if [ -f snikket.conf ]; then
	echo "It appears you already have a snikket.conf file"
	echo -n "Would you like to keep the existing file? [Y/n] "
	read -r -n1 -p "" remove_existing_config
	case "$remove_existing_config" in
	n | N) rm snikket.conf .env 2>/dev/null || true ;;
	*)
		exit 0
		;;
	esac
	echo ""
	echo ""
fi

echo "## SnikketX setup ##"
echo ""
echo "Welcome to SnikketX. We're nearly ready to start your"
echo "new service. First we need some configuration details."

echo ""
echo ""
echo "SnikketX domain. This is the domain name your service will use."
echo "For example, 'example.com' or 'chat.example.com'."
echo "It must be a domain you own, with DNS records for this"
echo "server's IP address. The domain/subdomain you enter will be"
echo "dedicated to SnikketX, and cannot be shared with e.g. a website."
echo ""
read -r -p "Enter domain: " SNIKKET_DOMAIN

echo ""
echo ""
echo "Admin email address. This is communicated to your users"
echo "of the $SNIKKET_DOMAIN service in case they require assistance."
echo "It is also provided to Let's Encrypt for SSL/TLS certificates."
echo ""
read -r -p "Enter admin email address: " SNIKKET_ADMIN_EMAIL

echo ""
echo ""
echo "Finally, please confirm that you accept the Let's Encrypt terms"
echo "of service, which can be reviewed at:"
echo "https://letsencrypt.org/documents/LE-SA-v1.2-November-15-2017.pdf"
echo ""
read -r -n1 -p "Enter 'Y' to confirm: " SNIKKET_LETSENCRYPT_TOS_AGREE
echo ""

case "$SNIKKET_LETSENCRYPT_TOS_AGREE" in
Y | y) ;;
*)
	echo "SnikketX requires certificates from Let's Encrypt to set up"
	echo "the server. Since you do not accept the terms of service"
	echo "(you answered: $SNIKKET_LETSENCRYPT_TOS_AGREE), the installation"
	echo "cannot continue."
	exit 1
	;;
esac

write_config "$SNIKKET_DOMAIN" "$SNIKKET_ADMIN_EMAIL" "$SNIKKET_LETSENCRYPT_TOS_AGREE"

echo ""
echo "Success! Configuration saved to snikket.conf and .env."
echo "Check DNS and ports:  ./scripts/preflight.sh"
echo "Start production:     make up"
echo "Start local builds:   make up-dev   or  ./scripts/init.sh --dev && make up-dev"

exit 0
