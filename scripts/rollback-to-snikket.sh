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
Usage: ./scripts/rollback-to-snikket.sh --from /path/to/classic/snikket --backup-dir /abs/backup/dir [--yes]

Stops SnikketX, restores Prosody /snikket from a pre-migrate backup into the
classic volume/container layout, and starts classic compose in --from.

Requires the classic checkout to still exist with its docker-compose.yml.
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

if [[ -z "$FROM_DIR" || ! -d "$FROM_DIR" ]]; then
	echo "--from DIR is required" >&2
	exit 1
fi
FROM_DIR="$(cd "$FROM_DIR" && pwd)"

if [[ -z "$BACKUP_DIR" ]]; then
	if [[ -f backups/LAST_PRE_MIGRATE ]]; then
		BACKUP_DIR=$(cat backups/LAST_PRE_MIGRATE)
	fi
fi
if [[ -z "$BACKUP_DIR" || ! -d "$BACKUP_DIR" ]]; then
	echo "--backup-dir DIR is required (snikketx-backup-* directory)" >&2
	exit 1
fi
BACKUP_DIR="$(cd "$BACKUP_DIR" && pwd)"

DATA_TAR=$(ls -1 "$BACKUP_DIR"/snikket-data-*.tar.gz 2>/dev/null | head -n1 || true)
if [[ -z "$DATA_TAR" ]]; then
	echo "No snikket-data-*.tar.gz in ${BACKUP_DIR}" >&2
	exit 1
fi

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
if [[ ! -f "$FROM_DIR/docker-compose.yml" ]]; then
	echo "Classic docker-compose.yml missing in ${FROM_DIR}" >&2
	exit 1
fi
(cd "$FROM_DIR" && docker compose up -d snikket_server 2>/dev/null || docker compose up -d) || true
# Ensure snikket container exists
sleep 2

echo "== Restore Prosody data =="
./scripts/restore.sh "$DATA_TAR" --yes

# Restore classic snikket.conf if we have it
if [[ -f "$BACKUP_DIR/snikket.conf" ]]; then
	cp -a "$BACKUP_DIR/snikket.conf" "$FROM_DIR/snikket.conf"
fi

echo "== Start classic stack =="
(cd "$FROM_DIR" && docker compose up -d)

echo ""
echo "Rollback complete. Classic stack should be running from ${FROM_DIR}."
echo "Verify XMPP and the classic portal before removing SnikketX."
