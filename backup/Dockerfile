# syntax=docker/dockerfile:1.27.0@sha256:bde3983e9c939224420ddaf6b784cc30e09b035a4dea01f581230c50809f372e

FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/backup ./cmd/backup
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/backup ./cmd/backup

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
ARG TARGETARCH=amd64
ARG RESTIC_VERSION=0.18.0
RUN apk upgrade --no-cache \
	&& apk add --no-cache ca-certificates curl docker-cli docker-cli-compose tar gzip \
	&& arch="${TARGETARCH}" \
	&& if [ "$arch" = "arm64" ]; then arch=arm64; elif [ "$arch" = "amd64" ] || [ -z "$arch" ]; then arch=amd64; fi \
	&& curl -fsSL -o /tmp/restic.bz2 "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_linux_${arch}.bz2" \
	&& bunzip2 -c /tmp/restic.bz2 > /usr/local/bin/restic \
	&& chmod +x /usr/local/bin/restic \
	&& rm -f /tmp/restic.bz2 \
	&& rm -rf /var/cache/apk/*
COPY --from=build /out/backup /usr/local/bin/snikket-backup
USER root
EXPOSE 9292
ENTRYPOINT ["/usr/local/bin/snikket-backup"]
