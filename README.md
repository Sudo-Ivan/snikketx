# snikketx

Fork of the Snikket stack as SnikketX (eXtended) in one repo: Prosody server image, web
portal, cert manager, Android client, plus Traefik on the HTTP edge.

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
./scripts/setup.sh
```

The setup wizard runs everything: base config, optional migration from
classic Snikket, optional OIDC/LDAP auth, DNS records, certificate
mode, preflight, start, firewall and postcheck. Preview it first with
`./scripts/setup.sh --dry-run`.

Or drive each step manually:

```
./scripts/init.sh
./scripts/preflight.sh
make up
./scripts/new-invite.sh --admin --group default
```

DNS must point the domain plus `share.` and `groups.` at this host.
Ports **80+443** TCP and **443** UDP (HTTP and HTTP/3), **5222/5223/5269**
(XMPP), **3478/5349** TCP and UDP (STUN/TURN), and the UDP relay range
**49152-49251** for calls. On Cloudflare this can be set up automatically,
see [DNS setup with Cloudflare](#dns-setup-with-cloudflare).

### Migrate from classic Snikket

Works with the stock docker compose install from
<https://snikket.org/service/quickstart/>, a directory like `/etc/snikket`
holding `docker-compose.yml` and `snikket.conf`. Keeps Prosody data
(accounts, MAM chats, MUCs, uploads). A full backup runs first, always.

```
make migrate FROM=/etc/snikket BACKUP_DIR=/var/backups/snikketx
```

What it does:

1. Tars `/snikket` out of the running classic `snikket` container and stores
   the classic `snikket.conf` and compose file in the backup dir, with
   SHA-256 sidecars for every archive.
2. Detects the classic data volume from the container's mounts (named volume
   or bind mount), before `docker compose down` removes the containers.
   Directory name and `COMPOSE_PROJECT_NAME` do not matter.
3. Stops the classic stack.
4. Copies the classic data into the `snikketx_snikket_data` volume, so the
   classic volume or bind mount stays byte-for-byte intact for rollback.
   Pass `--share` to mount the classic data directly instead (the old
   behaviour). Copies `snikket.conf` and merges the required keys into
   `.env` (existing keys like `SNIKKETX_IMAGE_TAG` or Restic credentials
   are preserved).
5. Runs preflight and starts SnikketX.
6. Records rollback state in `deploy/migrate/state.env`.

All SnikketX containers are named `snikketx-*` and all volumes are
`snikketx_*`, so nothing collides with the classic stack's `snikket*`
names. Rolling back is `make down` in the SnikketX checkout followed by
`docker compose up -d` in the classic directory.

Both `docker compose` (plugin) and `docker-compose` (v1 binary) are
supported. Add `MIGRATE_FLAGS=--yes` to skip the confirmation prompt.

After start, log in at `https://<domain>/` with your existing admin XMPP
account. The first HTTPS request can take a minute while Traefik finishes
ACME issuance. `make invite` still works for new users.

Keep the classic directory until you trust the new stack. If migration fails:

```
make rollback FROM=/etc/snikket BACKUP_DIR=/var/backups/snikketx
```

`BACKUP_DIR` may point at a `snikketx-backup-*` dir or its parent. The newest
usable backup is picked automatically. Without flags, rollback falls back to
`deploy/migrate/state.env`.

## DNS setup with Cloudflare

`scripts/dns-setup.py` creates and verifies every record SnikketX needs on
Cloudflare. It only ever touches records inside the XMPP domain subtree
(for `chat.example.com` that is the name itself, `share.`/`groups.` and the
`_xmpp-*._tcp` SRV names). It never deletes records, and it writes a full
backup of the zone before changing anything.

Create a scoped API token at <https://dash.cloudflare.com/profile/api-tokens>
with `Zone:Read` and `DNS:Edit` on your zone only, then:

```
./scripts/dns-setup.py --domain chat.example.com --token <token>
```

It prints each step, shows the plan (keep / create / update per record),
asks before writing, and verifies the result through the API. `--dry-run`
shows the plan without writing. `--ipv4` / `--ipv6` override autodetected
addresses and `--no-caa` skips the CAA `letsencrypt.org` records.

Backups land in the repo root as `dns-backup-<zone>-<timestamp>.json` and
`.zone`. Restore by hand from the zone file if needed.

## Optional DNS-01 certificates

By default HTTPS certificates come from Let's Encrypt via TLS-ALPN-01 at
the edge and HTTP-01 for XMPP. If DNS is hosted on Cloudflare, DNS-01
covers both and works even when port 80 is not reachable. To switch:

```
./scripts/enable-dns-acme.sh
```

It verifies the token, locates the zone, writes `CF_DNS_API_TOKEN` to
`.env`, activates `deploy/acme-dns/docker-compose.acme-dns.yml`, and
restarts Traefik and cert-manager. Existing certificates stay valid and
renew through DNS-01 from then on.

To switch back to TLS-ALPN-01:

```
rm deploy/acme-dns/docker-compose.acme-dns.yml
./scripts/update.sh
```

Then remove `CF_DNS_API_TOKEN` from `.env`.

