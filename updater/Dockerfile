# syntax=docker/dockerfile:1.27.0@sha256:bde3983e9c939224420ddaf6b784cc30e09b035a4dea01f581230c50809f372e

FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/updater ./cmd/updater
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/updater ./cmd/updater

FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS cosign
# The cosign v3.1.3 release binary bundles grpc-go <1.83.1 and a Go 1.26
# stdlib with published CVEs. Build from this pinned source commit (verified
# through go.sum) until a release ships grpc-go >=1.83.1, then switch back to
# the sha256-verified release binary.
ARG COSIGN_COMMIT=633e8f4303b7db64c369bdfa315374a8bd511ac6
# The commit still pins x/crypto and x/mod below the versions fixing published
# CVEs; lift them to the first fixed releases during the build.
ARG COSIGN_XCRYPTO=v0.57.0
ARG COSIGN_XMOD=v0.41.0
RUN apk add --no-cache git ca-certificates \
	&& mkdir /src && cd /src \
	&& git init && git remote add origin https://github.com/sigstore/cosign.git \
	&& git fetch --depth 1 origin ${COSIGN_COMMIT} \
	&& git checkout FETCH_HEAD \
	&& go get golang.org/x/crypto@${COSIGN_XCRYPTO} golang.org/x/mod@${COSIGN_XMOD} \
	&& CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /cosign ./cmd/cosign

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN apk upgrade --no-cache \
	&& apk add --no-cache ca-certificates docker-cli docker-cli-compose \
	&& rm -rf /var/cache/apk/*
COPY --from=cosign /cosign /usr/local/bin/cosign
COPY --from=build /out/updater /usr/local/bin/snikket-updater
USER root
EXPOSE 9191
ENTRYPOINT ["/usr/local/bin/snikket-updater"]
