# mod_snikketx_bots

Virtual bot identities for SnikketX. Bots are addressable XMPP entities
under a dedicated component host (`name@bots.DOMAIN`) that require no
account, no password and no XMPP client library. Bot runtimes talk to the
component over plain HTTP transports:

- Webhook push for inbound events plus REST actions for outbound traffic
- Server-Sent Events stream plus REST actions
- WebSocket for bidirectional JSON frames

Any language that can speak HTTP can host a bot.

## Quick start

Enable the component in prosody.cfg.lua (already wired behind
`SNIKKET_TWEAK_BOTS=1` in the SnikketX image):

```lua
Component ("bots."..DOMAIN) "snikketx_bots"
    bot_muc_hosts = { "groups."..DOMAIN }
    bot_admin_tokens = { "a-long-random-admin-token" }
```

The API is served on the component host at `/bots`. With the default
SnikketX HTTP listener, requests look like:

```sh
curl -H "Host: bots.example.com" \
     -H "Authorization: Bearer a-long-random-admin-token" \
     http://127.0.0.1:5280/bots
```

Put a reverse proxy or internal service in front of the internal HTTP
port as needed. The API is meant for operators and trusted services, not
for public internet exposure.

Create a bot and a token:

```sh
curl -X POST -H "Host: bots.example.com" \
     -H "Authorization: Bearer a-long-random-admin-token" \
     -H "Content-Type: application/json" \
     -d '{"name":"echo","owner":"alice@example.com"}' \
     http://127.0.0.1:5280/bots
```

The response includes a `token` field like `sxb_<id>_<secret>`. It is
shown exactly once; only its SHA-256 hash is stored. The bot now exists
as `echo@bots.example.com`.

Join a room and reply to everything:

```sh
curl -X POST -H "Host: bots.example.com" \
     -H "Authorization: Bearer sxb_..." \
     -H "Content-Type: application/json" \
     -d '{"room":"chat@groups.example.com","nick":"echo"}' \
     http://127.0.0.1:5280/bots/echo/join
```

## Architecture

```
+-----------------+        XMPP         +-------------------------+
|  user clients   | <-----------------> |  prosody localhost      |
+-----------------+                     +-------------------------+
                                                | stanzas
+-----------------+        XMPP         +-------v-----------------+
|   MUC component | <-----------------> |  bots.DOMAIN component  |
| groups.DOMAIN   |                     |  mod_snikketx_bots      |
+-----------------+                     +-------+-----------------+
                                                | JSON
                              +-----------------+----------------+
                              | webhooks  /  SSE  /  WebSocket    |
                              +-----------------+----------------+
                                                |
                                        +-------v-------+
                                        | bot runtimes  |
                                        +---------------+
```

Stanzas addressed to `name@bots.DOMAIN` are normalized into JSON events.
Actions from the API are turned into stanzas sent with the bot JID as
`from`. The API never accepts raw XML; every stanza is constructed by the
module, which keeps the injection surface small.

### Files

- `mod_snikketx_bots.lua` component core: stanza routing, MUC
  occupancy, action dispatch, lifecycle, shell commands
- `registry.lua` bot records, scoped tokens, persistence
- `events.lua` event normalization, sequence numbers, ring buffer
- `webhook.lua` webhook validation, queueing, retries, SSRF checks
- `ws.lua` WebSocket upgrade, framing, JSON protocol
- `httpapi.lua` HTTP routes, authentication, SSE transport
- `ratelimit.lua` token buckets

## Authentication

Three credential kinds, checked in this order:

1. `bot_admin_tokens` (config list): full management access. Use for
   provisioning systems.
2. tokenauth sessions: users authenticate with a token created by
   `prosodyctl mod_tokenauth` or the Snikket app token flow. A user can
   manage bots they own. Admins can manage all bots.
3. `sxb_` bot tokens: scoped runtime credentials for a single bot.

All endpoints require `Authorization: Bearer <token>`.

### Scopes

| Scope     | Allows                                            |
|-----------|---------------------------------------------------|
| `read`    | SSE/WS streams, status, rooms, occupants          |
| `write`   | send messages                                     |
| `rooms`   | join and leave rooms                              |
| `presence`| set availability in joined rooms                  |
| `manage`  | update own bot config, implies all other scopes   |

`manage` is intended for bot runtime self-service. Owners and admins are
not scope-limited.

### Token binding

Tokens can be locked to a client at mint time, so a leaked token is
useless from another machine or another runtime:

```json
{"scopes": ["read", "write"], "ips": ["203.0.113.10", "fd00::/8"],
 "ua": "mybot", "name": "prod runtime"}
```

- `ips`: up to 16 IPv4/IPv6 addresses or CIDR ranges. The request source
  IP must match one of them.
- `ua`: case-insensitive substring match against the `User-Agent`
  header.
