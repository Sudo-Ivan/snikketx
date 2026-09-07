#!/usr/bin/env bash
# Smoke-test local image tags built for CI (snikketx/<component>:ci).
set -euo pipefail

COMPONENT="${1:?usage: smoke-test.sh <server|web-portal|web-proxy|cert-manager>}"
IMAGE="snikketx/${COMPONENT}:ci"

case "$COMPONENT" in
server)
	docker run --rm --entrypoint /bin/sh "$IMAGE" -c 'command -v prosody >/dev/null && command -v s6-svscan >/dev/null && test -x /bin/entrypoint.sh'
	# Entrypoint must refuse to start without SNIKKET_DOMAIN
	set +e
	docker run --rm -e SNIKKET_DOMAIN= "$IMAGE" > /tmp/snikket-server-smoke.out 2>&1
	status=$?
	set -e
	if [ "$status" -eq 0 ]; then
		echo "server smoke: expected failure without SNIKKET_DOMAIN" >&2
		exit 1
	fi
	grep -qi 'SNIKKET_DOMAIN' /tmp/snikket-server-smoke.out
	echo "server smoke: ok"
	;;
web-portal)
	name="smoke-portal-$$"
	cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
	trap cleanup EXIT
	docker run -d --name "$name" \
		-e SNIKKET_DOMAIN=ci.example \
		-e SNIKKET_TWEAK_PORTAL_INTERNAL_HTTP_INTERFACE=0.0.0.0 \
		-e SNIKKET_TWEAK_PORTAL_INTERNAL_HTTP_PORT=5765 \
		-e SNIKKET_WEB_PROSODY_ENDPOINT=http://127.0.0.1:5280/ \
		"$IMAGE" >/dev/null
	ok=0
	for _ in $(seq 1 30); do
		if docker exec "$name" python3 -c 'import urllib.request; urllib.request.urlopen("http://127.0.0.1:5765/_health", timeout=2).read()' 2>/dev/null; then
			ok=1
			break
		fi
		sleep 1
	done
	test "$ok" = "1"
	echo "web-portal smoke: ok"
	;;
web-proxy)
	docker run --rm --entrypoint /bin/sh "$IMAGE" -c 'command -v nginx >/dev/null && command -v tini >/dev/null && test -x /entrypoint.sh'
	echo "web-proxy smoke: ok"
	;;
cert-manager)
	docker run --rm --entrypoint /bin/sh "$IMAGE" -c 'command -v certbot >/dev/null && command -v tini >/dev/null && test -x /entrypoint.sh'
	echo "cert-manager smoke: ok"
	;;
*)
	echo "unknown component: $COMPONENT" >&2
	exit 1
	;;
esac
