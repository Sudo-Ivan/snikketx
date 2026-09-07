# snikketx

My Sloppulus fork of the Snikket stack as SnikketX in one repo: Prosody server image, web
portal, cert manager, plus Traefik and RavenGuard on the HTTP edge.

Upstream lives at [snikket-im](https://github.com/snikket-im). This fork
publishes SnikketX images to GHCR under `ghcr.io/sudo-ivan/snikketx/`.

## Major changes from upstream

- One monorepo instead of separate Snikket packages (server, portal, cert-manager, proxy).
- HTTP edge is Traefik plus RavenGuard. The nginx web-proxy image is kept but not used by default compose.
- Server, cert-manager, and web-proxy images build on Alpine 3.24. Prosody comes from apk (13.x), not Debian nightlies.
- Web portal is a stdlib Go single binary on distroless, not the upstream Python/Quart app.
- Invite helpers use `prosodyctl shell invite` (create_account / create_reset). The old `mod_invites generate` path is gone.
- Publish pipeline signs images keyless with Cosign, attaches Syft SPDX SBOMs, runs Trivy and container smoke tests.

HTTP path:

```
Client -> Traefik -> RavenGuard -> web-portal
                 \-> Prosody HTTP (upload, BOSH, websocket)
XMPP/STUN/TURN -> snikket_server (direct ports)
```

RavenGuard runs behind Traefik with `trust.mode = behind_proxy`. See the
[intro](https://ravenguard.quad4.io/docs/intro) and
[configuration](https://ravenguard.quad4.io/docs/configuration) docs.

## Layout

- `server/` - Prosody-based SnikketX server image
- `web-portal/` - account and admin web UI
- `web-proxy/` - legacy nginx front door (not used by default compose)
- `cert-manager/` - Let's Encrypt for XMPP TLS (prod)
- `deploy/traefik/` - Traefik static and generated dynamic config
- `deploy/ravenguard/` - RavenGuard TOML and blocklists
- `scripts/` - install and ops helpers
- `docker-compose.yml` - production stack (pull GHCR + Traefik + RavenGuard)
- `docker-compose.dev.yml` - local builds, no proxy/certs package

## Production

1. Point DNS at this host (A/AAAA for the SnikketX domain, plus share/groups).
2. Install Docker with the Compose plugin.
3. Configure and start:

```
./scripts/init.sh
./scripts/start.sh
```

Create an admin invite:

```
./scripts/new-invite.sh --admin --group default
```

## Local development

Builds `server` and `web-portal` from this tree. Skips GHCR pulls for those
images, skips cert-manager and the legacy web-proxy, and uses self-signed
certs so Prosody can start.

```
./scripts/init.sh
./scripts/start.sh --dev
```

HTTP is on port 8080. Point the Host header or `/etc/hosts` at your
`SNIKKET_DOMAIN`. XMPP still publishes 5222 and friends on the host.

## Build images only

```
make docker
```

Images land as `ghcr.io/sudo-ivan/snikketx/{server,web-portal,web-proxy,cert-manager}`.

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

Each package keeps its upstream license file. See `server/LICENSE`,
`web-portal/LICENSE`, and `web-proxy/LICENSE`.
