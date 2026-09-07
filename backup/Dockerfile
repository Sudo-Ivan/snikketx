# syntax=docker/dockerfile:1.27.0@sha256:bde3983e9c939224420ddaf6b784cc30e09b035a4dea01f581230c50809f372e

FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/backup ./cmd/backup
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/backup ./cmd/backup

FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS restic
ARG RESTIC_VERSION=0.19.1
RUN apk add --no-cache git \
	&& git clone --depth 1 --branch "v${RESTIC_VERSION}" https://github.com/restic/restic.git /src \
	&& cd /src \
	&& go get golang.org/x/crypto@v0.56.0 golang.org/x/net@v0.58.0 go.opentelemetry.io/otel/sdk@v1.46.0 google.golang.org/grpc@v1.83.2 \
	&& go mod tidy \
	&& CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/restic ./cmd/restic

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN apk upgrade --no-cache \
	&& apk add --no-cache ca-certificates docker-cli docker-cli-compose tar gzip \
	&& rm -rf /var/cache/apk/*
COPY --from=restic /out/restic /usr/local/bin/restic
COPY --from=build /out/backup /usr/local/bin/snikket-backup
USER root
EXPOSE 9292
ENTRYPOINT ["/usr/local/bin/snikket-backup"]
