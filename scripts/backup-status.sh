#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

endpoint="${SNIKKET_BACKUP_ENDPOINT:-http://127.0.0.1:9292}"
token="${SNIKKET_BACKUP_TOKEN:-snikket-backup-local}"

if ! curl -fsS --max-time 3 "${endpoint}/healthz" >/dev/null 2>&1; then
	echo "Backup service not reachable at ${endpoint}" >&2
	exit 1
fi

curl -fsS -H "Authorization: Bearer ${token}" "${endpoint}/v1/status" | python3 -m json.tool 2>/dev/null \
	|| curl -fsS -H "Authorization: Bearer ${token}" "${endpoint}/v1/status"
echo
