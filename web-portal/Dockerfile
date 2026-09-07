# syntax=docker/dockerfile:1.7


FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS build

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        python3 python3-mypy python3-dotenv python3-toml python3-babel \
        python3-distutils sassc make \
    && rm -rf /var/lib/apt/lists/*

COPY Makefile /opt/snikket-web-portal/Makefile
COPY snikket_web/ /opt/snikket-web-portal/snikket_web/
COPY babel.cfg /opt/snikket-web-portal/babel.cfg

WORKDIR /opt/snikket-web-portal
RUN make

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

ARG BUILD_SERIES=dev
ARG BUILD_ID=0
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    SNIKKET_WEB_PYENV=/etc/snikket-web-portal/env.py \
    SNIKKET_WEB_PROSODY_ENDPOINT=http://127.0.0.1:5280/ \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      python3 \
      python3-aiohttp python3-email-validator python3-flask-babel \
      python3-flaskext.wtf python3-hsluv python3-hypercorn \
      python3-quart python3-typing-extensions python3-wtforms \
      python3-pip \
    && pip3 install --no-cache-dir --break-system-packages environ-config \
    && apt-get remove -y --purge python3-pip \
    && apt-get autoremove -y \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* /root/.cache /tmp/* \
    && groupadd --system --gid 10001 snikket \
    && useradd --system --uid 10001 --gid snikket --home-dir /var/lib/snikket-web-portal \
        --shell /usr/sbin/nologin snikket \
    && install -d -o snikket -g snikket -m 0750 /var/lib/snikket-web-portal \
    && install -d -o snikket -g snikket -m 0755 /opt/snikket-web-portal \
    && install -d -o root -g snikket -m 0755 /etc/snikket-web-portal

COPY docker/env.py /etc/snikket-web-portal/env.py
COPY --from=build --chown=snikket:snikket /opt/snikket-web-portal/snikket_web/ /opt/snikket-web-portal/snikket_web/
COPY --chown=snikket:snikket babel.cfg /opt/snikket-web-portal/babel.cfg
COPY --chown=root:snikket --chmod=0555 docker/entrypoint.sh /entrypoint.sh

RUN echo "$BUILD_SERIES $BUILD_ID" > /opt/snikket-web-portal/.app_version \
    && chown snikket:snikket /opt/snikket-web-portal/.app_version

WORKDIR /opt/snikket-web-portal

USER 10001:10001

EXPOSE 5765

STOPSIGNAL SIGTERM

ENTRYPOINT ["/entrypoint.sh"]

LABEL org.opencontainers.image.title="snikketx-web-portal" \
      org.opencontainers.image.description="Snikket web portal" \
      org.opencontainers.image.source="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.url="https://github.com/Sudo-Ivan/snikketx" \
      org.opencontainers.image.documentation="https://github.com/Sudo-Ivan/snikketx#readme" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.vendor="Sudo-Ivan" \
      org.opencontainers.image.version="${BUILD_SERIES}.${BUILD_ID}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"
