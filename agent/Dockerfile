# syntax=docker/dockerfile:1.7

# Digest-pinned; refresh periodically with:
#   docker buildx imagetools inspect node:22-bookworm-slim
FROM node:22-bookworm-slim@sha256:f3a68cf41a855d227d1b0ab832bed9749469ef38cf4f58182fb8c893bc462383 AS base

ENV DEBIAN_FRONTEND=noninteractive \
    NODE_ENV=production \
    NPM_CONFIG_FUND=false \
    NPM_CONFIG_AUDIT=false \
    PI_OFFLINE=1 \
    PI_SKIP_VERSION_CHECK=1

# Install sandbox runtime dependencies.
# ripgrep + fd are pre-installed so pi-mono doesn't try to download them on first run.
RUN --mount=type=cache,id=dclaw-apt-cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,id=dclaw-apt-lists,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      curl \
      git \
      jq \
      less \
      openssl \
      procps \
      ripgrep \
      fd-find \
      tini \
      unzip && \
    ln -s /usr/bin/fdfind /usr/local/bin/fd

# --- Build stage: install production deps only ---
FROM base AS deps
WORKDIR /app
COPY --chown=node:node package.json package-lock.json ./
RUN --mount=type=cache,id=dclaw-npm-cache,target=/root/.npm,sharing=locked \
    npm ci --omit=dev --no-audit --no-fund

# --- Runtime stage ---
FROM base AS runtime

# The `node` user (uid 1000) is pre-created by the upstream image.
USER node
WORKDIR /workspace

COPY --from=deps --chown=node:node /app/node_modules /app/node_modules
COPY --chown=node:node run.mjs /app/run.mjs
COPY --chown=node:node package.json /app/package.json

ENV PATH=/app/node_modules/.bin:$PATH

# tini forwards SIGTERM/SIGINT to Node and reaps zombies.
ENTRYPOINT ["/usr/bin/tini", "--", "node", "/app/run.mjs"]
