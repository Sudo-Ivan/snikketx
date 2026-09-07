#!/bin/bash

set -eo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

MODE=prod
COMPOSE_ARGS=(-f docker-compose.yml)

if [[ "${1:-}" == "--dev" || "${1:-}" == "dev" ]]; then
	MODE=dev
	COMPOSE_ARGS+=(-f docker-compose.dev.yml)
fi

if [ ! -f docker-compose.yml ]; then
	echo 'docker-compose.yml is missing from this checkout.'
	exit 1
fi

if [ ! -f snikket.conf ]; then
	echo 'Almost there! You need to run ./scripts/init.sh first.'
	exit 1
fi

if [ ! -f .env ]; then
	# Recover .env from snikket.conf for compose interpolation
	domain=$(grep -E '^SNIKKET_DOMAIN=' snikket.conf | head -n1 | cut -d= -f2-)
	email=$(grep -E '^SNIKKET_ADMIN_EMAIL=' snikket.conf | head -n1 | cut -d= -f2-)
	secret=$(head -c 32 /dev/urandom | base64 | tr -d '\n=/+' | head -c 32)
	cat > .env <<EOF
SNIKKET_DOMAIN=${domain}
SNIKKET_ADMIN_EMAIL=${email}
RG_CHALLENGE_SECRET=${secret}
EOF
	echo "Wrote .env from snikket.conf"
fi

./scripts/render-edge.sh "$MODE"

if [[ "$MODE" == "dev" ]]; then
	exec docker compose "${COMPOSE_ARGS[@]}" up -d --build
fi

exec docker compose "${COMPOSE_ARGS[@]}" up -d
