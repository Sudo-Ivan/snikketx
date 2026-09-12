#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root

FROM_DIR=""
BACKUP_DIR=""
YES=0

usage() {
	cat <<'EOF'
Usage: ./scripts/migrate-from-snikket.sh --from /path/to/classic/snikket [--backup-dir /abs/path] [--yes]

Migrates a classic Snikket docker compose install (the layout from
https://snikket.org/service/quickstart/, e.g. /etc/snikket with
docker-compose.yml + snikket.conf) to this SnikketX checkout without losing
Prosody data (accounts, MAM chats, MUCs, uploads).

Steps:
  1. Full backup of the running classic stack (mandatory)
  2. Locate the classic /snikket data volume (named volume or bind mount)
  3. Stop classic containers and remove leftovers
  4. Write deploy/migrate/docker-compose.migrate.yml, import snikket.conf,
     merge required keys into .env
  5. Preflight + start SnikketX
  6. Verify containers and print rollback instructions

State for rollback is written to deploy/migrate/state.env and
<backup-dir>/LAST_PRE_MIGRATE.

Keep the classic directory until you trust the new stack.
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	-h | --help)
		usage
		exit 0
		;;
	--from)
		FROM_DIR="$2"
		shift 2
		;;
	--backup-dir)
		BACKUP_DIR="$2"
		shift 2
		;;
	--yes | -y)
		YES=1
		shift
		;;
	*)
		echo "Unknown argument: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

if [[ -z "$FROM_DIR" || ! -d "$FROM_DIR" ]]; then
	echo "--from DIR is required and must exist" >&2
	usage >&2
	exit 1
fi
FROM_DIR="$(cd "$FROM_DIR" && pwd)"

if [[ -z "$BACKUP_DIR" ]]; then
	BACKUP_DIR="$(pwd)/backups"
fi
mkdir -p "$BACKUP_DIR"
BACKUP_DIR="$(cd "$BACKUP_DIR" && pwd)"

if [[ "$YES" -ne 1 ]]; then
	echo "This will stop the classic Snikket stack in ${FROM_DIR}"
	echo "after writing a backup under ${BACKUP_DIR}."
	echo -n "Continue? [y/N] "
	read -r -n1 ans
	echo ""
	case "$ans" in
	y | Y) ;;
	*)
		echo "Aborting."
		exit 1
		;;
	esac
fi

echo "== 1/6 Backup classic stack =="
./scripts/backup.sh "$BACKUP_DIR" --local
# Point at newest backup dir
LATEST=$(ls -1dt "$BACKUP_DIR"/snikketx-backup-* 2>/dev/null | head -n1)
if [[ -z "$LATEST" ]]; then
	echo "Backup failed to produce a directory." >&2
	exit 1
fi
# backup.sh copies this checkout's snikket.conf; overwrite with the classic
# files so the archive can fully restore the classic install.
if [[ -f "$FROM_DIR/snikket.conf" ]]; then
	cp -a "$FROM_DIR/snikket.conf" "$LATEST/snikket.conf"
fi
for f in docker-compose.yml docker-compose.yaml compose.yml compose.yaml; do
	if [[ -f "$FROM_DIR/$f" ]]; then
		cp -a "$FROM_DIR/$f" "$LATEST/docker-compose.classic.yml"
		break
	fi
done
echo "Using backup ${LATEST}"
printf '%s\n' "$LATEST" >"$BACKUP_DIR/LAST_PRE_MIGRATE"
printf '%s\n' "$FROM_DIR" >"$BACKUP_DIR/LAST_CLASSIC_FROM"

