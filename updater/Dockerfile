FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/updater ./cmd/updater
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/updater ./cmd/updater

FROM alpine:3.21 AS cosign
ARG TARGETARCH=amd64
ARG COSIGN_VERSION=2.4.3
RUN apk add --no-cache curl ca-certificates \
	&& arch="${TARGETARCH}" \
	&& if [ "$arch" = "arm64" ]; then arch=arm64; elif [ "$arch" = "amd64" ] || [ -z "$arch" ]; then arch=amd64; fi \
	&& curl -fsSL -o /cosign "https://github.com/sigstore/cosign/releases/download/v${COSIGN_VERSION}/cosign-linux-${arch}" \
	&& chmod +x /cosign

FROM docker:27-cli
RUN apk add --no-cache ca-certificates
COPY --from=cosign /cosign /usr/local/bin/cosign
COPY --from=build /out/updater /usr/local/bin/snikket-updater
USER root
EXPOSE 9191
ENTRYPOINT ["/usr/local/bin/snikket-updater"]
