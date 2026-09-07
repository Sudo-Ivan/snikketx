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

.PHONY: race
race:
	$(GO) test -race ./...

.PHONY: fuzz
fuzz:
	$(GO) test ./internal/session -fuzz=FuzzDecode -fuzztime=10s
	$(GO) test ./internal/csrf -fuzz=FuzzCSRFValid -fuzztime=10s
	$(GO) test ./internal/colour -fuzz=FuzzTextToCSS -fuzztime=5s
	$(GO) test ./internal/xmpp -fuzz=FuzzExtractIQReply -fuzztime=10s

.PHONY: bench
bench:
	$(GO) test ./internal/session ./internal/colour -bench=. -benchmem

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: fix
fix:
	$(GO) fix ./...

.PHONY: gosec
gosec:
	gosec -fmt=text ./...

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: check
check: fmt fix vet gosec race test

.PHONY: clean
clean:
	rm -rf bin
