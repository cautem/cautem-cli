# SPDX-FileCopyrightText: Copyright (c) 2026 the cautem authors
# SPDX-License-Identifier: Apache-2.0

ARG DEBIAN_BASE=mirror.gcr.io/library/debian:bookworm@sha256:2c037a04925515fdd6ea85ea14a682d0e79931f5e9f5d07b6dbfc6ba12f9e858
FROM ${DEBIAN_BASE}

ENV DEBIAN_FRONTEND=noninteractive \
    CAUTEM_POLICY=/etc/cautem/policy.yaml \
    CAUTEM_LOG_DIR=/tmp/cautem-logs

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 cautem \
    && useradd --uid 10001 --gid cautem --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin cautem \
    && mkdir -p /etc/cautem /tmp/cautem-logs \
    && chown -R cautem:cautem /tmp/cautem-logs

COPY --chmod=0555 cautem /usr/local/bin/cautem
COPY --chmod=0444 default.yaml /etc/cautem/policy.yaml

USER 10001:10001
EXPOSE 3128
ENTRYPOINT ["/usr/local/bin/cautem"]
CMD ["proxy", "--listen", "0.0.0.0:3128", "--policy", "/etc/cautem/policy.yaml"]
