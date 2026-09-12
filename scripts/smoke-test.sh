#!/usr/bin/env bash
# Smoke-test local image tags built for CI (snikketx/<component>:ci).
set -euo pipefail

COMPONENT="${1:?usage: smoke-test.sh <server|web-portal|cert-manager|updater|backup>}"
IMAGE="snikketx/${COMPONENT}:ci"

case "$COMPONENT" in
server)
	docker run --rm --entrypoint /bin/sh "$IMAGE" -c 'command -v prosody >/dev/null && command -v s6-svscan >/dev/null && test -x /bin/entrypoint.sh'
	set +e
	docker run --rm -e SNIKKET_DOMAIN= "$IMAGE" >/tmp/snikket-server-smoke.out 2>&1
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
	docker run -d --name "$name" -p 127.0.0.1::5765 \
		-e SNIKKET_DOMAIN=ci.example \
		-e SNIKKET_WEB_DOMAIN=ci.example \
		-e SNIKKET_WEB_SECRET_KEY=0123456789abcdef0123456789abcdef \
		-e SNIKKET_WEB_INSECURE_COOKIES=true \
		-e SNIKKET_TWEAK_PORTAL_INTERNAL_HTTP_INTERFACE=0.0.0.0 \
		-e SNIKKET_TWEAK_PORTAL_INTERNAL_HTTP_PORT=5765 \
		-e SNIKKET_WEB_PROSODY_ENDPOINT=http://127.0.0.1:5280/ \
		"$IMAGE" >/dev/null
	port="$(docker port "$name" 5765/tcp | head -1 | awk -F: '{print $NF}')"
	ok=0
	for _ in $(seq 1 30); do
		if wget -q -O- --timeout=2 "http://127.0.0.1:${port}/_health" 2>/dev/null | grep -q 'STATUS OK'; then
			ok=1
			break
		fi
		if command -v curl >/dev/null 2>&1; then
			if curl -fsS --max-time 2 "http://127.0.0.1:${port}/_health" | grep -q 'STATUS OK'; then
				ok=1
				break
			fi
		fi
		sleep 1
	done
	test "$ok" = "1"
	echo "web-portal smoke: ok"
	;;
cert-manager)
	docker run --rm --entrypoint /bin/sh "$IMAGE" -c 'command -v certbot >/dev/null && command -v tini >/dev/null && test -x /entrypoint.sh'
	echo "cert-manager smoke: ok"
	;;
backup)
	name="smoke-backup-$$"
	cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
	trap cleanup EXIT
	docker run -d --name "$name" -p 127.0.0.1::9292 \
		-e SNIKKET_BACKUP_TOKEN=ci-token \
		-e SNIKKET_BACKUP_LISTEN=0.0.0.0:9292 \
		-e SNIKKET_BACKUP_STATE_DIR=/var/lib/snikket-backup \
		-e SNIKKET_BACKUP_ARCHIVE_DIR=/var/lib/snikket-backup/archives \
		-e SNIKKET_BACKUP_COMPOSE_DIR=/work \
		"$IMAGE" >/dev/null
	port="$(docker port "$name" 9292/tcp | head -1 | awk -F: '{print $NF}')"
	ok=0
	for _ in $(seq 1 30); do
		if wget -q -O- --timeout=2 "http://127.0.0.1:${port}/healthz" 2>/dev/null | grep -q 'ok'; then
			ok=1
			break
		fi
		if command -v curl >/dev/null 2>&1; then
			if curl -fsS --max-time 2 "http://127.0.0.1:${port}/healthz" | grep -q 'ok'; then
				ok=1
				break
			fi
		fi
		sleep 1
	done
	test "$ok" = "1"
	echo "backup smoke: ok"
	;;
updater)
	name="smoke-updater-$$"
	cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
	trap cleanup EXIT
	docker run -d --name "$name" -p 127.0.0.1::9191 \
		-e SNIKKET_UPDATER_TOKEN=ci-token \
		-e SNIKKET_UPDATER_LISTEN=0.0.0.0:9191 \
		-e SNIKKET_UPDATER_STATE_DIR=/var/lib/snikket-updater \
		-e SNIKKET_UPDATER_COMPOSE_DIR=/work \
		"$IMAGE" >/dev/null
	port="$(docker port "$name" 9191/tcp | head -1 | awk -F: '{print $NF}')"
	ok=0
	for _ in $(seq 1 30); do
		if wget -q -O- --timeout=2 "http://127.0.0.1:${port}/healthz" 2>/dev/null | grep -q 'ok'; then
			ok=1
			break
		fi
		if command -v curl >/dev/null 2>&1; then
			if curl -fsS --max-time 2 "http://127.0.0.1:${port}/healthz" | grep -q 'ok'; then
				ok=1
				break
			fi
		fi
		sleep 1
	done
	test "$ok" = "1"
	echo "updater smoke: ok"
	;;
*)
	echo "unknown component: $COMPONENT" >&2
	exit 1
	;;
esac
