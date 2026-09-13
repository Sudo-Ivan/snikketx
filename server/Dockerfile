# syntax=docker/dockerfile:1.27.0@sha256:bde3983e9c939224420ddaf6b784cc30e09b035a4dea01f581230c50809f372e


# Badinage web client build. No release tarballs are published, so the
# source is fetched at a pinned commit and built with VITE_BASE=/chat/ to
# match the HTTP path mod_snikket_badinage serves it under.
# Pinned pnpm install follows upstream docker/Dockerfile.
FROM docker.io/library/node:26-alpine@sha256:ef24c5053d50fdc3e4e56eb4e7ddb7861874ab0fdc797046ba897581deb8e868 AS badinage-build

ARG BADINAGE_VERSION=73930133b3b62cf1a9704adbb1187c6bc241fd3d
ARG BADINAGE_SHA256=bcaf797100042900d40ca800d05a335194131b7c01835dcc512f1aaa0cc718d9
ARG PNPM_VERSION=11.24.0
ARG PNPM_SHA256_X64=2fce00e10729d0c1f3836d499b908697fd3cd0832332651fbcee79c0a7442dcb
ARG PNPM_SHA256_ARM64=950160e3d1d039e0bd1f234da5a307fdc9c1d6b9c030683adfcb85f607c7d500
ARG TARGETARCH

WORKDIR /badinage

RUN set -eu; \
    case "${TARGETARCH:-$(uname -m)}" in \
      amd64|x86_64) pnpm_arch=x64; pnpm_sum="$PNPM_SHA256_X64" ;; \
      arm64|aarch64) pnpm_arch=arm64; pnpm_sum="$PNPM_SHA256_ARM64" ;; \
      *) echo "unsupported arch" >&2; exit 1 ;; \
    esac; \
    wget -qO /tmp/pnpm.tgz "https://github.com/pnpm/pnpm/releases/download/v${PNPM_VERSION}/pnpm-linux-${pnpm_arch}-musl.tar.gz"; \
    echo "$pnpm_sum  /tmp/pnpm.tgz" | sha256sum -c -; \
    mkdir -p /usr/local/lib/pnpm; \
    tar -xzf /tmp/pnpm.tgz -C /usr/local/lib/pnpm; \
    ln -s /usr/local/lib/pnpm/pnpm /usr/local/bin/pnpm; \
    rm /tmp/pnpm.tgz

RUN set -eu; \
    wget -qO /tmp/badinage.tar.gz "https://github.com/Quad4-Software/Badinage/archive/${BADINAGE_VERSION}.tar.gz"; \
    echo "$BADINAGE_SHA256  /tmp/badinage.tar.gz" | sha256sum -c -; \
    tar -xzf /tmp/badinage.tar.gz --strip-components=1; \
    rm /tmp/badinage.tar.gz; \
    pnpm install --frozen-lockfile; \
    VITE_BASE=/chat/ pnpm build


FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS build

ARG BUILD_SERIES=dev
ARG BUILD_ID=0
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG PROSODY_MODULES_REV=7851ba324048

ENV LANG=C.UTF-8

RUN apk upgrade --no-cache \
    && apk add --no-cache \
      ca-certificates curl wget tar gzip bash coreutils shadow \
      prosody coturn s6 msmtp idn2-utils libqrencode-tools \
      lua5.4 lua5.4-expat lua5.4-filesystem lua5.4-sec lua5.4-socket \
      lua5.4-unbound lua5.4-dbi-sqlite3 lua5.4-ldap openssl

COPY tools/smtp-url-to-msmtp.lua /usr/local/bin/smtp-url-to-msmtp
COPY docker/entrypoint.sh /bin/entrypoint.sh
COPY docker/install-alpine.sh /opt/snikket-build/install-alpine.sh
COPY snikket-modules /usr/local/lib/snikket-modules
COPY ansible/files/prosody.cfg.lua \
     ansible/files/migrator.cfg.lua \
     ansible/files/restricted_users.pfw \
     ansible/files/turnserver.conf \
     ansible/files/msmtp.conf \
     ansible/files/snikketx-logo.png \
     ansible/files/refresh-certs.cron \
     /opt/snikket-build/
COPY ansible/files/bin /opt/snikket-build/bin
COPY ansible/files/services /opt/snikket-build/services

RUN chmod 550 /usr/local/bin/smtp-url-to-msmtp /bin/entrypoint.sh \
      /opt/snikket-build/install-alpine.sh \
    && BUILD_SERIES="$BUILD_SERIES" BUILD_ID="$BUILD_ID" \
       PROSODY_MODULES_REV="$PROSODY_MODULES_REV" \
       /opt/snikket-build/install-alpine.sh \
    && rm -rf /opt/snikket-build /var/cache/apk/* /tmp/*

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

ARG BUILD_SERIES=dev
ARG BUILD_ID=0
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

ENV LANG=C.UTF-8

COPY --from=build / /
COPY --from=badinage-build /badinage/dist /usr/local/lib/snikket-badinage

USER root

VOLUME ["/snikket"]

STOPSIGNAL SIGTERM

ENTRYPOINT ["/bin/entrypoint.sh"]

LABEL org.opencontainers.image.title="snikketx-server" \
      org.opencontainers.image.description="Snikket Prosody server image" \
      org.opencontainers.image.source="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.url="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.documentation="https://github.com/Sudo-Ivan/snikketx#readme" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="Sudo-Ivan" \
      org.opencontainers.image.version="${BUILD_SERIES}.${BUILD_ID}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"
