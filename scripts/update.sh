#!/bin/bash
# Update a SnikketX deployment: git pull, pull images, restart, verify.
#
#   ./scripts/update.sh [--dev|--prod] [--no-git] [--skip-external]
#
# --no-git         skip the fetch/pull step (just pull images + restart)
# --skip-external  forwarded to postcheck.sh (skip DNS/TLS/DANE probes)
#
# If the pull brings in new commits the script re-execs itself once so the
# new code runs the rest of the update.

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

NO_GIT=0
for arg in "$@"; do
	case "$arg" in
	--no-git) NO_GIT=1 ;;
	esac
done

if [[ "$NO_GIT" -eq 0 && "${SNIKKETX_UPDATE_REEXEC:-0}" != "1" ]] \
	&& git rev-parse --git-dir >/dev/null 2>&1; then
	echo "== git update =="
	if ! git rev-parse --abbrev-ref '@{u}' >/dev/null 2>&1; then
		echo "WARN  no upstream configured for this branch; skipping pull"
	elif ! git fetch --quiet; then
		echo "WARN  git fetch failed; continuing with local checkout"
	elif [[ -n "$(git status --porcelain)" ]]; then
		echo "WARN  working tree has changes; skipping pull"
	elif git merge-base --is-ancestor HEAD '@{u}'; then
		if [[ "$(git rev-parse HEAD)" == "$(git rev-parse '@{u}')" ]]; then
			echo "already up to date ($(git rev-parse --short HEAD))"
		else
			old=$(git rev-parse --short HEAD)
			git pull --ff-only
			echo "updated ${old} -> $(git rev-parse --short HEAD); re-exec"
			SNIKKETX_UPDATE_REEXEC=1 exec "$0" "$@"
		fi
	else
		echo "WARN  branch has diverged from upstream; skipping pull"
	fi
fi

snikketx_parse_mode "$@"
snikketx_require_conf
snikketx_ensure_env
snikketx_export_build_meta

echo "== compose update (${COMPOSE_MODE}) =="
if [[ "$COMPOSE_MODE" == "dev" ]]; then
	snikketx_compose up -d --build
else
	snikketx_compose pull
	snikketx_compose up -d
fi

./scripts/seed-ravenguard-routes.sh "$COMPOSE_MODE"
./scripts/postcheck.sh "$COMPOSE_MODE" "${COMPOSE_EXTRA_ARGS[@]}"
