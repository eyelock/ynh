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

# Cursor publishes no checksum or signature for its installer, so the script
# itself is pinned by digest instead: fetched to disk, verified, and only then
# executed. Previously it was piped straight to bash, so a change at
# cursor.com or its CDN would have run silently in the image.
#
# This trades a maintenance cost for that: when Cursor edits the installer the
# build fails with a digest mismatch, and the value below has to be updated
# deliberately after reading the diff. That is the point. A build that keeps
# working through an unreviewed change to a remote script is the thing being
# fixed.
ARG CURSOR_INSTALL_SHA256=f145a23a600d42c79bdf6a4cd36b77decb7b68359f4f0b86f60be3c84deea96c
RUN curl -fsS https://cursor.com/install -o /tmp/cursor-install.sh && \
    echo "${CURSOR_INSTALL_SHA256}  /tmp/cursor-install.sh" | sha256sum -c - && \
    bash /tmp/cursor-install.sh && \
    rm -f /tmp/cursor-install.sh && \
    cp -L /root/.local/bin/agent /usr/local/bin/agent && \
    chmod 755 /usr/local/bin/agent

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