- `name`: free-form label for audit readability.

A bound token used from a non-matching context gets `401`, an
`auth.deny` audit entry with `ip-not-bound` or `ua-not-bound`, and the
token id is logged so you can see exactly which credential leaked.

### Stream tickets

Browser `EventSource` and `WebSocket` cannot set an `Authorization`
header. `POST /bots/{name}/ticket` returns a single-use ticket valid for
`bot_ticket_ttl` seconds (default 60). Use it as `?ticket=...` on the
`events` or `ws` endpoints. Tickets grant `read` scope only, are
consumed on use, are bound to the minting request's source IP, and are
rate limited.

## HTTP API

Base path `/bots` on the component host.

### Management (owner session or admin token)

| Method | Path                      | Purpose                          |
|--------|---------------------------|----------------------------------|
| GET    | `/bots`                   | List bots (own, or all as admin) |
| POST   | `/bots`                   | Create bot, returns first token  |
| GET    | `/bots/{name}`            | Bot record plus runtime stats    |
| PATCH  | `/bots/{name}`            | Update label, disabled, events, webhook |
| DELETE | `/bots/{name}`            | Delete bot, leave all rooms      |
| POST   | `/bots/{name}/tokens`     | Mint token `{scopes, ttl, name, ips, ua}` |
| GET    | `/bots/{name}/tokens`     | List tokens (hashed, no secrets) |
| DELETE | `/bots/{name}/tokens/{id}`| Revoke token                     |
| GET    | `/bots/audit`             | Audit log (admin only)           |

`audit` is a reserved name and cannot be a bot.

Create body: `{"name", "owner"?, "label"?, "events"?, "webhook"?}`.
Non-admin callers are always the owner. `owner` is required for admin
callers. Names match `^[a-z0-9][a-z0-9._-]*$`, max 48 chars.

PATCH fields:

- `label` display name, also the default MUC nick
- `disabled` boolean, drops the bot from all rooms
- `events` map of event filters (see below)
- `webhook` `{"url", "secret"?}` or `null`/`false`/`{}` to remove
- `webhook_enabled` boolean to re-enable a circuit-broken webhook

### Runtime (bot token, owner, or admin)

| Method | Path                            | Purpose                    |
|--------|---------------------------------|----------------------------|
| GET    | `/bots/{name}/events`           | SSE stream                 |
| GET    | `/bots/{name}/ws`               | WebSocket stream           |
| POST   | `/bots/{name}/send`             | Send message               |
| POST   | `/bots/{name}/join`             | Join room                  |
| POST   | `/bots/{name}/leave`            | Leave room                 |
| POST   | `/bots/{name}/presence`         | Set show/status            |
| GET    | `/bots/{name}/rooms`            | List joined rooms          |
| GET    | `/bots/{name}/occupants?room=`  | Room occupants             |
| GET    | `/bots/{name}/status`           | Health and counters        |
| POST   | `/bots/{name}/ticket`           | Mint stream ticket         |

Errors are `{"error":"code"}` JSON with an appropriate status code.
`400` covers validation and policy failures, `401` missing or bad auth,
`403` scope or ownership violations, `404` unknown bot or path, `429`
rate limits.

## Actions

### send

```json
{
  "to": "chat@groups.example.com",
  "kind": "groupchat",
  "body": "hello",
  "subject": "...",
  "thread": "...",
  "reply_to": {"to": "...", "id": "stanza-id"},
  "reactions": {"id": "stanza-id", "emojis": ["+1"]},
  "chat_state": "composing",
  "stanza_id": "client-chosen-id",
  "omemo": {"xmlns": "urn:xmpp:omemo:2", "sid": "1", "iv": "...",
            "keys": [{"rid": "7", "key": "...", "prekey": true}],
            "payload": "..."}
}
```

Target policy:

- `groupchat` to a room JID requires the bot to be joined in that room
- `chat` to `room@groups.DOMAIN/nick` sends a room PM, same requirement
- `chat`/`normal` to a bare or full JID on the user host always works
- `chat` to another `name@bots.DOMAIN` works (bot-to-bot pipelines)
- other hosts need `bot_allowed_remote_hosts`

Returns `{"stanza_id": "..."}`.

### OMEMO

The module does not do encryption itself. Instead it passes OMEMO
payloads through in both directions, so a runtime that manages its own
key material can operate in OMEMO rooms:

- Inbound: an `<encrypted>` element surfaces as `encrypted: true`,
  `encryption: <eme namespace>` and an `omemo` object containing
  `xmlns`, `sid`, `iv`, `keys` and `payload`, verbatim.
- Outbound: set `omemo` on `send` and the module constructs the
  `<encrypted>` element plus a `store` archive hint and an EME
  (`urn:xmpp:eme:0`) marker.