## Other Commands

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
| `make update` | git pull + image pull + restart + post-check (`UPDATE_FLAGS=--no-git`) |
| `make postcheck` | Containers, edge probe, external domain verification |
| `make verify` | DNS/TLS/DANE/port verifier only (`scripts/verify-domain.py`) |
| `make backup-status` | Backup service status when the sidecar is running |
| `make restore ARCHIVE=/abs/snikket-data-....tar.gz` | Restore Prosody `/snikket` (`RESTORE_FLAGS=--full --dry-run` supported) |
| `make down` / `make down-dev` | Stop stack |
| `make screenshot` | Refresh README dark dashboard shot |

Config files:

- `snikket.conf`: domain, admin email, LE TOS (from `snikket.conf.example`)
- `.env`: updater/backup secrets for Compose (from `.env.example` or `init`)

## Major changes from upstream

- One monorepo instead of separate Snikket packages (server, portal, cert-manager).
- HTTP edge is Traefik (TLS via ACME in prod). The legacy nginx `web-proxy/` tree is not part of the default stack or publish pipeline.
- Server and cert-manager images build on Alpine 3.24. Prosody comes from apk (13.x), not Debian nightlies.
- Web portal is a stdlib Go single binary on distroless, not the upstream Python/Quart app, with a refreshed dark-mode admin panel (footer shows build metadata, uptime, and Healthy / Degraded / Down).
- Optional self-hosted Android APK on the portal (`Admin -> Apps`, public `/download/android.apk`) with cache refresh, source override for forks, and per-IP download limits.
- Backup sidecar schedules local archives, retention, dry-run restore checks, and optional Restic offsite (`Admin -> Backup`).
- Invite helpers use `prosodyctl shell invite` (create_account / create_reset). The old `mod_invites generate` path is gone.
- Publish pipeline signs images keyless with Cosign, attaches Syft SPDX SBOMs, runs Trivy and container smoke tests. Production compose requires Cosign verification before applying updates.
- Security additions: SASL SCRAM downgrade protection (XEP-0474 via mod_sasl_ssdp), XEP-0424 tombstoning of retracted 1:1 archive entries, XEP-0334 no-store hints in MUC archives, MUC log retention matching RETENTION_DAYS, a published XEP-0504 data policy form, explicit push payload minimization, and opt-in SCRAM-SHA-256 password storage (SNIKKET_TWEAK_PASSWORD_HASH, new installs only).
- Android privacy additions: optional biometric or device-credential app lock with configurable auto-lock timeout, per-conversation disappearing messages (XEP-0466), EXIF/GPS metadata stripped from image uploads including send-as-file, OMEMO keys and history databases excluded from cloud backup, incognito keyboard flag on message input, and a signed APK SHA-256 plus signing certificate fingerprint published at `/download/android.apk.sha256` for AppVerifier checks.

HTTP path:

```
Client -> Traefik (ACME TLS) -> web-portal
                              \-> Prosody HTTP (upload, BOSH, websocket)
                              \-> acme_webroot (certbot HTTP-01)
XMPP/STUN/TURN -> snikket_server (direct ports)
```

Traefik runs as the first hop and terminates TLS. Certificates come from
Let's Encrypt via TLS-ALPN-01 and are stored in the `traefik_data` volume.
Routes live in `deploy/traefik/dynamic.yml`, rendered by
`scripts/render-edge.sh` from `snikket.conf`, and the file provider
hot-reloads them. Prosody HTTP paths (upload, BOSH, websocket, invite APIs)
go to the server container and the certbot `/.well-known/acme-challenge`
path goes to `acme_webroot` so `snikket_certs` can keep issuing XMPP certs.

## Backup scope

`make backup DEST=/abs/dir` or the `snikket_backup` sidecar writes a timestamped directory with:

- `snikket-data-*.tar.gz`: entire `/snikket` volume (accounts, message archives, group chats, uploads, XMPP LE material)
- copies of `snikket.conf` and `.env`
- SnikketX-only volumes when present: portal, Traefik, updater, backup state
- optional Restic push to an S3-compatible (or other) repository

Configure schedule, retention, and offsite under **Admin -> Backup**. Use `./scripts/restore.sh ... --dry-run` to validate an archive without writing.

Classic Snikket uses the same `/snikket` layout, so the data tarball is compatible for migrate/rollback. Every archive gets a `.sha256` sidecar plus an aggregate `SHA256SUMS.txt`, verified automatically by `restore.sh`.

## Layout

- `server/` - Prosody-based SnikketX server image
- `web-portal/` - account and admin web UI
- `android-app/` - SnikketX Android client (forked from snikket-android / Conversations)
- `updater/` - container update service with Cosign verify
- `backup/` - scheduled backup / retention / Restic sidecar
- `web-proxy/` - legacy nginx front door (archive only, not published)
- `cert-manager/` - Let's Encrypt for XMPP TLS (prod)
- `deploy/traefik/` - Traefik static config and rendered dynamic routes
- `deploy/migrate/` - volume override + rollback state written by migrate script
- `scripts/` - install and ops helpers
- `docker-compose.yml` - production stack (pull GHCR + Traefik edge)
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
