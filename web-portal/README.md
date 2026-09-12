# web-portal

SnikketX account and admin portal for the snikketx monorepo. Single Go binary.
Image: `ghcr.io/sudo-ivan/snikketx/web-portal`.

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

## Sign-in second factors

Accounts can enroll passkeys (WebAuthn) and a TOTP authenticator app under
`/user/security`. When either is configured, the password grant still runs
first and the issued Prosody token is held in a pending session until the
second factor passes at `/login/verify` (TOTP code or passkey assertion). A
passkey assertion with user verification satisfies the second factor even
when TOTP is also enabled.

State lives in `SNIKKET_WEB_STATE_DIR/credentials.json`; TOTP secrets are
sealed with AES-GCM keyed by `SNIKKET_WEB_SECRET_KEY`. The WebAuthn relying
party id is derived from the request host, so enroll through the hostname the
portal is actually served on.
