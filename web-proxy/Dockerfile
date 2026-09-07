# syntax=docker/dockerfile:1.27.0@sha256:bde3983e9c939224420ddaf6b784cc30e09b035a4dea01f581230c50809f372e


FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS build

ENV LANG=C.UTF-8

RUN apk upgrade --no-cache \
    && apk add --no-cache \
      tini nginx s6 gettext idn2-utils \
    && rm -f /etc/nginx/http.d/default.conf \
    && rm -rf /var/cache/apk/* /tmp/*

COPY entrypoint.sh /entrypoint.sh
COPY render-template.sh /usr/local/bin/render-template.sh
COPY nginx/nginx.conf /etc/nginx/nginx.conf
COPY nginx/snikket-common.template /etc/nginx/templates/snikket-common
COPY nginx/startup.template /etc/nginx/templates/startup
COPY nginx/http.template /etc/nginx/templates/http
COPY nginx/https.template /etc/nginx/templates/https
COPY service /etc/sv
COPY static /var/www/html/static
COPY startup.html /var/www/html/index.html
COPY error-pages /var/www/html/_errors

RUN chmod 0555 /entrypoint.sh /usr/local/bin/render-template.sh \
    && addgroup -S -g 82 www-data 2>/dev/null || true \
    && adduser -S -u 82 -G www-data -H -s /sbin/nologin www-data 2>/dev/null || true \
    && chown -R www-data:www-data /var/www/html \
    && install -d -o www-data -g www-data -m 0755 /run/nginx /etc/nginx/http.d /var/lib/nginx /var/log/nginx

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
CMD ["/bin/sh", "/entrypoint.sh"]

LABEL org.opencontainers.image.title="snikketx-web-proxy" \
      org.opencontainers.image.description="Legacy SnikketX nginx proxy" \
      org.opencontainers.image.source="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.url="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.documentation="https://github.com/Sudo-Ivan/snikketx#readme" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="Sudo-Ivan" \
      org.opencontainers.image.version="${BUILD_SERIES}.${BUILD_ID}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"
