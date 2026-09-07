#!/bin/bash

set -eo pipefail

if [[ "$#" != "1" ]]; then
	echo "Please supply the name of a channel to switch to"
	exit 1;
fi

case "$1" in
latest|dev|stable) ;;
*)
	echo "Invalid channel name: $1"
	echo "Choose from: latest, dev, stable"
	exit 1;
;;
esac

exec sed -i 's|^\( *image: ghcr.io/sudo-ivan/snikketx/[^:]*\):.*$|\1:'"$1"'|' docker-compose.yml
