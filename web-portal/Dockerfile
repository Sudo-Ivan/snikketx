# syntax=docker/dockerfile:1.7

FROM golang:1.27-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS build

ARG BUILD_SERIES=dev
ARG BUILD_ID=0

ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
COPY web/ web/

RUN go build -trimpath \
        -ldflags "-s -w -X main.version=${BUILD_SERIES}.${BUILD_ID}" \
        -o /out/portal ./cmd/portal

# The state directory is created here so a fresh named volume inherits the
# ownership of the unprivileged account the container runs as.
RUN install -d -m 0750 /out/state

FROM gcr.io/distroless/static-debian12:latest@sha256:d75cdd72874d4790092fcb1b058493ecf6bb5bf2b2b897045b00ff01d91843f2

ARG BUILD_SERIES=dev
ARG BUILD_ID=0
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

ENV SNIKKET_WEB_PROSODY_ENDPOINT=http://snikket_server:5280 \
    SNIKKET_TWEAK_PORTAL_INTERNAL_HTTP_INTERFACE=0.0.0.0 \
    SNIKKET_TWEAK_PORTAL_INTERNAL_HTTP_PORT=5765

COPY --from=build /out/portal /portal
COPY --from=build --chown=10001:10001 /out/state /var/lib/snikket-web-portal

USER 10001:10001

WORKDIR /var/lib/snikket-web-portal

EXPOSE 5765

STOPSIGNAL SIGTERM

ENTRYPOINT ["/portal"]

LABEL org.opencontainers.image.title="snikketx-web-portal" \
      org.opencontainers.image.description="SnikketX web portal" \
      org.opencontainers.image.source="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.url="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.documentation="https://github.com/Sudo-Ivan/snikketx#readme" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.vendor="Sudo-Ivan" \
      org.opencontainers.image.version="${BUILD_SERIES}.${BUILD_ID}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"
