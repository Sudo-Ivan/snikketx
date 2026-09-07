#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root
snikketx_parse_mode "$@"
snikketx_require_conf
snikketx_ensure_env
snikketx_export_build_meta

if [[ "$COMPOSE_MODE" == "dev" ]]; then
	snikketx_compose up -d --build
else
	snikketx_compose up -d
fi

./scripts/seed-ravenguard-routes.sh "$COMPOSE_MODE"

if [[ "$COMPOSE_MODE" == "dev" ]]; then
	./scripts/bootstrap-admin.sh --dev || true
	echo ""
	echo "Dev stack ready: $(snikketx_http_base)/login"
	echo "Default admin: admin@$(snikketx_domain) / admin"
fi
