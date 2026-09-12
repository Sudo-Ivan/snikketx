# syntax=docker/dockerfile:1.27.0@sha256:bde3983e9c939224420ddaf6b784cc30e09b035a4dea01f581230c50809f372e


FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS build

ENV LANG=C.UTF-8

RUN apk upgrade --no-cache \
    && apk add --no-cache \
      certbot tini idn2-utils jq bash coreutils shadow python3 py3-requests \
    && adduser -S -D -H -h /snikket/letsencrypt -s /sbin/nologin letsencrypt \
    && install -d -m 755 /etc/cron.daily \
    && rm -rf /var/cache/apk/*

COPY entrypoint.sh /entrypoint.sh
COPY certbot.cron /etc/cron.daily/certbot
COPY sendmail /usr/local/bin/snikket-sendmail
COPY wait_for_http.py /usr/local/bin/wait_for_http.py
COPY tools/report-error.sh /usr/local/bin/report-error.sh

RUN rm -f /usr/sbin/sendmail \
    && ln -s /usr/local/bin/snikket-sendmail /usr/sbin/sendmail \
    && chmod 0555 /entrypoint.sh /etc/cron.daily/certbot \
        /usr/local/bin/snikket-sendmail /usr/local/bin/wait_for_http.py \
        /usr/local/bin/report-error.sh

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

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/bin/bash", "/entrypoint.sh"]

LABEL org.opencontainers.image.title="snikketx-cert-manager" \
      org.opencontainers.image.description="SnikketX Let's Encrypt helper" \
      org.opencontainers.image.source="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.url="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.documentation="https://github.com/Sudo-Ivan/snikketx#readme" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="Sudo-Ivan" \
      org.opencontainers.image.version="${BUILD_SERIES}.${BUILD_ID}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"
