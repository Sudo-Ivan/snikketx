.PHONY: all docker site

DOCS := $(wildcard docs/*.md docs/*/*.md)

all: docker

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

docker:
	docker build $(BUILD_ARGS) -t $(IMAGE_PREFIX)/server:$(TAG) .

site: mkdocs.yml $(DOCS)
	echo $(DOCS)
	mkdocs build

docs/_po/snikket-server-docs.pot: po4a.conf $(DOCS)
	po4a \
	  --package-name snikket-server \
	  --package-version vcs \
	  --copyright-holder "Snikket Team <team@snikket.org>" \
	  po4a.conf
