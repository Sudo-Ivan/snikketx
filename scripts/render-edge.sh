#!/bin/bash
# Deprecated: Traefik dynamic config is gone. Routes are seeded into RavenGuard.
exec "$(cd "$(dirname "$0")" && pwd)/seed-ravenguard-routes.sh" "${1:-prod}"
