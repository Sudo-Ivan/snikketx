# web-portal

SnikketX account and admin portal for the snikketx monorepo. Single Go binary,
stdlib only. Image: `ghcr.io/sudo-ivan/snikketx/web-portal`.

## Development

```
cd web-portal
cp example.env .env
# export vars from .env, or use your shell dotenv
make build-offline
./bin/portal
```

Required env: `SNIKKET_WEB_SECRET_KEY` (at least 32 bytes, or writable secret file),
`SNIKKET_WEB_PROSODY_ENDPOINT`, `SNIKKET_WEB_DOMAIN`. See `example.env`.

For plain HTTP local use set `SNIKKET_WEB_INSECURE_COOKIES=true`.
Scrapes of `/metrics` need `Authorization: Bearer $SNIKKET_WEB_METRICS_TOKEN`.

Refresh Lucide icons (needs network): `make icons`.

Checks: `make check` runs fmt, fix, vet, gosec, race, and tests.
Fuzz: `make fuzz`. Bench: `make bench`.

## Endpoints

- `/_health` — liveness (RavenGuard)
- `/_health/ready` — Prosody reachability
- `/metrics` — Prometheus text (optional `SNIKKET_WEB_METRICS_TOKEN`)
- `/admin/` — admin panel with ops summary
- `/admin/health/` — component health and recent errors
