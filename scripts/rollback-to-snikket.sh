#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

FROM_DIR=""
BACKUP_DIR=""
YES=0

usage() {
	cat <<'EOF'
Usage: ./scripts/rollback-to-snikket.sh [--from /path/to/classic/snikket] [--backup-dir /abs/backup/dir] [--yes]

Stops SnikketX, restores Prosody /snikket from a pre-migrate backup into the
classic volume/container layout, and starts classic compose in --from.

Flags fall back to deploy/migrate/state.env (written by migrate), then
backups/LAST_PRE_MIGRATE. --backup-dir accepts a snikketx-backup-* dir or
its parent. If the classic compose file is gone it is restored from the
backup copy (docker-compose.classic.yml).
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	-h | --help)
		usage
		exit 0
		;;
	--from)
		FROM_DIR="$2"
		shift 2
		;;
	--backup-dir)
		BACKUP_DIR="$2"
		shift 2
		;;
	--yes | -y)
		YES=1
		shift
		;;
	*)
		echo "Unknown argument: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

# deploy/migrate/state.env is written by migrate-from-snikket.sh and provides
# CLASSIC_FROM plus BACKUP_DIR (the snikketx-backup-* dir) as fallbacks.
# Explicit flags win over state.env values.
if [[ -f deploy/migrate/state.env ]]; then
	saved_from="$FROM_DIR"
	saved_backup="$BACKUP_DIR"
	# shellcheck disable=SC1091
	. deploy/migrate/state.env
	FROM_DIR="${saved_from:-${CLASSIC_FROM:-}}"
	BACKUP_DIR="${saved_backup:-${BACKUP_DIR:-}}"
fi

if [[ -z "$FROM_DIR" || ! -d "$FROM_DIR" ]]; then
	echo "--from DIR is required" >&2
	exit 1
fi
FROM_DIR="$(cd "$FROM_DIR" && pwd)"

if [[ -z "$BACKUP_DIR" && -f backups/LAST_PRE_MIGRATE ]]; then
	BACKUP_DIR=$(cat backups/LAST_PRE_MIGRATE)
fi

# BACKUP_DIR may point at a snikketx-backup-* dir or at a parent dir that
# holds several of them. Pick the newest dir containing a data tarball.
find_data_tar() {
	local dir="$1"
	if [[ -d "$dir" ]]; then
		ls -1 "$dir"/snikket-data-*.tar.gz 2>/dev/null | head -n1
	fi
}
DATA_TAR=$(find_data_tar "$BACKUP_DIR")
if [[ -z "$DATA_TAR" && -d "$BACKUP_DIR" ]]; then
	nested=$(ls -1dt "$BACKUP_DIR"/snikketx-backup-* 2>/dev/null | head -n1 || true)
	if [[ -n "$nested" ]]; then
		DATA_TAR=$(find_data_tar "$nested")
		[[ -n "$DATA_TAR" ]] && BACKUP_DIR="$nested"
	fi
fi
if [[ -z "$DATA_TAR" ]]; then
	echo "No snikket-data-*.tar.gz found under ${BACKUP_DIR:-<unset>}" >&2
	echo "Pass --backup-dir pointing at a snikketx-backup-* directory." >&2
	exit 1
fi
BACKUP_DIR="$(cd "$BACKUP_DIR" && pwd)"

if [[ "$YES" -ne 1 ]]; then
	echo "This will stop SnikketX and restore Prosody data into classic stack at ${FROM_DIR}"
	echo "from ${DATA_TAR}"
	echo -n "Continue? [y/N] "
	read -r -n1 ans
	echo ""
	case "$ans" in
	y | Y) ;;
	*)
		echo "Aborting."
		exit 1
		;;
	esac
fi

echo "== Stop SnikketX =="
./scripts/stop.sh || true
rm -f deploy/migrate/docker-compose.migrate.yml

echo "== Start classic containers enough for volume =="
CLASSIC_COMPOSE_FOUND=0
for f in docker-compose.yml docker-compose.yaml compose.yml compose.yaml; do
	if [[ -f "$FROM_DIR/$f" ]]; then
		CLASSIC_COMPOSE_FOUND=1
		break
	fi
done
if [[ "$CLASSIC_COMPOSE_FOUND" -eq 0 && -f "$BACKUP_DIR/docker-compose.classic.yml" ]]; then
	cp -a "$BACKUP_DIR/docker-compose.classic.yml" "$FROM_DIR/docker-compose.yml"
	echo "Restored classic docker-compose.yml from backup"
	CLASSIC_COMPOSE_FOUND=1
fi
if [[ "$CLASSIC_COMPOSE_FOUND" -eq 0 ]]; then
	echo "Classic compose file missing in ${FROM_DIR} and none in the backup." >&2
	exit 1
fi
(cd "$FROM_DIR" && snikketx_docker_compose up -d snikket_server 2>/dev/null || snikketx_docker_compose up -d) || true
# Ensure snikket container exists
sleep 2

echo "== Restore Prosody data =="
./scripts/restore.sh "$DATA_TAR" --yes

# Restore classic snikket.conf if we have it
if [[ -f "$BACKUP_DIR/snikket.conf" ]]; then
	cp -a "$BACKUP_DIR/snikket.conf" "$FROM_DIR/snikket.conf"
fi

echo "== Start classic stack =="
(cd "$FROM_DIR" && snikketx_docker_compose up -d)

echo ""
echo "Rollback complete. Classic stack should be running from ${FROM_DIR}."
echo "Verify XMPP and the classic portal before removing SnikketX."
