#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

echo "Renewing XMPP certificates..."
docker exec "$SNIKKETX_CONTAINER_CERTS" /etc/cron.daily/certbot

echo "Reloading Prosody..."
docker exec "$SNIKKETX_CONTAINER_SERVER" supervisorctl signal hup prosody

echo "HTTP TLS is managed by RavenGuard ACME. No nginx proxy reload is required."
echo "Complete."
