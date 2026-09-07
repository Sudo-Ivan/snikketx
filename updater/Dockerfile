# syntax=docker/dockerfile:1.27.0@sha256:bde3983e9c939224420ddaf6b784cc30e09b035a4dea01f581230c50809f372e

FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/updater ./cmd/updater
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/updater ./cmd/updater

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS cosign
ARG TARGETARCH=amd64
ARG COSIGN_VERSION=3.1.3
# sha256 of official cosign-linux-{amd64,arm64} from cosign_checksums.txt for v3.1.3
ARG COSIGN_SHA256_AMD64=4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71
ARG COSIGN_SHA256_ARM64=c5d324e091826b0d7a78eb16fef316450b4eb9aaec045611c08ba06f5e73220a
RUN apk add --no-cache curl ca-certificates \
	&& arch="${TARGETARCH}" \
	&& if [ "$arch" = "arm64" ]; then arch=arm64; elif [ "$arch" = "amd64" ] || [ -z "$arch" ]; then arch=amd64; fi \
	&& if [ "$arch" = "arm64" ]; then expect="${COSIGN_SHA256_ARM64}"; else expect="${COSIGN_SHA256_AMD64}"; fi \
	&& curl -fsSL -o /cosign "https://github.com/sigstore/cosign/releases/download/v${COSIGN_VERSION}/cosign-linux-${arch}" \
	&& echo "${expect}  /cosign" | sha256sum -c - \
	&& chmod +x /cosign

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN apk upgrade --no-cache \
	&& apk add --no-cache ca-certificates docker-cli docker-cli-compose \
	&& rm -rf /var/cache/apk/*
COPY --from=cosign /cosign /usr/local/bin/cosign
COPY --from=build /out/updater /usr/local/bin/snikket-updater
USER root
EXPOSE 9191
ENTRYPOINT ["/usr/local/bin/snikket-updater"]
