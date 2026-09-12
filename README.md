# snikketx

Fork of the Snikket stack as SnikketX in one repo: Prosody server image, web
portal, cert manager, Android client, plus RavenGuard on the HTTP edge.

Upstream lives at [snikket-im](https://github.com/snikket-im). This fork
publishes SnikketX images to GHCR under `ghcr.io/sudo-ivan/snikketx/`.

![SnikketX admin dashboard in dark mode](docs/images/portal-dashboard-dark.png)

## Quick start

### Local development

```
make init-dev
make up-dev
```

Open http://chat.localhost:8080/login with `admin@chat.localhost` / `admin`.
Put `chat.localhost` in `/etc/hosts` as `127.0.0.1` if needed. HTTP edge is port **8080**.

### New production host

```
./scripts/init.sh
./scripts/preflight.sh
make up
./scripts/new-invite.sh --admin --group default
```

DNS must point the domain plus `share.` and `groups.` at this host. Ports **80/443** (HTTP), **5222/5269** (XMPP), **3478/5349** (STUN/TURN).

### Migrate from classic Snikket

Works with the stock docker compose install from
<https://snikket.org/service/quickstart/> — a directory like `/etc/snikket`
holding `docker-compose.yml` and `snikket.conf`. Keeps Prosody data
(accounts, MAM chats, MUCs, uploads). A full backup runs first, always.

```
make migrate FROM=/etc/snikket BACKUP_DIR=/var/backups/snikketx
```

What it does:

1. Tars `/snikket` out of the running classic `snikket` container and stores
   the classic `snikket.conf` and compose file in the backup dir.
2. Detects the classic data volume from the container's mounts (named volume
   or bind mount), before `docker compose down` removes the containers.
   Directory name and `COMPOSE_PROJECT_NAME` do not matter.
3. Stops the classic stack and removes leftover classic containers so the
   shared `snikket` container name is free.
4. Writes `deploy/migrate/docker-compose.migrate.yml` so the `snikket_data`
   volume points at the classic data, copies `snikket.conf`, and merges the
   required keys into `.env` (existing keys like `SNIKKETX_IMAGE_TAG` or
   Restic credentials are preserved).
5. Runs preflight and starts SnikketX.
6. Records rollback state in `deploy/migrate/state.env`.

Both `docker compose` (plugin) and `docker-compose` (v1 binary) are
supported. Add `MIGRATE_FLAGS=--yes` to skip the confirmation prompt.

After start, log in at `https://<domain>/` with your existing admin XMPP
account. The first HTTPS request can take a minute while RavenGuard finishes
ACME issuance. `make invite` still works for new users.

Keep the classic directory until you trust the new stack. If migration fails:

```
make rollback FROM=/etc/snikket BACKUP_DIR=/var/backups/snikketx
```

`BACKUP_DIR` may point at a `snikketx-backup-*` dir or its parent; the newest
usable backup is picked automatically. Without flags, rollback falls back to
`deploy/migrate/state.env`.

## Day-2 commands

| Command | Purpose |
|---------|---------|
| `make status` | Compose ps + login HTTP probe (dev / `:8080`) |
| `make status-prod` | Same probe against prod compose / HTTPS |
| `make logs` | Follow compose logs (dev) |
| `make logs-prod` | Follow compose logs (prod) |
| `make admin` | Create/reset local admin (dev password `admin`) |
| `make invite` | Admin invite link |
| `make preflight` | Docker, DNS, ports, firewall hints |
| `make backup DEST=/abs/dir` | Full backup (data + config + SnikketX volumes) |
| `make backup-status` | Backup service status when the sidecar is running |
| `make restore ARCHIVE=/abs/snikket-data-....tar.gz` | Restore Prosody `/snikket` (`RESTORE_FLAGS=--full --dry-run` supported) |
| `make down` / `make down-dev` | Stop stack |
| `make screenshot` | Refresh README dark dashboard shot |

Config files:

- `snikket.conf` — domain, admin email, LE TOS (from `snikket.conf.example`)
- `.env` — RavenGuard/updater secrets for Compose (from `.env.example` or `init`)

## Major changes from upstream

