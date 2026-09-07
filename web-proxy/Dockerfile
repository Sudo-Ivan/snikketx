# syntax=docker/dockerfile:1.7


FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS build

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        tini nginx s6 gettext-base idn2 libjs-bootstrap4 libjs-jquery \
    && rm -f /etc/nginx/sites-enabled/default \
    && rm -rf /var/lib/apt/lists/* /var/cache/* /tmp/*

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
    && chown -R www-data:www-data /var/www/html /var/lib/nginx /var/log/nginx \
    && install -d -o www-data -g www-data -m 0755 /run/nginx /etc/nginx/sites-enabled /etc/nginx/snippets

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

ARG BUILD_SERIES=dev
ARG BUILD_ID=0
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

ENV LANG=C.UTF-8

COPY --from=build / /

# Entrypoint renders configs then s6. nginx workers run as www-data.
USER root

VOLUME ["/snikket"]

STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/bin/sh", "/entrypoint.sh"]

LABEL org.opencontainers.image.title="snikketx-web-proxy" \
      org.opencontainers.image.description="Legacy Snikket nginx proxy" \
      org.opencontainers.image.source="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.url="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.documentation="https://github.com/Sudo-Ivan/snikketx#readme" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="Sudo-Ivan" \
      org.opencontainers.image.version="${BUILD_SERIES}.${BUILD_ID}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"
