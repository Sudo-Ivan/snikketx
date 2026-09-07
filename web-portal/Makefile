GO ?= go
BIN ?= bin/portal
ICONLIST ?= tools/icons.list
SPRITE ?= web/static/img/icons.svg
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all
all: build

# Rebuild the SVG sprite from the pinned Lucide release. Needs network access.
.PHONY: icons
icons:
	./tools/fetch-icons.sh $(ICONLIST) $(SPRITE)

.PHONY: build
build: icons
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/portal

# Same build without refreshing the sprite, for offline and container builds.
.PHONY: build-offline
build-offline:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/portal

.PHONY: run
run: build-offline
	$(BIN)

.PHONY: test
test:
	$(GO) test ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: check
check: vet test

.PHONY: clean
clean:
	rm -rf bin
