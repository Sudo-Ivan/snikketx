# Bot framework

SnikketX can host virtual bot identities on a dedicated component
(`name@bots.DOMAIN`). Bots need no account, no password and no XMPP
library. Bot runtimes receive XMPP traffic as JSON over a webhook, an
SSE stream or a WebSocket, and act through a small REST API.

The feature is disabled by default. Enable it with:

```
SNIKKET_TWEAK_BOTS=1
```

This adds a component:

```lua
Component ("bots."..DOMAIN) "snikketx_bots"
    bot_muc_hosts = { "groups."..DOMAIN }
```

Optionally set a static management token for provisioning tooling:

```
SNIKKET_TWEAK_BOTS_ADMIN_TOKEN=some-long-random-string
```

The API lives at `/bots` on the component host and is reachable through
the internal HTTP port (default 5280). Keep it behind trusted networks
or an authenticating reverse proxy. Admins and owners authenticate with
Bearer tokens; bot runtimes get scoped `sxb_` tokens minted per bot.

## Web portal self service

The web portal exposes a **Bots** page where each user manages the bots
owned by their account: create and delete bots, enable or disable them
and mint or revoke `sxb_` tokens. Token secrets are shown once and are
never stored by the portal.

To enable it, set the same admin token on the server and the portal:

```
SNIKKET_TWEAK_BOTS_ADMIN_TOKEN=long-random-secret
SNIKKET_WEB_BOTS_ADMIN_TOKEN=long-random-secret
```

The portal calls the management API at
`{SNIKKET_WEB_PROSODY_ENDPOINT}/bots`; override with
`SNIKKET_WEB_BOTS_ENDPOINT` when the layout differs. The admin token is
powerful: it can manage every bot, so keep it secret and rotate it.

## Capabilities

- Bots can join MUC rooms, send and receive direct messages, groupchat
  messages and room PMs, set presence and read occupant lists.
- Bots auto-accept room config on creation and rejoin their rooms after
  a server restart.
- Inbound traffic becomes JSON events: messages, presence, invites,
  kicks, bans and subscription requests.
- Webhook delivery is signed (`X-Bots-Signature`, HMAC-SHA-256),
  retried with backoff and dead-lettered, and the target URL is checked
  against SSRF (private IPs blocked by default, optional host
  allowlist).

## Usage

See the module README at
`server/snikket-modules/mod_snikketx_bots/README.md` for the full API,
event schema, scope model and configuration reference. An example
Python echo bot lives in `examples/echo_bot.py` inside the same
directory.

Shell commands are available for operators:

```
prosodyctl shell
> bots:create bots.example.com alice@example.com mybot
> bots:list bots.example.com
> bots:token bots.example.com mybot
> bots:delete bots.example.com mybot
```
