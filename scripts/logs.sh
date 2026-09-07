#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root
snikketx_parse_mode "$@"
snikketx_require_conf

exec docker compose "${COMPOSE_ARGS[@]}" logs -f "${COMPOSE_EXTRA_ARGS[@]}"
