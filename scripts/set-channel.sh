#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

if [[ "$#" != "1" ]]; then
	echo "Please supply the name of a channel to switch to"
	exit 1
fi

case "$1" in
latest | dev | stable) ;;
*)
	echo "Invalid channel name: $1"
	echo "Choose from: latest, dev, stable"
	exit 1
	;;
esac

if [[ -f .env ]] && grep -q '^SNIKKETX_IMAGE_TAG=' .env; then
	sed -i 's|^SNIKKETX_IMAGE_TAG=.*$|SNIKKETX_IMAGE_TAG='"$1"'|' .env
else
	printf 'SNIKKETX_IMAGE_TAG=%s\n' "$1" >>.env
fi

echo "Set SNIKKETX_IMAGE_TAG=$1 in .env. Run ./scripts/update.sh to apply."
