.PHONY: all docker docker-server docker-portal docker-proxy docker-certs

IMAGE_PREFIX ?= ghcr.io/sudo-ivan/snikketx
TAG ?= latest
BUILD_SERIES ?= dev
BUILD_ID ?= 0
VCS_REF ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

BUILD_ARGS = \
	--build-arg BUILD_SERIES=$(BUILD_SERIES) \
	--build-arg BUILD_ID=$(BUILD_ID) \
	--build-arg VCS_REF=$(VCS_REF) \
	--build-arg BUILD_DATE=$(BUILD_DATE)

all: docker

docker: docker-server docker-portal docker-proxy docker-certs

docker-server:
	docker build $(BUILD_ARGS) -t $(IMAGE_PREFIX)/server:$(TAG) ./server

docker-portal:
	docker build $(BUILD_ARGS) -t $(IMAGE_PREFIX)/web-portal:$(TAG) ./web-portal

docker-proxy:
	docker build $(BUILD_ARGS) -t $(IMAGE_PREFIX)/web-proxy:$(TAG) ./web-proxy

docker-certs:
	docker build $(BUILD_ARGS) -t $(IMAGE_PREFIX)/cert-manager:$(TAG) ./cert-manager