Both `eu.siacs.conversations.axolotl` (OMEMO 1) and `urn:xmpp:omemo:2`
are accepted. Key management, sessions and device lists are the
runtime's problem; the module only shuttles opaque payloads.

### join

```json
{"room": "chat@groups.example.com", "nick": "optional", "password": "optional"}
```

The nick defaults to the bot label or name. Joins are asynchronous: the
call returns `{"pending": true}` and a `join` or `join_failed` event
follows. When the bot creates a room it auto-accepts the default
configuration so the room is not left locked.

### leave

```json
{"room": "chat@groups.example.com", "reason": "optional"}
```

### presence

```json
{"show": "away|xa|dnd|chat", "status": "text"}
```

Broadcasts availability to all joined rooms.

## Events

Every event is a JSON object. Common fields:

- `v` protocol version (1)
- `seq` per-bot sequence number, 1-based, strictly increasing
- `bot` bot name
- `type` event type
- `ts` unix timestamp

### Event types

| type              | Meaning                                       |
|-------------------|-----------------------------------------------|
| `message`         | `kind` is `dm`, `pm` or `groupchat`           |
| `reaction`        | XEP-0444 reaction to a message                |
| `presence`        | occupant presence in a joined room            |
| `join`            | bot joined a room (`created`, `renamed` flags)|
| `join_failed`     | join rejected, `condition` field              |
| `leave`           | bot left a room                               |
| `kicked`          | removed by a moderator (status 307)           |
| `banned`          | banned (status 301)                           |
| `removed`         | affiliation revoked or room closed            |
| `nick_changed`    | server-side nick change (status 303)          |
| `invite`          | direct or mediated room invite                |
| `subscribe_request`| a user sent a subscription request           |
| `error`           | stanza error reply                            |
| `deleted`         | final event before the bot is gone            |

Message events carry `from`, `body`, `thread`, `stanza_id`,
`origin_id`, `replace_id` (last message correction), `reply_to`,
`stamp` (delayed delivery), plus `room`, `nick` and `real_jid` for
groupchat and PM events where the room is non-anonymous.

### Event filters

`bot.events` controls which inbound traffic becomes events:

- `dm`, `pm`, `groupchat`: message classes, default on
- `presence`: occupant presence noise, default off
- `invites`: room invites, default on
- `subscribe_requests`: roster subscription attempts, default on
- `mentions_only`: only groupchat messages containing the bot nick,
  default off
- `echo_self`: deliver the bot's own reflected groupchat messages,
  default off
- `auto_accept_invites`: join on any invite from a local user, default
  off (config `bot_auto_accept_invites`)

## Transports

### Webhook

Set `bot.webhook` to `{"url": "https://...", "secret": "..."}`.
Each event is POSTed as a JSON body with headers:

- `X-Bots-Event` event type
- `X-Bots-Event-Id` event sequence number
- `X-Bots-Signature` `sha256=<hmac>` over the raw body when a secret
  is set

Deliveries retry with backoff (1s, 5s, 30s, 120s, then dead-letter). A
15s watchdog releases stalled connections. Ten consecutive failures
disable the webhook until re-enabled through the API or a config update.
The per-bot queue is bounded (`bot_webhook_queue_size`), overflow events
increment `webhook_dropped`.

URL policy: `https` required by default (`bot_webhook_require_https`),
private and loopback addresses are blocked unless
`bot_webhook_allow_private`, `localhost` and `.internal` names are
always blocked, and `bot_webhook_allowed_hosts` can pin delivery to
specific hostnames or suffixes. DNS answers are checked before delivery.
Note that Prosody's HTTP client re-resolves DNS at connect time, so
high-assurance deployments should also restrict egress or use
`bot_webhook_allowed_hosts`.

### SSE

`GET /bots/{name}/events` with a bot token (`read` scope) or a ticket.

```
id: 4
event: message
data: {"type":"message","kind":"dm",...}
```

Resume with `Last-Event-ID` or `?since=<seq>`; the server replays the
bounded ring buffer (`bot_event_buffer`, default 512). If the requested
sequence fell out of the buffer, a `resync` event is sent first. A
comment keepalive is written every `bot_sse_keepalive` seconds.

### WebSocket

`GET /bots/{name}/ws` performs an RFC 6455 upgrade. The server sends a
`hello` frame, then one JSON text frame per event. Client-to-server
frames are JSON text with `op` plus the same fields as the matching
REST action:

```json
{"op": "send", "id": "1", "to": "chat@groups.example.com",
 "kind": "groupchat", "body": "hi"}
```

Responses are `{"type":"result","id":"1","ok":true,"data":{...}}`.
Binary frames are rejected, fragmented messages are reassembled, pings
are answered, and `bot_websocket_frame_limit` bounds input. Disable with
`bot_websocket = false`.

## Shell commands

Registered under `prosodyctl shell`:

```
bots:list host
bots:create host owner_jid name [label]
bots:delete host name
bots:token host name
```

`host` is the component host, for example `bots.example.com`.

## Configuration reference

| Option                       | Default   | Meaning                              |
|------------------------------|-----------|--------------------------------------|
| `bot_owner_host`             | derived   | user host, `bots.example.com` gives `example.com` |
| `bot_muc_hosts`              | `{}`      | room hosts bots may join             |
| `bot_allowed_remote_hosts`   | `{}`      | remote DM targets                    |
| `bot_admin_tokens`           | `{}`      | static management tokens             |
| `bot_max_per_owner`          | 10        | per-owner bot quota                  |
| `bot_max_total`              | 200       | component-wide bot quota             |
| `bot_max_tokens`             | 10        | live tokens per bot                  |
| `bot_send_rate`              | 30/min    | outbound stanza rate per bot         |
| `bot_send_rate_target`       | 60/min    | outbound rate per bot per target JID |
| `bot_dm_sender_rate`         | 60/min    | inbound DM/PM rate per sender per bot|
| `bot_authfail_rate`          | 30/min    | failed auth attempts per source IP   |
| `bot_audit_size`             | 1000      | audit trail entries kept             |
| `bot_event_rate`             | 600/min   | inbound event rate per bot           |
| `bot_api_rate`               | 120/min   | API requests per principal           |
| `bot_event_buffer`           | 512       | replay buffer events per bot         |
| `bot_api_max_body`           | 65536     | request body cap                     |
| `bot_max_message_body`       | 8192      | outbound message body cap            |
| `bot_webhook_require_https`  | true      | reject http webhook URLs             |
| `bot_webhook_allow_private`  | false     | allow private-IP webhook targets     |
| `bot_webhook_allowed_hosts`  | nil       | hostname allowlist for webhooks      |
| `bot_webhook_queue_size`     | 1024      | pending webhook deliveries per bot   |
| `bot_webhook_timeout`        | 30        | delivery watchdog timeout (s)        |
| `bot_websocket`              | true      | enable the WebSocket endpoint        |
| `bot_websocket_frame_limit`  | 131072    | max inbound WS buffer                |
| `bot_sse_keepalive`          | 25        | SSE comment interval (s)             |
| `bot_ticket_ttl`             | 60        | stream ticket lifetime (s)           |
| `bot_auto_accept_invites`    | false     | join invites from local users        |
| `bot_api_cors`               | false     | emit permissive CORS headers         |

## Security and abuse notes

- Bot names are restricted and never collide with user JIDs since they
  live under a dedicated component host. `audit` is a reserved name.
- Only token hashes are stored. Tokens carry `sxb_` prefixes for easy
  detection in logs.
- Tokens can be bound to source IP ranges and a User-Agent substring;
  bound-token misuse is rejected and audited as `auth.deny`.
- Groupchat sends are restricted to joined rooms. DM sends default to
  the local user host, the bot component itself, and an explicit remote
  allowlist. Room PMs go through normal MUC routing and respect room
  policies.
- Event and send paths are rate limited per bot, per target
  (`bot_send_rate_target`) and per DM sender (`bot_dm_sender_rate`).
  Failed authentications are throttled per source IP
  (`bot_authfail_rate`) and audited.
- Every request is access-logged (`bots-api:` info lines with method,
  path, status and source IP). Security-relevant operations are recorded
  in a bounded audit trail readable via `GET /bots/audit`.
- Webhook queues are bounded with dead-lettering and a failure circuit
  breaker.
- Bot deletion sends a final `deleted` event, closes WebSocket sessions,
  clears pending webhook deliveries and revokes all tokens.
- Deleting the owner account disables all of that user's bots.
- Bots have no roster, cannot subscribe to user presence, and answer
  subscription requests with `unsubscribed`. A `subscribe_request` event
  is emitted so runtimes can notice.
- Presence subscription, MUC kick and ban handling all consume normal
  XMPP semantics: if the bot is kicked or the room is destroyed, the
  room record is removed and an event is emitted.

## Audit log

`GET /bots/audit` (admin only) returns the newest entries first.
Query params: `bot`, `since` (unix ts), `limit` (max 1000, default 100).
The trail is persisted in the `bots_audit` store and bounded by
`bot_audit_size` (default 1000 entries). Every entry has `id`, `ts`,
`action`, `actor`, `bot`, `detail`, `ip`.

Recorded actions: `bot.create`, `bot.update`, `bot.delete`,
`token.mint`, `token.revoke`, `bot.join`, `bot.leave`, `auth.fail`,
`auth.deny`.

## Example client

`examples/echo_bot.py` is a dependency-free Python webhook echo bot:
it receives events over HTTP POST and replies through the REST API.
Run it with the bot token and point `bot.webhook.url` at it.