echo "== 2/6 Locate classic data volume =="
# Must run before `docker compose down`, which removes the classic containers.
CLASSIC_VOL=""
CLASSIC_BIND=""
CLASSIC_PROJECT=""
if docker container inspect "$SNIKKETX_CONTAINER_SERVER" >/dev/null 2>&1; then
	CLASSIC_VOL=$(docker inspect -f '{{range .Mounts}}{{if and (eq .Destination "/snikket") (eq .Type "volume")}}{{.Name}}{{end}}{{end}}' "$SNIKKETX_CONTAINER_SERVER" 2>/dev/null || true)
	CLASSIC_BIND=$(docker inspect -f '{{range .Mounts}}{{if and (eq .Destination "/snikket") (eq .Type "bind")}}{{.Source}}{{end}}{{end}}' "$SNIKKETX_CONTAINER_SERVER" 2>/dev/null || true)
	CLASSIC_PROJECT=$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$SNIKKETX_CONTAINER_SERVER" 2>/dev/null || true)
fi
if [[ -z "$CLASSIC_PROJECT" ]]; then
	# Compose normalizes the project name from the directory basename.
	CLASSIC_PROJECT="${COMPOSE_PROJECT_NAME:-$(basename "$FROM_DIR" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]//g')}"
fi
if [[ -z "$CLASSIC_VOL" && -z "$CLASSIC_BIND" ]]; then
	for cand in "${CLASSIC_PROJECT}_snikket_data" snikket_snikket_data snikket-selfhosted_snikket_data snikketx_snikket_data; do
		if [[ -n "$cand" && "$cand" != "_snikket_data" ]] && docker volume inspect "$cand" >/dev/null 2>&1; then
			CLASSIC_VOL="$cand"
			break
		fi
	done
