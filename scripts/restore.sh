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

Restores Prosody /snikket from a backup tarball into the server container
(accounts, MAM chats, MUCs, uploads). Stack should be stopped or the server
container stopped.

--full      Also restore portal/traefik/updater sibling tarballs and host conf
--yes       Skip confirmation prompt
--dry-run   Validate the archive and print the restore plan without writing
--dev/--prod  Accepted for compose mode compatibility
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	-h | --help)
		usage
		exit 0
		;;
	--full)
		FULL=1
		shift
		;;
	--yes | -y)
		YES=1
		shift
		;;
	--dry-run)
		DRY_RUN=1
		shift
		;;
	--dev | dev | --prod | prod)
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

# Verify integrity when the backup carries a sha256 sidecar or an
# aggregate SHA256SUMS file. Refuse to restore a corrupted archive.
verify_archive() {
	local archive="$1" dir base
	dir=$(dirname "$archive")
	base=$(basename "$archive")
	if [[ -f "${dir}/${base}.sha256" ]]; then
		(cd "$dir" && sha256sum -c "${base}.sha256" >/dev/null 2>&1) || return 1
		return 0
	fi
	if [[ -f "${dir}/SHA256SUMS.txt" ]] && grep -q "${base}" "${dir}/SHA256SUMS.txt" 2>/dev/null; then
		(cd "$dir" && grep " ${base}\$" SHA256SUMS.txt | sha256sum -c - >/dev/null 2>&1) || return 1
		return 0
	fi
	return 2 # no checksum available
}

verify_rc=0
verify_archive "$ARCHIVE" || verify_rc=$?
case "$verify_rc" in
0) echo "Checksum verified: ${SRC_BASE}" ;;
1)
	echo "Checksum mismatch for ${SRC_BASE}, refusing to restore." >&2
	exit 1
	;;
*) echo "No checksum sidecar for ${SRC_BASE}, skipping integrity check" ;;
esac

if [[ "$DRY_RUN" -eq 1 ]]; then
	echo "Dry-run restore plan"
	echo "  Prosody data: ${ARCHIVE}"
	echo "  Will replace /snikket in container ${SNIKKETX_CONTAINER_SERVER}"
	if [[ "$FULL" -eq 1 ]]; then
		echo "  Full restore enabled"
		for f in \
			"${SRC_DIR}/portal-data-${stamp}.tar.gz" \
			"${SRC_DIR}/traefik-data-${stamp}.tar.gz" \
			"${SRC_DIR}/updater-data-${stamp}.tar.gz" \
			"${SRC_DIR}/backup-data-${stamp}.tar.gz"; do
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

if docker container inspect "$SNIKKETX_CONTAINER_SERVER" >/dev/null 2>&1; then
	running=$(docker inspect -f '{{.State.Running}}' "$SNIKKETX_CONTAINER_SERVER" 2>/dev/null || echo false)
	if [[ "$running" == "true" ]]; then
		echo "Stopping ${SNIKKETX_CONTAINER_SERVER} container for restore ..."
		docker stop "$SNIKKETX_CONTAINER_SERVER" >/dev/null
	fi
else
	echo "Container ${SNIKKETX_CONTAINER_SERVER} does not exist. Start the stack once so the volume is attached, then re-run restore." >&2
	exit 1
fi

if [[ "$YES" -ne 1 ]]; then
	echo "WARNING: This will replace all data currently under /snikket in the ${SNIKKETX_CONTAINER_SERVER} container"
	echo "         with the contents of the provided backup. Existing data will be lost."
	echo -n "Continue? [y/N] "
	read -r -n1 continue_answer
	echo ""
	case "$continue_answer" in
	y | Y) echo "Ok, proceeding..." ;;
	*)
		echo "Aborting."
		exit 1
		;;
	esac
fi

ALPINE="$SNIKKETX_HELPER_IMAGE"

docker run --rm --volumes-from="$SNIKKETX_CONTAINER_SERVER" \
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
	for side in portal-data traefik-data updater-data backup-data; do
		tb="${SRC_DIR}/${side}-${stamp}.tar.gz"
		if [[ "$side" == "traefik-data" && ! -f "$tb" ]]; then
			# Pre-Traefik backups used the ravenguard-data label.
			tb="${SRC_DIR}/ravenguard-data-${stamp}.tar.gz"
		fi
		if [[ -f "$tb" ]]; then
			verify_rc=0
			verify_archive "$tb" || verify_rc=$?
			if [[ "$verify_rc" -eq 1 ]]; then
				echo "Checksum mismatch for $(basename "$tb"), refusing to restore." >&2
				exit 1
			fi
		fi
		case "$side" in
		portal-data) restore_vol "$tb" "$SNIKKETX_VOL_PORTAL_DATA" ;;
		traefik-data) restore_vol "$tb" "$SNIKKETX_VOL_TRAEFIK_DATA" ;;
		updater-data) restore_vol "$tb" "$SNIKKETX_VOL_UPDATER_DATA" ;;
		backup-data) restore_vol "$tb" "$SNIKKETX_VOL_BACKUP_DATA" ;;
		esac
	done
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
