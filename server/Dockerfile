# syntax=docker/dockerfile:1.7


FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS build

ARG BUILD_SERIES=dev
ARG BUILD_ID=0
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

ENV LANG=C.UTF-8 \
    DEBIAN_FRONTEND=noninteractive

RUN install -d -m 755 /snikket

COPY tools/smtp-url-to-msmtp.lua /usr/local/bin/smtp-url-to-msmtp
COPY docker/entrypoint.sh /bin/entrypoint.sh
RUN chmod 550 /usr/local/bin/smtp-url-to-msmtp \
    && chmod 550 /bin/entrypoint.sh

COPY ansible /opt/ansible
COPY snikket-modules /usr/local/lib/snikket-modules

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        software-properties-common ca-certificates \
        gpg gpg-agent \
        ansible python3-passlib \
        build-essential \
    && c_rehash \
    && ansible-playbook -c local -i localhost, \
        --extra-vars "ansible_python_interpreter=/usr/bin/python3" \
        /opt/ansible/snikket.yml \
    && apt-get remove --purge -y \
         ansible \
         software-properties-common \
         gpg gpg-agent \
         python3-passlib \
         build-essential \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/* /var/cache/* /root/.cache /tmp/* \
    && rm -rf /opt/ansible \
    && echo "Snikket $BUILD_SERIES $BUILD_ID" > /usr/lib/prosody/snikket.version

# Runtime image: copy the built rootfs, keep a small attack surface.
FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

ARG BUILD_SERIES=dev
ARG BUILD_ID=0
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

ENV LANG=C.UTF-8

COPY --from=build / /

# Entrypoint needs root for volume ownership and s6. Child services drop via s6-setuidgid.
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
