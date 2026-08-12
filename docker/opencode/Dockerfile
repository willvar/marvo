FROM oven/bun:1-slim

ARG OPENCODE_VERSION=1.18.15
ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      ffmpeg \
      file \
      git \
      imagemagick \
      jq \
      libheif-examples \
      libimage-exiftool-perl \
      openssh-client \
      poppler-utils \
      python3 \
      ripgrep \
      unzip \
      webp \
      wget \
      xz-utils \
      zip && \
    rm -rf /var/lib/apt/lists/* && \
    bun add -g "opencode-ai@$OPENCODE_VERSION" && \
    chmod 755 /root && \
    rm -rf /root/.bun/install/cache

COPY --chmod=0444 AGENTS.md /opt/marvo/AGENTS.md
COPY --chmod=0444 opencode.json /opt/marvo/opencode.json
COPY --chmod=0555 bin/marvo-personalization /usr/local/bin/marvo-personalization
COPY --chmod=0555 entrypoint.sh /opt/marvo/entrypoint.sh
RUN chmod 0755 /opt/marvo

WORKDIR /workspace

EXPOSE 4096

ENTRYPOINT ["/opt/marvo/entrypoint.sh"]
CMD ["opencode", "serve", "--hostname", "0.0.0.0", "--port", "4096"]
