#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

DEST=""
FULL=1

usage() {
	cat <<'EOF'
Usage: ./scripts/backup.sh /absolute/destination/dir [--data-only]

Creates a backup directory containing:
  snikket-data-TIMESTAMP.tar.gz  Prosody /snikket (accounts, MAM chats, MUCs, uploads, XMPP certs)
  snikket.conf / .env            Host config copies when present
  portal-data-*.tar.gz           Portal state (SnikketX, when volume exists)
  ravenguard-data-*.tar.gz       RavenGuard ACME/admin (when volume exists)
  updater-data-*.tar.gz          Updater state (when volume exists)
  MANIFEST.txt

--data-only   Only archive /snikket from container snikket (classic-compatible)
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	-h|--help)
		usage
		exit 0
		;;
	--data-only)
		FULL=0
		shift
		;;
	/*)
		DEST="$1"
		shift
		;;
	*)
		echo "Unexpected argument: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

if [[ -z "$DEST" ]]; then
	usage >&2
	exit 1
fi

if [[ ! -d "$DEST" ]]; then
	echo "Destination directory does not exist: $DEST" >&2
	exit 1
fi

STAMP=$(date +%F-%H%M%S)
OUT="${DEST%/}/snikketx-backup-${STAMP}"
mkdir -p "$OUT"

ALPINE="alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"

if ! docker container inspect snikket >/dev/null 2>&1; then
	echo "Container 'snikket' not found. Start the stack (or classic Snikket) first." >&2
	exit 1
fi

echo "Backing up /snikket (accounts, chats/MAM, MUCs, uploads) ..."
docker run --rm --volumes-from=snikket -v "$OUT":/backup "$ALPINE" \
	tar czf "/backup/snikket-data-${STAMP}.tar.gz" /snikket

if [[ -f snikket.conf ]]; then
	cp -a snikket.conf "$OUT/snikket.conf"
fi
if [[ -f .env ]]; then
	cp -a .env "$OUT/env"
fi

archive_named_volume() {
	local vol="$1"
	local label="$2"
	if ! docker volume inspect "$vol" >/dev/null 2>&1; then
		return 0
	fi
	echo "Backing up volume ${vol} ..."
	docker run --rm -v "$vol":/data:ro -v "$OUT":/backup "$ALPINE" \
		tar czf "/backup/${label}-${STAMP}.tar.gz" -C / data
}

MODE=classic
if docker volume inspect snikketx_portal_data >/dev/null 2>&1 || docker volume inspect snikketx_ravenguard_data >/dev/null 2>&1; then
	MODE=snikketx
fi

if [[ "$FULL" -eq 1 ]]; then
	archive_named_volume snikketx_portal_data portal-data
	archive_named_volume snikketx_ravenguard_data ravenguard-data
	archive_named_volume snikketx_updater_data updater-data
	# Classic project name fallback
	archive_named_volume snikket_portal_data portal-data-classic
fi

{
	echo "SnikketX backup ${STAMP}"
	echo "mode=${MODE}"
	echo "full=${FULL}"
	echo "host=$(hostname 2>/dev/null || true)"
	echo "domain=$(snikketx_domain 2>/dev/null || true)"
	echo "created=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	echo "contents:"
	ls -1 "$OUT"
} > "$OUT/MANIFEST.txt"

echo ""
echo "Backup written to ${OUT}"
echo "Prosody data includes accounts, message archives (MAM), group chats (MUCs), and uploads."
