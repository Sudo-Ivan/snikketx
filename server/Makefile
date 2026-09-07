.PHONY: all docker site

DOCS := $(docs/**.md)

all: docker

IMAGE_PREFIX ?= ghcr.io/sudo-ivan/snikketx
TAG ?= latest

docker:
	docker build -t $(IMAGE_PREFIX)/server:$(TAG) .

site: mkdocs.yml $(DOCS)
	echo $(DOCS)
	mkdocs build

docs/_po/snikket-server-docs.pot: po4a.conf $(DOCS)
	po4a \
	  --package-name snikket-server \
	  --package-version vcs \
	  --copyright-holder "Snikket Team <team@snikket.org>" \
	  po4a.conf