fi
if [[ -z "$CLASSIC_VOL" && -z "$CLASSIC_BIND" ]]; then
	# Last resort: scan *_snikket_data volumes. Prefer the one whose compose
	# project label matches the classic project; take it when it is the only
	# candidate on the host.
	mapfile -t vols < <(docker volume ls --format '{{.Name}}' | grep '_snikket_data$' || true)
	for v in "${vols[@]:-}"; do
		[[ -z "$v" ]] && continue
		vp=$(docker volume inspect -f '{{index .Labels "com.docker.compose.project"}}' "$v" 2>/dev/null || true)
		if [[ -n "$CLASSIC_PROJECT" && "$vp" == "$CLASSIC_PROJECT" ]]; then
			CLASSIC_VOL="$v"
			break
		fi
	done
	if [[ -z "$CLASSIC_VOL" && ${#vols[@]} -eq 1 ]]; then
		CLASSIC_VOL="${vols[0]}"
	fi
fi
if [[ -z "$CLASSIC_VOL" && -z "$CLASSIC_BIND" ]]; then
	echo "Could not find the classic /snikket data volume or bind mount." >&2
	echo "Restore from backup into a fresh volume instead:" >&2
	echo "  make up && ./scripts/restore.sh ${LATEST}/snikket-data-*.tar.gz --yes" >&2
	exit 1
fi

mkdir -p deploy/migrate
if [[ -n "$CLASSIC_VOL" ]]; then
	echo "Classic volume: ${CLASSIC_VOL}"
	cat >deploy/migrate/docker-compose.migrate.yml <<EOF
# Generated by migrate-from-snikket.sh. Reuses classic Prosody data.
volumes:
  snikket_data:
    external: true
    name: ${CLASSIC_VOL}
EOF
else
	echo "Classic bind mount: ${CLASSIC_BIND}"
	cat >deploy/migrate/docker-compose.migrate.yml <<EOF
# Generated by migrate-from-snikket.sh. Reuses classic Prosody data.
volumes:
  snikket_data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: "${CLASSIC_BIND}"
EOF
fi
echo "Wrote deploy/migrate/docker-compose.migrate.yml"

cat >deploy/migrate/state.env <<EOF
BACKUP_DIR="${LATEST}"
CLASSIC_FROM="${FROM_DIR}"
CLASSIC_VOL="${CLASSIC_VOL}"
CLASSIC_BIND="${CLASSIC_BIND}"
EOF
echo "Wrote deploy/migrate/state.env"

echo "== 3/6 Stop classic stack =="
CLASSIC_HAS_COMPOSE=0
for f in docker-compose.yml docker-compose.yaml compose.yml compose.yaml; do
	if [[ -f "$FROM_DIR/$f" ]]; then
		CLASSIC_HAS_COMPOSE=1
		break
	fi
done
if [[ "$CLASSIC_HAS_COMPOSE" -eq 1 ]]; then
	(cd "$FROM_DIR" && snikketx_docker_compose down --remove-orphans) || \
		echo "classic compose down reported errors; continuing" >&2
fi
# Remove leftover classic containers that still hold the shared names
# (snikket, snikket-portal, ...) and would block `compose up`.
for c in snikket snikket-proxy snikket-portal snikket-certs snikket-web-proxy; do
	if docker container inspect "$c" >/dev/null 2>&1; then
		proj=$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$c" 2>/dev/null || true)
		if [[ "$proj" != "snikketx" ]]; then
			echo "Removing leftover classic container ${c}"
			docker rm -f "$c" >/dev/null 2>&1 || true
		fi
	fi
done

echo "== 4/6 Import config =="
if [[ -f "$FROM_DIR/snikket.conf" ]]; then
	cp -a "$FROM_DIR/snikket.conf" ./snikket.conf
elif [[ -f "$LATEST/snikket.conf" ]]; then
	cp -a "$LATEST/snikket.conf" ./snikket.conf
else
	echo "No snikket.conf found in classic dir or backup." >&2
	exit 1
fi
domain=$(snikketx_conf_get SNIKKET_DOMAIN)
email=$(snikketx_conf_get SNIKKET_ADMIN_EMAIL)
if [[ -z "$domain" ]]; then
	echo "SNIKKET_DOMAIN is missing from ${FROM_DIR}/snikket.conf" >&2
	exit 1
fi

# Merge into .env instead of overwriting: keep SNIKKETX_IMAGE_TAG, Restic
# credentials and endpoint overrides the operator may already have set.
if [[ -f .env ]]; then
	cp -a .env "$LATEST/env.pre-migrate"
else
	touch .env
fi
snikketx_env_set SNIKKET_DOMAIN "$domain"
if [[ -n "$email" ]]; then
	snikketx_env_set SNIKKET_ADMIN_EMAIL "$email"
fi
snikketx_env_ensure RG_CHALLENGE_SECRET "$(snikketx_random 32)"
snikketx_env_ensure RG_ADMIN_BOOTSTRAP_PASSWORD "$(snikketx_random 24)"
snikketx_env_ensure SNIKKET_UPDATER_TOKEN "$(snikketx_random 32)"
snikketx_env_ensure SNIKKET_BACKUP_TOKEN "$(snikketx_random 32)"
echo "Updated .env for domain ${domain}"

echo "== 5/6 Preflight + start =="
./scripts/preflight.sh || true
./scripts/start.sh

echo "== 6/6 Verify =="
for _ in $(seq 1 60); do
	up=1
	for c in snikket snikket-portal snikketx-ravenguard; do
		st=$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null || echo false)
		[[ "$st" == "true" ]] || up=0
	done
	[[ "$up" -eq 1 ]] && break
	sleep 2
done
./scripts/status.sh || echo "HTTPS probe still pending (first ACME issuance can take a minute)."

cat <<EOF

Migration started for ${domain}.
Backup kept at: ${LATEST}
Classic compose kept at: ${FROM_DIR}

Log in at https://${domain}/ with your existing admin XMPP account.
The first HTTPS hit can take a minute while RavenGuard finishes ACME issuance.

If something is wrong:
  ./scripts/rollback-to-snikket.sh --from ${FROM_DIR} --backup-dir ${LATEST}

When you trust SnikketX, you can remove deploy/migrate/docker-compose.migrate.yml
only after copying data into a native snikketx_snikket_data volume (optional).
EOF
