# snikketx

Fork of the Snikket service stack as one repo: Prosody server image, web
portal, nginx proxy, and cert manager.

Upstream lives at [snikket-im](https://github.com/snikket-im). This fork
publishes images to GHCR under `ghcr.io/sudo-ivan/snikketx/`.

## Layout

- `server/` - Prosody-based Snikket server image
- `web-portal/` - account and admin web UI
- `web-proxy/` - nginx front door
- `cert-manager/` - Let's Encrypt renewal
- `scripts/` - install and ops helpers
- `docker-compose.yml` - pull and run published images

## Run published images

1. Point DNS at this host (A/AAAA for your Snikket domain).
2. Install Docker with the Compose plugin.
3. Clone this repo, then configure:

```
./scripts/init.sh
./scripts/start.sh
```

Create an admin invite:

```
./scripts/new-invite.sh --admin --group default
```

## Build locally

```
make docker
```

Or with Compose:

```
docker compose -f docker-compose.yml -f docker-compose.dev.yml build
```

Images land as `ghcr.io/sudo-ivan/snikketx/{server,web-portal,web-proxy,cert-manager}`.

## License

Each package keeps its upstream license file. See `server/LICENSE`,
`web-portal/LICENSE`, and `web-proxy/LICENSE`.
