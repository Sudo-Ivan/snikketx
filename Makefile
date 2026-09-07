.PHONY: all docker docker-server docker-portal docker-proxy docker-certs docker-updater \
	init-dev up up-dev down down-dev status logs admin invite preflight \
	backup restore migrate rollback screenshot

IMAGE_PREFIX ?= ghcr.io/sudo-ivan/snikketx
TAG ?= latest
BUILD_SERIES ?= dev
BUILD_ID ?= 0
VCS_REF ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

DEST ?=
ARCHIVE ?=
FROM ?= /etc/snikket
BACKUP_DIR ?= $(CURDIR)/backups

BUILD_ARGS = \
	--build-arg BUILD_SERIES=$(BUILD_SERIES) \
	--build-arg BUILD_ID=$(BUILD_ID) \
	--build-arg VCS_REF=$(VCS_REF) \
	--build-arg BUILD_DATE=$(BUILD_DATE)

all: docker

docker: docker-server docker-portal docker-proxy docker-certs docker-updater

docker-server:
	docker build $(BUILD_ARGS) -t $(IMAGE_PREFIX)/server:$(TAG) ./server

docker-portal:
	docker build $(BUILD_ARGS) -t $(IMAGE_PREFIX)/web-portal:$(TAG) ./web-portal

docker-proxy:
	docker build $(BUILD_ARGS) -t $(IMAGE_PREFIX)/web-proxy:$(TAG) ./web-proxy

docker-certs:
	docker build $(BUILD_ARGS) -t $(IMAGE_PREFIX)/cert-manager:$(TAG) ./cert-manager

docker-updater:
	docker build $(BUILD_ARGS) -t $(IMAGE_PREFIX)/updater:$(TAG) ./updater

init-dev:
	./scripts/init.sh --dev

up:
	./scripts/start.sh

up-dev:
	./scripts/start.sh --dev

down:
	./scripts/stop.sh

down-dev:
	./scripts/stop.sh --dev

status:
	./scripts/status.sh

logs:
	./scripts/logs.sh

admin:
	./scripts/bootstrap-admin.sh --dev

invite:
	./scripts/new-invite.sh --admin --group default

preflight:
	./scripts/preflight.sh

backup:
	@test -n "$(DEST)" || (echo "Set DEST=/absolute/backup/dir" >&2; exit 1)
	./scripts/backup.sh "$(DEST)"

restore:
	@test -n "$(ARCHIVE)" || (echo "Set ARCHIVE=/absolute/path/to/snikket-data-*.tar.gz" >&2; exit 1)
	./scripts/restore.sh "$(ARCHIVE)"

migrate:
	./scripts/migrate-from-snikket.sh --from "$(FROM)" --backup-dir "$(BACKUP_DIR)"

rollback:
	./scripts/rollback-to-snikket.sh --from "$(FROM)" --backup-dir "$(BACKUP_DIR)"

screenshot:
	./scripts/screenshot-dashboard.sh
