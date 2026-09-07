#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root
snikketx_parse_mode "$@"

ARCHIVE=""
FULL=0
YES=0
DRY_RUN=0

usage() {
	cat <<'EOF'
Usage: ./scripts/restore.sh /absolute/path/to/snikket-data-*.tar.gz [flags]

Restores Prosody /snikket from a backup tarball into container snikket
(accounts, MAM chats, MUCs, uploads). Stack should be stopped or snikket stopped.

--full      Also restore portal/ravenguard/updater sibling tarballs and host conf
--yes       Skip confirmation prompt
--dry-run   Validate the archive and print the restore plan without writing
--dev/--prod  Accepted for compose mode compatibility
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	-h|--help)
		usage
		exit 0
		;;
	--full)
		FULL=1
		shift
		;;
	--yes|-y)
		YES=1
		shift
		;;
	--dry-run)
		DRY_RUN=1
		shift
		;;
	--dev|dev|--prod|prod)
		shift
		;;
	/*)
		ARCHIVE="$1"
		shift
		;;
	*)
		echo "Unexpected argument: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

if [[ -z "$ARCHIVE" || ! -f "$ARCHIVE" ]]; then
	echo "Backup file missing: ${ARCHIVE:-}" >&2
	usage >&2
	exit 1
fi

first=$(tar tzf "$ARCHIVE" | head -n1 || true)
if [[ "$first" != "snikket/" && "$first" != "./snikket/" ]]; then
	if ! tar tzf "$ARCHIVE" | head -n20 | grep -qE '^(\./)?snikket/'; then
		echo "Not a valid Snikket data backup (expected snikket/ prefix)." >&2
		exit 1
	fi
fi

SRC_DIR=$(dirname "$ARCHIVE")
SRC_BASE=$(basename "$ARCHIVE")
stamp=${SRC_BASE#snikket-data-}
stamp=${stamp%.tar.gz}

if [[ "$DRY_RUN" -eq 1 ]]; then
	echo "Dry-run restore plan"
	echo "  Prosody data: ${ARCHIVE}"
	echo "  Will replace /snikket in container snikket"
	if [[ "$FULL" -eq 1 ]]; then
		echo "  Full restore enabled"
		for f in \
			"${SRC_DIR}/portal-data-${stamp}.tar.gz" \
			"${SRC_DIR}/ravenguard-data-${stamp}.tar.gz" \
			"${SRC_DIR}/updater-data-${stamp}.tar.gz" \
			"${SRC_DIR}/backup-data-${stamp}.tar.gz"
		do
			if [[ -f "$f" ]]; then
				echo "  found $(basename "$f")"
			else
				echo "  missing $(basename "$f") (skipped)"
			fi
		done
		[[ -f "${SRC_DIR}/snikket.conf" ]] && echo "  would restore snikket.conf" || true
		[[ -f "${SRC_DIR}/env" ]] && echo "  would restore .env" || true
	fi
	echo "No changes were made."
	exit 0
fi

if docker container inspect snikket >/dev/null 2>&1; then
	running=$(docker inspect -f '{{.State.Running}}' snikket 2>/dev/null || echo false)
	if [[ "$running" == "true" ]]; then
		echo "Stopping snikket container for restore ..."
		docker stop snikket >/dev/null
	fi
else
	echo "Container snikket does not exist. Start the stack once so the volume is attached, then re-run restore." >&2
	exit 1
fi

if [[ "$YES" -ne 1 ]]; then
	echo "WARNING: This will replace all data currently under /snikket in the snikket container"
	echo "         with the contents of the provided backup. Existing data will be lost."
	echo -n "Continue? [y/N] "
	read -r -n1 continue_answer
	echo ""
	case "$continue_answer" in
	y|Y) echo "Ok, proceeding..." ;;
	*) echo "Aborting."; exit 1 ;;
	esac
fi

ALPINE="alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"

docker run --rm --volumes-from=snikket \
	--mount type=bind,source="$ARCHIVE",destination=/backup.tar.gz,readonly \
	"$ALPINE" \
	sh -c 'rm -rf /snikket/* /snikket/.[!.]* 2>/dev/null || true; tar xzf /backup.tar.gz -C /'

echo "Restored Prosody data from ${ARCHIVE}"

restore_vol() {
	local tarball="$1"
	local vol="$2"
	[[ -f "$tarball" ]] || return 0
	if ! docker volume inspect "$vol" >/dev/null 2>&1; then
		docker volume create "$vol" >/dev/null
	fi
	echo "Restoring ${vol} from $(basename "$tarball") ..."
	docker run --rm -v "$vol":/data -v "$tarball":/backup.tar.gz:ro "$ALPINE" \
		sh -c 'rm -rf /data/* /data/.[!.]* 2>/dev/null || true; tar xzf /backup.tar.gz -C /data --strip-components=1'
}

if [[ "$FULL" -eq 1 ]]; then
	restore_vol "${SRC_DIR}/portal-data-${stamp}.tar.gz" snikketx_portal_data
	restore_vol "${SRC_DIR}/ravenguard-data-${stamp}.tar.gz" snikketx_ravenguard_data
	restore_vol "${SRC_DIR}/updater-data-${stamp}.tar.gz" snikketx_updater_data
	restore_vol "${SRC_DIR}/backup-data-${stamp}.tar.gz" snikketx_backup_data
	if [[ -f "${SRC_DIR}/snikket.conf" ]]; then
		cp -a "${SRC_DIR}/snikket.conf" ./snikket.conf
		echo "Restored snikket.conf"
	fi
	if [[ -f "${SRC_DIR}/env" ]]; then
		cp -a "${SRC_DIR}/env" ./.env
		echo "Restored .env"
	fi
fi

echo "Start the stack again with make up or make up-dev."
