#!/bin/bash

set -eo pipefail

echo "Renewing XMPP certificates..."
docker exec snikket-certs /etc/cron.daily/certbot

echo "Reloading Prosody..."
docker exec snikket supervisorctl signal hup prosody

echo "HTTP TLS is managed by RavenGuard ACME. No nginx proxy reload is required."
echo "Complete."
