.PHONY: all docker docker-server docker-portal docker-proxy docker-certs

IMAGE_PREFIX ?= ghcr.io/sudo-ivan/snikketx
TAG ?= latest
BUILD_SERIES ?= dev
BUILD_ID ?= 0

all: docker

docker: docker-server docker-portal docker-proxy docker-certs

docker-server:
	docker build \
		--build-arg BUILD_SERIES=$(BUILD_SERIES) \
		--build-arg BUILD_ID=$(BUILD_ID) \
		-t $(IMAGE_PREFIX)/server:$(TAG) \
		./server

docker-portal:
	docker build \
		--build-arg BUILD_SERIES=$(BUILD_SERIES) \
		--build-arg BUILD_ID=$(BUILD_ID) \
		-t $(IMAGE_PREFIX)/web-portal:$(TAG) \
		./web-portal

docker-proxy:
	docker build \
		--build-arg BUILD_SERIES=$(BUILD_SERIES) \
		--build-arg BUILD_ID=$(BUILD_ID) \
		-t $(IMAGE_PREFIX)/web-proxy:$(TAG) \
		./web-proxy

docker-certs:
	docker build \
		--build-arg BUILD_SERIES=$(BUILD_SERIES) \
		--build-arg BUILD_ID=$(BUILD_ID) \
		-t $(IMAGE_PREFIX)/cert-manager:$(TAG) \
		./cert-manager
