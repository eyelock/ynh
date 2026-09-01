# syntax=docker/dockerfile:1

# Stage 1: Build ynh and ynd
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

# Copy module files first for layer caching (currently zero deps, but
# this avoids invalidating the module cache when source changes)
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -v \
    -ldflags "-s -w -X github.com/eyelock/ynh/internal/config.Version=${VERSION}" \
    -o /out/ynh ./cmd/ynh && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -v \
    -ldflags "-s -w -X github.com/eyelock/ynh/internal/config.Version=${VERSION}" \
    -o /out/ynd ./cmd/ynd

# Stage 2a: Claude Code CLI (parallel)
FROM node:22-alpine AS claude-cli
ARG CLAUDE_CODE_VERSION=2.1.76
RUN --mount=type=cache,target=/root/.npm \
    npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}"

# Stage 2b: Codex CLI (parallel)
FROM node:22-alpine AS codex-cli
ARG CODEX_VERSION=0.114.0
RUN --mount=type=cache,target=/root/.npm \
    npm install -g --include=optional "@openai/codex@${CODEX_VERSION}"

# Stage 2c: Cursor Agent CLI (parallel)
FROM alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40 AS cursor-cli
RUN apk add --no-cache curl bash

# The installer at cursor.com/install is regenerated per release: it embeds
# the version and a direct artifact URL, and its content changed within an
# hour during this work. Pinning the installer by digest was tried and
# abandoned: a tripwire that fires hourly is one people disable, which is
# worse than none, because it trains everyone to bump it without reading the
# diff.
#
# So the installer is bypassed entirely and the versioned artifact it points
# at is fetched directly. That is reproducible, and the digest of a given
# version is stable, unlike the script that installs it.
#
# To upgrade: read cursor.com/install for the current version, then take the
# two digests from
#   https://downloads.cursor.com/lab/<version>/linux/{x64,arm64}/agent-cli-package.tar.gz
ARG CURSOR_VERSION=2026.08.31-4057e58
ARG CURSOR_SHA256_X64=7e306db5750219a99c00ed517fe8b235d3c54e4ca5f77e2ff160cc97ce707798
ARG CURSOR_SHA256_ARM64=cf5db6b5047b3280d8a49471cfd41beb1d5e475774177df5df2851857ab6514a
RUN set -eu; \
    case "$(uname -m)" in \
      x86_64|amd64)  CURSOR_ARCH=x64;   CURSOR_SHA="${CURSOR_SHA256_X64}"   ;; \
      arm64|aarch64) CURSOR_ARCH=arm64; CURSOR_SHA="${CURSOR_SHA256_ARM64}" ;; \
      *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/agent.tar.gz \
      "https://downloads.cursor.com/lab/${CURSOR_VERSION}/linux/${CURSOR_ARCH}/agent-cli-package.tar.gz"; \
    echo "${CURSOR_SHA}  /tmp/agent.tar.gz" | sha256sum -c -; \
    mkdir -p /tmp/agent; \
    tar xzf /tmp/agent.tar.gz -C /tmp/agent; \
    cp /tmp/agent/dist-package/cursor-agent /usr/local/bin/agent; \
    chmod 755 /usr/local/bin/agent; \
    rm -rf /tmp/agent /tmp/agent.tar.gz

# Stage 3: Runtime — assemble everything
FROM node:22-alpine

RUN apk add --no-cache git openssh-client tini bash curl

# Vendor CLIs from parallel stages (scoped dirs, no overlap)
COPY --from=claude-cli /usr/local/lib/node_modules/@anthropic-ai/ /usr/local/lib/node_modules/@anthropic-ai/
COPY --from=claude-cli /usr/local/bin/claude /usr/local/bin/claude
COPY --from=codex-cli  /usr/local/lib/node_modules/@openai/ /usr/local/lib/node_modules/@openai/
RUN ln -s ../lib/node_modules/@openai/codex/bin/codex.js /usr/local/bin/codex
COPY --from=cursor-cli /usr/local/bin/agent /usr/local/bin/agent

# Copy ynh binaries from builder
COPY --link --from=builder /out/ynh /usr/local/bin/ynh
COPY --link --from=builder /out/ynd /usr/local/bin/ynd

# Configurable UID/GID to match host user (avoids permission issues with bind mounts).
# node:22-alpine already uses GID 1000 for 'node', so we try the requested GID
# first and fall back to letting Alpine assign one.
ARG USER_UID=1000
ARG USER_GID=1000
RUN addgroup -g ${USER_GID} ynh 2>/dev/null || addgroup ynh; \
    adduser -u ${USER_UID} -G ynh -D ynh 2>/dev/null || adduser -G ynh -D ynh

# Default YNH_HOME inside container
ENV YNH_HOME=/home/ynh/.ynh

# Create directory structure
RUN mkdir -p /home/ynh/.ynh/harnesses \
             /home/ynh/.ynh/cache \
             /home/ynh/.ynh/run \
             /home/ynh/.ynh/bin && \
    chown -R ynh:ynh /home/ynh

# Working directory for project mounts
RUN mkdir -p /workspace && chown ynh:ynh /workspace
WORKDIR /workspace

USER ynh

# Image metadata — versions of all packaged binaries
ARG VERSION=dev
ARG CLAUDE_CODE_VERSION=2.1.76
ARG CODEX_VERSION=0.114.0
LABEL org.opencontainers.image.title="ynh" \
      org.opencontainers.image.description="Harness template manager for AI coding assistants" \
      org.opencontainers.image.source="https://github.com/eyelock/ynh" \
      org.opencontainers.image.version="${VERSION}" \
      dev.ynh.version="${VERSION}" \
      dev.ynh.claude-code.version="${CLAUDE_CODE_VERSION}" \
      dev.ynh.codex.version="${CODEX_VERSION}" \
      dev.ynh.cursor-agent.version="latest"

# tini handles PID 1 signal forwarding correctly for both:
# - Claude's syscall.Exec (process replacement)
# - Codex/Cursor's child process signal forwarding
ENTRYPOINT ["tini", "-s", "--", "ynh"]
CMD ["help"]
