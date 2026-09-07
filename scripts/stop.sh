#!/bin/bash

set -eo pipefail

COMPOSE_ARGS=(-f docker-compose.yml)
if [[ "${1:-}" == "--dev" || "${1:-}" == "dev" ]]; then
	COMPOSE_ARGS+=(-f docker-compose.dev.yml)
fi

exec docker compose "${COMPOSE_ARGS[@]}" down
