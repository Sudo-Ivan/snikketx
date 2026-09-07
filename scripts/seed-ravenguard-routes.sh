#!/bin/bash
# Seed RavenGuard admin upstreams and host/path routes for snikketx.
# Replaces Traefik dynamic routing. Idempotent.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

MODE="${1:-prod}"
CONF="${ROOT}/snikket.conf"
ENVFILE="${ROOT}/.env"
CONTAINER="${RG_CONTAINER:-snikketx-ravenguard}"

domain=""
if [[ -f "$CONF" ]]; then
	domain=$(grep -E '^SNIKKET_DOMAIN=' "$CONF" | head -n1 | cut -d= -f2- | tr -d '"' | tr -d "'")
fi
if [[ -z "$domain" && -f "$ENVFILE" ]]; then
	domain=$(grep -E '^SNIKKET_DOMAIN=' "$ENVFILE" | head -n1 | cut -d= -f2- | tr -d '"' | tr -d "'")
fi
domain="${domain:-${SNIKKET_DOMAIN:-}}"
if [[ -z "$domain" ]]; then
	echo "SNIKKET_DOMAIN is not set. Run ./scripts/init.sh or export it." >&2
	exit 1
fi

password=""
if [[ -f "$ENVFILE" ]]; then
	password=$(grep -E '^RG_ADMIN_BOOTSTRAP_PASSWORD=' "$ENVFILE" | head -n1 | cut -d= -f2- || true)
fi
password="${RG_ADMIN_BOOTSTRAP_PASSWORD:-$password}"

echo "Waiting for RavenGuard (${CONTAINER})..."
ok=0
for _ in $(seq 1 90); do
	if docker container inspect "$CONTAINER" >/dev/null 2>&1; then
		st=$(docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null || echo false)
		if [[ "$st" == "true" ]]; then
			ok=1
			break
		fi
	fi
	sleep 1
done
if [[ "$ok" != "1" ]]; then
	echo "Container ${CONTAINER} is not running." >&2
	exit 1
fi

if [[ -z "$password" ]]; then
	for _ in $(seq 1 30); do
		password=$(docker exec "$CONTAINER" cat /data/admin/initial_admin_password 2>/dev/null || true)
		if [[ -n "$password" ]]; then
			break
		fi
		sleep 1
	done
fi
if [[ -z "$password" ]]; then
	echo "No RavenGuard admin password. Set RG_ADMIN_BOOTSTRAP_PASSWORD in .env." >&2
	exit 1
fi

net=$(docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}' "$CONTAINER")
docker rm -f snikketx-rg-seed >/dev/null 2>&1 || true
docker run -d --name snikketx-rg-seed --network "$net" \
	docker.io/library/python:3.12-alpine \
	sleep 180 >/dev/null

docker exec -i -e RG_PASS="$password" -e RG_DOMAIN="$domain" -e RG_MODE="$MODE" snikketx-rg-seed python3 - <<'PY'
import json, os, urllib.request, http.cookiejar, time, urllib.error

base = "http://ravenguard:9090/api/v1"
password = os.environ["RG_PASS"]
domain = os.environ["RG_DOMAIN"]
mode = os.environ.get("RG_MODE", "prod")
hosts = [domain, f"share.{domain}", f"groups.{domain}"]

cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def call(method, path, body=None, csrf=None):
	data = None
	headers = {"Content-Type": "application/json"}
	if csrf:
		headers["X-CSRF-Token"] = csrf
	if body is not None:
		data = json.dumps(body).encode()
	req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
	with opener.open(req, timeout=30) as resp:
		raw = resp.read().decode() or "null"
		return json.loads(raw)

last_err = None
for _ in range(60):
	try:
		call("POST", "/auth/login", {"username": "admin", "password": password})
		last_err = None
		break
	except Exception as e:
		last_err = e
		time.sleep(1)
if last_err is not None:
	raise SystemExit(f"admin login failed: {last_err}")

me = call("GET", "/auth/me")
csrf = me.get("csrf_token") or me.get("csrf")
if not csrf:
	raise SystemExit(f"no csrf in /auth/me: {me}")

def as_list(payload, key):
	if isinstance(payload, list):
		return payload
	if isinstance(payload, dict) and key in payload:
		return payload[key]
	raise SystemExit(f"unexpected {key} payload: {payload!r}")

upstreams = as_list(call("GET", "/upstreams"), "upstreams")
by_name = {u["name"]: u for u in upstreams}

def ensure_upstream(name, url, health_path="/", health_enabled=True):
	if name in by_name:
		return by_name[name]["id"]
	body = {
		"name": name,
		"url": url,
		"health_enabled": health_enabled,
		"health_path": health_path,
	}
	up = call("POST", "/upstreams", body, csrf=csrf)
	by_name[name] = up
	print(f"created upstream {name} -> {url}")
	return up["id"]

portal_id = ensure_upstream("portal", "http://snikket_portal:5765", "/_health", True)
prosody_id = ensure_upstream("prosody", "http://snikket_server:5280", "/", False)
acme_id = None
if mode != "dev":
	acme_id = ensure_upstream("acme-webroot", "http://acme_webroot:8080", "/", False)

routes = as_list(call("GET", "/routes"), "routes")
route_keys = {(r.get("name"), r.get("path_prefix")) for r in routes}

def ensure_route(name, path_prefix, upstream_id, priority):
	key = (name, path_prefix)
	if key in route_keys:
		return
	body = {
		"name": name,
		"enabled": True,
		"hosts": hosts,
		"path_prefix": path_prefix,
		"upstream_id": upstream_id,
		"strip_prefix": False,
		"priority": priority,
	}
	call("POST", "/routes", body, csrf=csrf)
	route_keys.add(key)
	print(f"created route {name} {path_prefix} priority={priority}")

prosody_paths = [
	"/upload",
	"/http-bind",
	"/xmpp-websocket",
	"/admin_api",
	"/invites_api",
	"/invites_bootstrap",
	"/.well-known/host-meta",
]
for p in prosody_paths:
	slug = p.strip("/").replace("/", "-") or "root"
	ensure_route(f"prosody-{slug}", p, prosody_id, 50)

if acme_id:
	ensure_route("acme-challenge", "/.well-known/acme-challenge", acme_id, 100)

ensure_route("portal", "/", portal_id, 10)
print(f"seeded RavenGuard routes for {domain} ({mode})")
PY

docker rm -f snikketx-rg-seed >/dev/null 2>&1 || true
echo "RavenGuard routes ready for ${domain} (${MODE})"