- One monorepo instead of separate Snikket packages (server, portal, cert-manager).
- HTTP edge is RavenGuard alone (TLS via ACME in prod). Traefik and the legacy nginx `web-proxy/` tree are not part of the default stack or publish pipeline.
- Server and cert-manager images build on Alpine 3.24. Prosody comes from apk (13.x), not Debian nightlies.
- Web portal is a stdlib Go single binary on distroless, not the upstream Python/Quart app, with a refreshed dark-mode admin panel (footer shows build metadata, uptime, and Healthy / Degraded / Down).
- Optional self-hosted Android APK on the portal (`Admin → Apps`, public `/download/android.apk`) with cache refresh, source override for forks, and per-IP download limits.
- Backup sidecar schedules local archives, retention, dry-run restore checks, and optional Restic offsite (`Admin → Backup`).
- Invite helpers use `prosodyctl shell invite` (create_account / create_reset). The old `mod_invites generate` path is gone.
- Publish pipeline signs images keyless with Cosign, attaches Syft SPDX SBOMs, runs Trivy and container smoke tests. Production compose requires Cosign verification before applying updates.

HTTP path:

```
Client -> RavenGuard (ACME TLS) -> web-portal
                                \-> Prosody HTTP (upload, BOSH, websocket)
XMPP/STUN/TURN -> snikket_server (direct ports)
```

RavenGuard runs as the first hop with `trust.mode = edge` and Let's Encrypt
(`tls.mode = acme`, TLS-ALPN-01). Path routes for Prosody and the certbot
HTTP-01 webroot are seeded into the RavenGuard admin store by
`scripts/seed-ravenguard-routes.sh`. The JS challenge stays off so XMPP
WebSocket clients are not blocked. See the
[intro](https://ravenguard.quad4.io/docs/intro) and
[configuration](https://ravenguard.quad4.io/docs/configuration) docs.

## Backup scope

`make backup DEST=/abs/dir` or the `snikket_backup` sidecar writes a timestamped directory with:

- `snikket-data-*.tar.gz` — entire `/snikket` volume (accounts, message archives, group chats, uploads, XMPP LE material)
- copies of `snikket.conf` and `.env`
- SnikketX-only volumes when present: portal, RavenGuard, updater, backup state
- optional Restic push to an S3-compatible (or other) repository

Configure schedule, retention, and offsite under **Admin → Backup**. Use `./scripts/restore.sh … --dry-run` to validate an archive without writing.

Classic Snikket used the same `/snikket` layout and container name `snikket`, so the data tarball is compatible for migrate/rollback.

## Layout

- `server/` - Prosody-based SnikketX server image
- `web-portal/` - account and admin web UI
- `android-app/` - SnikketX Android client (forked from snikket-android / Conversations)
- `updater/` - container update service with Cosign verify
- `backup/` - scheduled backup / retention / Restic sidecar
- `web-proxy/` - legacy nginx front door (archive only, not published)
- `cert-manager/` - Let's Encrypt for XMPP TLS (prod)
- `deploy/ravenguard/` - RavenGuard TOML and blocklists
- `deploy/migrate/` - volume override + rollback state written by migrate script
- `scripts/` - install and ops helpers
- `docker-compose.yml` - production stack (pull GHCR + RavenGuard edge)
- `docker-compose.dev.yml` - local builds, no certs package

## Build images only

```
make docker
```

Images land as `ghcr.io/sudo-ivan/snikketx/{server,web-portal,cert-manager,updater,backup}`.

Publish tags each image with mutable tags (`latest`, `dev`, `sha-<commit>`) and
records the immutable digest (`ghcr.io/sudo-ivan/snikketx/<component>@sha256:...`)
as a workflow artifact and job summary. Images are signed keyless with Cosign
(Sigstore) and get a Syft SPDX SBOM. CI also runs container smoke tests and
Trivy scans.

Verify a published digest:

```
cosign verify \
  --certificate-identity-regexp='https://github.com/Sudo-Ivan/snikketx/.*' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  ghcr.io/sudo-ivan/snikketx/server@sha256:<digest>
```

## License

Each package keeps its upstream license file. See `server/LICENSE` and
`web-portal/LICENSE`. The legacy `web-proxy/LICENSE` remains with that archive tree.
