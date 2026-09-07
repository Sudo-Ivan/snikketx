# syntax=docker/dockerfile:1.7


FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS build

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        certbot tini anacron idn2 jq \
    && mv /etc/cron.daily/0anacron /tmp/0anacron \
    && rm -f /etc/cron.daily/* /etc/cron.d/* \
    && mv /tmp/0anacron /etc/cron.daily/0anacron \
    && rm -rf /var/lib/apt/lists/* /var/cache/* /tmp/* \
    && useradd --system --create-home --home-dir /snikket/letsencrypt letsencrypt

COPY entrypoint.sh /entrypoint.sh
COPY certbot.cron /etc/cron.daily/certbot
COPY sendmail /usr/sbin/sendmail
COPY wait_for_http.py /usr/local/bin/wait_for_http.py
COPY tools/report-error.sh /usr/local/bin/report-error.sh

RUN chmod 0555 /entrypoint.sh /etc/cron.daily/certbot \
        /usr/sbin/sendmail /usr/local/bin/wait_for_http.py \
        /usr/local/bin/report-error.sh

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

ARG BUILD_SERIES=dev
ARG BUILD_ID=0
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

ENV LANG=C.UTF-8

COPY --from=build / /

# Entrypoint adjusts volume ownership then runs anacron. Certbot runs as letsencrypt.
USER root

VOLUME ["/snikket"]

STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/bin/bash", "/entrypoint.sh"]

LABEL org.opencontainers.image.title="snikketx-cert-manager" \
      org.opencontainers.image.description="Snikket Let's Encrypt helper" \
      org.opencontainers.image.source="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.url="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.documentation="https://github.com/Sudo-Ivan/snikketx#readme" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="Sudo-Ivan" \
      org.opencontainers.image.version="${BUILD_SERIES}.${BUILD_ID}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"
