# syntax=docker/dockerfile:1.7
#
# Proxy images run by the Supabase Manager Proxy Manager: the official nginx or Traefik image
# plus sm-proxy-agent, which supervises the proxy and applies the configuration the manager
# sends over gRPC. Build from the repo root:
#
#   docker build -f services/proxy-agent/proxy.Dockerfile --target nginx \
#     -t izetmolla/supabase-manager-proxy-nginx:latest .
#   docker build -f services/proxy-agent/proxy.Dockerfile --target traefik \
#     -t izetmolla/supabase-manager-proxy-traefik:latest .
#
# Or: make proxy-images

ARG GO_VERSION=1.27.1
ARG NGINX_IMAGE=nginx:stable-alpine
ARG TRAEFIK_IMAGE=traefik:v3

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
ENV CGO_ENABLED=0 \
    GOFLAGS="-trimpath" \
    GOPROXY=https://proxy.golang.org,direct
COPY shared/go.mod shared/go.sum ./shared/
COPY services/version/go.mod ./services/version/
COPY services/proxy-agent/go.mod services/proxy-agent/go.sum ./services/proxy-agent/
WORKDIR /src/services/proxy-agent
RUN --mount=type=cache,target=/go/pkg/mod go mod download
WORKDIR /src
COPY shared ./shared
COPY services/version ./services/version
COPY services/proxy-agent ./services/proxy-agent
ARG VERSION=0.0.0-dev
ARG COMMIT_SHA=unknown
WORKDIR /src/services/proxy-agent
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH GOWORK=off \
    go build -ldflags="-s -w -X github.com/supabase-manager/version.Version=${VERSION} -X github.com/supabase-manager/version.CommitSHA=${COMMIT_SHA}" \
    -o /sm-proxy-agent ./cmd

FROM ${NGINX_IMAGE} AS nginx
RUN apk add --no-cache tini \
    && mkdir -p /etc/nginx/sm /var/log/nginx/sm
COPY --from=build /sm-proxy-agent /usr/local/bin/sm-proxy-agent
ENV SM_PROXY_KIND=nginx
LABEL org.opencontainers.image.title="Supabase Manager proxy (nginx)"
STOPSIGNAL SIGTERM
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/sm-proxy-agent"]
CMD []

FROM ${TRAEFIK_IMAGE} AS traefik
RUN apk add --no-cache tini \
    && mkdir -p /etc/traefik/sm /var/log/traefik
COPY --from=build /sm-proxy-agent /usr/local/bin/sm-proxy-agent
ENV SM_PROXY_KIND=traefik
LABEL org.opencontainers.image.title="Supabase Manager proxy (Traefik)"
STOPSIGNAL SIGTERM
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/sm-proxy-agent"]
CMD []
