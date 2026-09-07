# syntax=docker/dockerfile:1.27.0@sha256:bde3983e9c939224420ddaf6b784cc30e09b035a4dea01f581230c50809f372e


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
      lua5.4-unbound lua5.4-dbi-sqlite3 openssl

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
