# syntax=docker/dockerfile:1.7
#
# Build from repo root (go.work + frontend workspace):
#   docker build -f services/manager/Dockerfile \
#     --build-arg VERSION=1.0.0 --build-arg COMMIT_SHA=$(git rev-parse --short HEAD) \
#     -t izetmolla/supabase-manager:latest .
#
# Or: make docker-build service=manager
# Requires BuildKit (DOCKER_BUILDKIT=1).

ARG NODE_VERSION=24
ARG PNPM_VERSION=11.22.0
ARG GO_VERSION=1.27.1
ARG ALPINE_VERSION=3.22
ARG DEBIAN_VERSION=trixie
ARG SUPABASE_CLI_VERSION=2.119.0
ARG DOCKER_CLI_VERSION=29
ARG APP_UID=1000
ARG APP_GID=1000

# ---- UI: frontend/apps/manager → services/manager/web/dist ------------------------------------
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS web
ARG PNPM_VERSION
RUN npm install -g pnpm@${PNPM_VERSION} --no-audit --no-fund
WORKDIR /src/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml frontend/.npmrc ./
COPY frontend/apps/manager/package.json ./apps/manager/
COPY frontend/packages/ui/package.json ./packages/ui/
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm --filter manager build

# ---- API: static binary with the UI embedded -------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
# The module's replace directives point at shared/ and services/version; the workspace file also
# lists modules that are not copied into this image.
ENV CGO_ENABLED=0 \
    GOFLAGS="-trimpath" \
    GOWORK=off \
    GOPROXY=https://proxy.golang.org,direct

COPY shared/go.mod shared/go.sum ./shared/
COPY services/manager/go.mod services/manager/go.sum ./services/manager/
COPY services/version/go.mod ./services/version/
WORKDIR /src/services/manager
RUN --mount=type=cache,target=/go/pkg/mod go mod download

WORKDIR /src
COPY shared ./shared
COPY services/version ./services/version
COPY services/manager ./services/manager
COPY --from=web /src/services/manager/web/dist ./services/manager/web/dist

ARG VERSION=0.0.0-dev
ARG COMMIT_SHA=unknown
WORKDIR /src/services/manager
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w -X github.com/supabase-manager/version.Version=${VERSION} -X github.com/supabase-manager/version.CommitSHA=${COMMIT_SHA}" \
    -o /main ./cmd

# ---- Docker CLI: the Supabase CLI shells out to `docker` (static binary) ------------------------
FROM docker:${DOCKER_CLI_VERSION}-cli AS docker-cli

# ---- shrink stage: strip + UPX inside the toolkit image ---------------------------------------
FROM izetmolla/containerws:binoptimization AS shrink
COPY --from=build /main /out/main
COPY --from=docker-cli /usr/local/bin/docker /out/docker
RUN ls -lh /out/main /out/docker \
    && optimize /out/main --best --lzma \
    && optimize /out/docker --best --lzma

# ---- Supabase CLI: official release, checksum verified ----------------------------------------
FROM alpine:${ALPINE_VERSION} AS supabase-cli
ARG SUPABASE_CLI_VERSION TARGETARCH
RUN apk add --no-cache curl
WORKDIR /tmp/cli
RUN set -eu; \
    asset="supabase_${SUPABASE_CLI_VERSION}_linux_${TARGETARCH}.tar.gz"; \
    base="https://github.com/supabase/cli/releases/download/v${SUPABASE_CLI_VERSION}"; \
    curl -fsSLO "$base/$asset"; \
    curl -fsSL "$base/checksums.txt" | grep " $asset\$" | sha256sum -c -; \
    tar -xzf "$asset" supabase; \
    install -m 0755 supabase /usr/local/bin/supabase

# ---- rootfs: the few files scratch needs ------------------------------------------------------
# The Supabase CLI is a Bun-compiled binary and needs the glibc loader and libraries; UPX would
# corrupt its embedded payload, so it is copied as is.
FROM debian:${DEBIAN_VERSION}-slim AS rootfs
ARG APP_UID APP_GID
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*
COPY --from=supabase-cli /usr/local/bin/supabase /rootfs/usr/local/bin/supabase
RUN set -eu; \
    for lib in $(ldd /rootfs/usr/local/bin/supabase | grep -o '/[^ ]*'); do cp --parents -L "$lib" /rootfs; done; \
    mkdir -p /rootfs/etc/ssl/certs /rootfs/usr/share /rootfs/tmp /appdata/home /appdata/bin; \
    cp /etc/ssl/certs/ca-certificates.crt /rootfs/etc/ssl/certs/; \
    cp -r /usr/share/zoneinfo /rootfs/usr/share/; \
    chmod 1777 /rootfs/tmp; \
    printf 'root:x:0:0:root:/root:/sbin/nologin\nmanager:x:%s:%s:manager:/data/home:/sbin/nologin\n' "$APP_UID" "$APP_GID" > /rootfs/etc/passwd; \
    printf 'root:x:0:\nmanager:x:%s:\n' "$APP_GID" > /rootfs/etc/group; \
    printf 'hosts: files dns\n' > /rootfs/etc/nsswitch.conf

# ---- runtime: scratch -------------------------------------------------------------------------
FROM scratch
ARG SUPABASE_CLI_VERSION APP_UID APP_GID VERSION COMMIT_SHA
COPY --from=rootfs /rootfs /
COPY --from=rootfs --chown=${APP_UID}:${APP_GID} /appdata /data
COPY --from=shrink /out/main /usr/local/bin/supabase-manager
COPY --from=shrink /out/docker /usr/local/bin/docker

# Host networking is expected: the manager reaches project ports on 127.0.0.1.
ENV PATH=/usr/local/bin \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt \
    ADDR=127.0.0.1:8080 \
    DB_DRIVER=sqlite \
    DB_DSN=/data/manager.db \
    SECRETS_FILE=/data/.env \
    TOOLS_DIR=/data/bin \
    HOME=/data/home \
    PROJECTS_ROOT=/etc/supabase-manager/projects \
    SUPABASE_BIN=supabase \
    DOCKER_SOCKET=/var/run/docker.sock

USER ${APP_UID}:${APP_GID}
WORKDIR /data
VOLUME ["/data"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["supabase-manager", "healthcheck"]

LABEL org.opencontainers.image.title="supabase-manager" \
      org.opencontainers.image.description="Control panel for running many local Supabase stacks on one Docker host" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT_SHA}" \
      io.supabase.cli.version="${SUPABASE_CLI_VERSION}"

ENTRYPOINT ["supabase-manager"]
