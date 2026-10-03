# ==============================================================================
# Supabase Manager — workspace Makefile
# One service (services/manager): API + embedded UI (frontend/apps/manager) in a single binary.
# ==============================================================================

.PHONY: help all fmt tidy update fix vet lint test list-modules upgrade run clean \
	dev dev-api dev-web web api build install-frontend run-frontend \
	docker-build docker-build-all docker-publish docker-deploy docker-deploy-all \
	docker-up docker-down docker-restart docker-logs docker-shell docker-cli \
	docker-users docker-user docker-passwd docker-import-local docker-backup \
	compose-up compose-down \
	k8s-apply \
	version bump-patch bump-minor bump-major release-patch release-minor release-major release \
	release-next next proto proto-tools test-integration proxy-images proxy-images-publish \
	frontend-check check deploy-all deploy-all-next deploy-all-run

GO_MODULES_SCRIPT := ./script/go-modules.sh
UPGRADE_GO_SCRIPT := ./script/upgrade-go.sh
RUN_SERVICE_SCRIPT := ./script/run-service.sh
FRONTEND_DIR := frontend

# The single service and its frontend app.
SERVICE      := manager
SERVICE_DIR  := services/$(SERVICE)
FRONTEND_APP := manager
UI_DIST      := $(SERVICE_DIR)/web/dist
BIN          := bin/supabase-manager

# Dynamic service names from services/<name>/ folders that contain cmd/main.go.
SERVICES := $(sort $(notdir $(patsubst %/cmd/main.go,%,$(wildcard services/*/cmd/main.go))))

# Services that have a Dockerfile and can be published as container images.
DOCKER_SERVICES := $(sort $(notdir $(patsubst %/Dockerfile,%,$(wildcard services/*/Dockerfile))))

# Frontend SPA apps from frontend/apps/<name>/ (must have package.json).
FRONTEND_APPS := $(sort $(notdir $(patsubst %/,%,$(wildcard $(FRONTEND_DIR)/apps/*/))))

VERSION    ?= $(shell v=$$(git describe --tags --match '$(SERVICE)/v*' --dirty 2>/dev/null) && echo $${v#$(SERVICE)/v} || echo 0.0.0-dev)
COMMIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS    := -s -w -X github.com/supabase-manager/version.Version=$(VERSION) -X github.com/supabase-manager/version.CommitSHA=$(COMMIT_SHA)

# Image: $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_PREFIX)-<service>:$(DOCKER_TAG) → izetmolla/supabase-manager:latest
DOCKER_REGISTRY      ?= izetmolla
DOCKER_IMAGE_PREFIX  ?= supabase
DOCKER_TAG           ?= latest
DOCKER_PLATFORM      ?= linux/amd64
NAMESPACE            ?= supabase-manager
SUPABASE_CLI_VERSION ?= 2.119.0
service              ?= $(SERVICE)

# Code generators for make proto (installed into $(GOBIN) when missing).
GOBIN                      := $(shell go env GOPATH 2>/dev/null)/bin
BUF_VERSION                ?= v1.73.0
PROTOC_GEN_GO_VERSION      ?= v1.36.12
PROTOC_GEN_GO_GRPC_VERSION ?= v1.6.2

# Runtime (docker-up / compose)
IMAGE         ?= $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_PREFIX)-$(SERVICE):$(DOCKER_TAG)
CONTAINER     ?= supabase-manager
DATA_VOLUME   ?= supabase-manager-data
PROJECTS_ROOT ?= /etc/supabase-manager/projects
DOCKER_SOCKET ?= /var/run/docker.sock
ADDR          ?= 127.0.0.1:8080
PUID          ?= $(shell id -u)
PGID          ?= $(shell id -g)
DOCKER_GID    ?= $(shell stat -c %g $(DOCKER_SOCKET) 2>/dev/null || echo 999)

DOCKER_ENV := DOCKER_REGISTRY=$(DOCKER_REGISTRY) DOCKER_IMAGE_PREFIX=$(DOCKER_IMAGE_PREFIX) \
	DOCKER_TAG=$(DOCKER_TAG) DOCKER_PLATFORM=$(DOCKER_PLATFORM) SUPABASE_CLI_VERSION=$(SUPABASE_CLI_VERSION)

all: help

## help: Show available targets
help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Build & run (single binary, UI embedded — no CDN, no gateway):"
	@echo "  make install-frontend  pnpm install in $(FRONTEND_DIR)/"
	@echo "  make web               Build $(FRONTEND_DIR)/apps/$(FRONTEND_APP) into $(UI_DIST) (embedded by go:embed)"
	@echo "  make api               Build $(BIN) from $(SERVICE_DIR)/cmd"
	@echo "  make build             web + api"
	@echo "  make dev               API (go run, :8080) + Vite dev server (:5173, proxies /api)"
	@echo "  make run-<service>     Run a service with air (live reload), root .env and .air.toml"
	@echo "  make run-frontend <app> Vite dev server for one app under $(FRONTEND_DIR)/apps"
	@echo "  make clean             Remove bin/, tmp/ and the built UI"
	@echo ""
	@echo "Go modules (services/, shared/):"
	@echo "  fmt | tidy | update | fix | vet | lint | test | list-modules | upgrade"
	@echo ""
	@echo "Docker image (script/):"
	@echo "  make docker-build [service=$(SERVICE)]    Build $(IMAGE) (+ :version)"
	@echo "  make docker-build-all                    Build all service images"
	@echo "  make docker-publish [service=$(SERVICE)]  Build and push :latest and :version"
	@echo "  make docker-deploy [service=$(SERVICE)]   Publish and kubectl rollout restart (namespace $(NAMESPACE))"
	@echo "  Env: DOCKER_REGISTRY=$(DOCKER_REGISTRY) DOCKER_IMAGE_PREFIX=$(DOCKER_IMAGE_PREFIX) DOCKER_TAG=$(DOCKER_TAG) SUPABASE_CLI_VERSION=$(SUPABASE_CLI_VERSION)"
	@echo ""
	@echo "Versioning (git tags $(SERVICE)/vX.Y.Z, images :X.Y.Z and :latest):"
	@echo "  make version                          Current release version"
	@echo "  make release-patch|minor|major        Bump, tag, build + push the image, push the git tag"
	@echo "  make release next                     update, fix, tidy, vet, lint, commit, then release-patch"
	@echo "  make release V=1.4.0                  Same with an explicit version"
	@echo "  make deploy-all                       Build and push every image (+ proxy images) at the current version and as :latest, no new tag"
	@echo "  make deploy-all next                  update, fix, fmt, tidy, proto, vet, lint, tests, build, integration tests,"
	@echo "                                        commit, then release-patch every service (images + proxy images to Docker Hub)"
	@echo "  make deploy-all V=1.4.0               Same with an explicit version (SKIP_INTEGRATION=1 skips the Docker tests)"
	@echo "  make check                            vet, lint, test and the frontend lint/typecheck"
	@echo "  make bump-patch|minor|major           Bump and push the git tag only (no image)"
	@echo ""
	@echo "Container operations:"
	@echo "  make docker-up | docker-down | docker-restart | docker-logs | docker-shell"
	@echo "  make docker-users | docker-user U=<email> | docker-passwd U=<email> | docker-cli ARGS=\"...\""
	@echo "  make docker-backup | docker-import-local"
	@echo "  make compose-up | compose-down"
	@echo ""
	@echo "Kubernetes (k8s/):"
	@echo "  make k8s-apply         Namespace + supabase-manager (hostNetwork, docker socket)"
	@echo ""
	@echo "Services (make run-<name>):"
	@for s in $(SERVICES); do echo "  make run-$$s"; done
	@echo "Frontend apps: $(FRONTEND_APPS)"
	@echo ""
	@echo "Ports: see docs/PORTS.md (manager :8080, project blocks from 54300)"

# ==============================================================================
# BUILD
# ==============================================================================

$(FRONTEND_DIR)/node_modules/.modules.yaml: $(FRONTEND_DIR)/package.json $(FRONTEND_DIR)/pnpm-lock.yaml
	cd $(FRONTEND_DIR) && pnpm install --frozen-lockfile
	@touch $@

## install-frontend: Install frontend workspace dependencies
install-frontend:
	cd $(FRONTEND_DIR) && pnpm install

## web: Build the UI into services/manager/web/dist
web: $(FRONTEND_DIR)/node_modules/.modules.yaml
	cd $(FRONTEND_DIR) && pnpm --filter $(FRONTEND_APP) build

# go:embed needs at least one file; a placeholder is enough while the Vite dev server serves the UI.
$(UI_DIST)/index.html:
	@mkdir -p $(UI_DIST) && [ -f $@ ] || echo '<!doctype html><p>Run "make web" to build the UI.</p>' > $@

## api: Build the static binary (UI embedded)
api: $(UI_DIST)/index.html
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./$(SERVICE_DIR)/cmd

## build: Build the UI, then the binary
build: web api

## dev: API + Vite dev server
dev:
	@$(MAKE) -j2 dev-api dev-web

dev-api: $(UI_DIST)/index.html
	go run ./$(SERVICE_DIR)/cmd

dev-web: $(FRONTEND_DIR)/node_modules/.modules.yaml
	cd $(FRONTEND_DIR) && pnpm --filter $(FRONTEND_APP) dev

## clean: Remove build output
clean:
	rm -rf bin tmp $(UI_DIST)

# ==============================================================================
# GO MODULES
# ==============================================================================

list-modules:
	@$(GO_MODULES_SCRIPT) list

fmt:
	@$(GO_MODULES_SCRIPT) fmt

tidy:
	@$(GO_MODULES_SCRIPT) tidy

update:
	@$(GO_MODULES_SCRIPT) update

fix:
	@$(GO_MODULES_SCRIPT) fix

## vet: go vet ./... in every module
vet: $(UI_DIST)/index.html
	@$(GO_MODULES_SCRIPT) vet

lint: $(UI_DIST)/index.html
	@$(GO_MODULES_SCRIPT) lint

## test: go test on every module in go.work
test: $(UI_DIST)/index.html
	go test $(addsuffix /...,$(addprefix ./,$(shell go list -m -f '{{.Dir}}' | sed 's|^$(CURDIR)/||')))

## proto-tools: install buf, protoc-gen-go and protoc-gen-go-grpc when missing
proto-tools:
	@test -x "$(GOBIN)/buf" || go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	@test -x "$(GOBIN)/protoc-gen-go" || go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	@test -x "$(GOBIN)/protoc-gen-go-grpc" || go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

## proto: lint the proto files and regenerate the proxy agent gRPC code
proto: proto-tools
	cd shared/proxyagent && PATH="$$(go env GOPATH)/bin:$$PATH" buf lint && PATH="$$(go env GOPATH)/bin:$$PATH" buf generate

## test-integration: nginx -t / Traefik on rendered configs and the proxy images end to end (needs Docker)
test-integration:
	go test -tags integration -count=1 ./services/manager/internal/proxy-manager/...

## frontend-check: oxlint and TypeScript over the frontend workspace
frontend-check: $(FRONTEND_DIR)/node_modules/.modules.yaml
	cd $(FRONTEND_DIR) && pnpm lint && pnpm typecheck

## check: every static check and unit test (Go and frontend)
check:
	@$(MAKE) --no-print-directory vet
	@$(MAKE) --no-print-directory lint
	@$(MAKE) --no-print-directory test
	@$(MAKE) --no-print-directory frontend-check

## upgrade: Fetch latest Go from go.dev and update go.mod, go.work, Dockerfiles
upgrade:
	@$(UPGRADE_GO_SCRIPT)

## run: Show available make run-<service> targets
run:
	@$(RUN_SERVICE_SCRIPT) --help

# Dynamic run-<service> targets from services/*/cmd/main.go.
define RUN_SERVICE_RULE
.PHONY: run-$(1)
run-$(1):
	@$(RUN_SERVICE_SCRIPT) $(1)
endef

$(foreach s,$(SERVICES),$(eval $(call RUN_SERVICE_RULE,$(s))))

# ==============================================================================
# FRONTEND
# ==============================================================================

# Allow positional app name: make run-frontend manager
ifeq (run-frontend,$(firstword $(MAKECMDGOALS)))
  RUN_FRONTEND_APP := $(word 2,$(MAKECMDGOALS))
  ifneq ($(RUN_FRONTEND_APP),)
    .PHONY: $(RUN_FRONTEND_APP)
    $(RUN_FRONTEND_APP):
	@:
  endif
endif

## run-frontend: Vite dev server for one SPA under frontend/apps (default: manager)
run-frontend:
	@command -v pnpm >/dev/null 2>&1 || (echo "Error: pnpm not found in PATH"; exit 1)
	@app="$(or $(RUN_FRONTEND_APP),$(FRONTEND_APP))"; \
	if [ ! -f "$(FRONTEND_DIR)/apps/$$app/package.json" ]; then \
		echo "Error: unknown frontend app '$$app'. Available: $(FRONTEND_APPS)" >&2; exit 1; \
	fi; \
	pkg=$$(sed -n 's/^[[:space:]]*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$(FRONTEND_DIR)/apps/$$app/package.json" | head -n 1); \
	echo "=== run-frontend: $$app (pnpm --filter $$pkg dev) ==="; \
	cd "$(FRONTEND_DIR)" && pnpm --filter "$$pkg" dev

# ==============================================================================
# DOCKER IMAGE (script/)
# ==============================================================================

docker-build:
	@$(DOCKER_ENV) ./script/docker-build.sh $(service)

docker-build-all:
	@$(DOCKER_ENV) ./script/docker-build-all.sh

docker-publish:
	@$(DOCKER_ENV) ./script/docker-publish.sh $(service)

## proxy-images: build the Proxy Manager images (nginx and Traefik with sm-proxy-agent)
proxy-images:
	@$(DOCKER_ENV) ./script/docker-proxy-images.sh build

## proxy-images-publish: build and push the Proxy Manager images
proxy-images-publish:
	@$(DOCKER_ENV) ./script/docker-proxy-images.sh publish

docker-deploy:
	@$(DOCKER_ENV) NAMESPACE=$(NAMESPACE) ./script/docker-deploy.sh $(service)

docker-deploy-all:
	@for s in $(DOCKER_SERVICES); do $(DOCKER_ENV) NAMESPACE=$(NAMESPACE) ./script/docker-deploy.sh $$s || exit 1; done

# ==============================================================================
# VERSIONING (script/release.sh)
# ==============================================================================

version:
	@./script/release.sh --current $(service)

bump-patch bump-minor bump-major:
	@PUBLISH=0 ./script/release.sh $(subst bump-,,$@) $(service)

release-patch release-minor release-major:
	@$(DOCKER_ENV) ./script/release.sh $(subst release-,,$@) $(service)

# make release V=1.4.0 | make release next
release:
ifneq ($(filter next,$(MAKECMDGOALS)),)
	@$(MAKE) --no-print-directory release-next
else
	@test -n "$(V)" || (echo "Usage: make release V=X.Y.Z, make release next (or make release-patch|minor|major)" >&2; exit 1)
	@$(DOCKER_ENV) ./script/release.sh $(V) $(service)
endif

# Only meaningful as "make release next" or "make deploy-all next".
next:
	@:

# The checks may rewrite go.mod/go.sum and sources; those changes are committed before tagging,
# so the tree must be clean at the start to keep unrelated work out of that commit.
release-next:
	@if [ "$(ALLOW_DIRTY)" != "1" ] && [ -n "$$(git status --porcelain -- .)" ]; then \
		echo "Error: uncommitted changes; commit them before 'make release next' (or ALLOW_DIRTY=1)" >&2; \
		git status --short -- . | head -n 20 >&2; exit 1; \
	fi
	@echo "==> Pre-release checks: update, fix, tidy, vet, lint"
	@$(MAKE) --no-print-directory update
	@$(MAKE) --no-print-directory fix
	@$(MAKE) --no-print-directory tidy
	@$(MAKE) --no-print-directory vet
	@$(MAKE) --no-print-directory lint
	@if [ "$(ALLOW_DIRTY)" != "1" ] && [ -n "$$(git status --porcelain -- .)" ]; then \
		echo "==> Committing changes from the pre-release checks"; \
		git status --short -- .; \
		git add -A -- . && git commit -q -m "chore(release): update dependencies and apply go fix"; \
	fi
	@$(DOCKER_ENV) ./script/release.sh patch $(service)

# make deploy-all: build and push every image at the current version (no checks, no new tag)
# make deploy-all next | make deploy-all V=1.4.0: full pipeline and a new release
deploy-all:
ifneq ($(filter next,$(MAKECMDGOALS)),)
	@$(MAKE) --no-print-directory deploy-all-run BUMP=patch
else ifneq ($(V),)
	@$(MAKE) --no-print-directory deploy-all-run BUMP=$(V)
else
	@docker info >/dev/null 2>&1 || (echo "Error: the Docker daemon is not reachable" >&2; exit 1)
	@echo "==> Building and publishing $(DOCKER_SERVICES) at version $$(bash -c 'source ./script/docker-common.sh && resolve_service_version manager "$$PWD"')"
	@for s in $(DOCKER_SERVICES); do $(DOCKER_ENV) ./script/docker-publish.sh $$s || exit 1; done
endif

deploy-all-next:
	@$(MAKE) --no-print-directory deploy-all-run BUMP=patch

# Full pipeline: checks that may rewrite the tree, checks that must pass, builds, integration
# tests against Docker, a commit of what the checks changed, then a release of every service
# (git tag, images and the Proxy Manager images pushed to Docker Hub, tag pushed to GIT_REMOTE).
deploy-all-run:
	@test -n "$(BUMP)" || (echo "Error: BUMP is not set; use make deploy-all next" >&2; exit 1)
	@if [ "$(ALLOW_DIRTY)" != "1" ] && [ -n "$$(git status --porcelain -- .)" ]; then \
		echo "Error: uncommitted changes; commit them before 'make deploy-all' (or ALLOW_DIRTY=1)" >&2; \
		git status --short -- . | head -n 20 >&2; exit 1; \
	fi
	@docker info >/dev/null 2>&1 || (echo "Error: the Docker daemon is not reachable" >&2; exit 1)
	@if [ "$(PUSH_TAG)" != "0" ] && ! git remote get-url "$(or $(GIT_REMOTE),origin)" >/dev/null 2>&1; then \
		echo "Error: git remote '$(or $(GIT_REMOTE),origin)' not found (set GIT_REMOTE or PUSH_TAG=0)" >&2; exit 1; \
	fi
	@echo "==> [1/6] Update and rewrite: update, fix, fmt, tidy, proto"
	@$(MAKE) --no-print-directory update
	@$(MAKE) --no-print-directory fix
	@$(MAKE) --no-print-directory fmt
	@$(MAKE) --no-print-directory tidy
	@$(MAKE) --no-print-directory proto
	@echo "==> [2/6] Checks: vet, lint, test, frontend lint and typecheck"
	@$(MAKE) --no-print-directory check
	@echo "==> [3/6] Build: UI and binary"
	@$(MAKE) --no-print-directory build
	@if [ "$(SKIP_INTEGRATION)" = "1" ]; then \
		echo "==> [4/6] Integration tests skipped (SKIP_INTEGRATION=1)"; \
	else \
		echo "==> [4/6] Integration tests: proxy images and Docker"; \
		$(MAKE) --no-print-directory proxy-images && $(MAKE) --no-print-directory test-integration || exit 1; \
	fi
	@if [ "$(ALLOW_DIRTY)" != "1" ] && [ -n "$$(git status --porcelain -- .)" ]; then \
		echo "==> [5/6] Committing changes from the checks"; \
		git status --short -- .; \
		git add -A -- . && git commit -q -m "chore(release): update dependencies, regenerate code and apply go fix"; \
	else \
		echo "==> [5/6] Nothing to commit"; \
	fi
	@echo "==> [6/6] Release and publish: $(DOCKER_SERVICES) ($(BUMP))"
	@for s in $(DOCKER_SERVICES); do $(DOCKER_ENV) ./script/release.sh $(BUMP) $$s || exit 1; done

# ==============================================================================
# CONTAINER OPERATIONS
# ==============================================================================

# Docker creates PROJECTS_ROOT on the host if missing; only its top level is chowned because
# project folders may hold database files owned by container users.
docker-up:
	docker volume create $(DATA_VOLUME) >/dev/null
	docker run --rm -v $(DATA_VOLUME):/data -v $(PROJECTS_ROOT):/projects alpine sh -c \
		'mkdir -p /data/home /data/bin && chown -R $(PUID):$(PGID) /data && chown $(PUID):$(PGID) /projects && chmod 0755 /projects'
	-docker rm -f $(CONTAINER) >/dev/null 2>&1
	docker run -d --name $(CONTAINER) --restart unless-stopped \
		--init --network host \
		--user $(PUID):$(PGID) --group-add $(DOCKER_GID) \
		-e ADDR=$(ADDR) -e PROJECTS_ROOT=$(PROJECTS_ROOT) \
		-v $(DATA_VOLUME):/data \
		-v $(PROJECTS_ROOT):$(PROJECTS_ROOT) \
		-v $(DOCKER_SOCKET):/var/run/docker.sock \
		$(IMAGE)
	@echo "Supabase Manager: http://$(ADDR)"

docker-down:
	docker rm -f $(CONTAINER)

docker-restart:
	docker restart $(CONTAINER)

docker-logs:
	docker logs -f --tail 200 $(CONTAINER)

# The image has no shell; this opens one in a throwaway container with the same mounts.
docker-shell:
	docker run --rm -it --volumes-from $(CONTAINER) --network host -w /data alpine sh

# make docker-cli ARGS="user get admin@example.com --json"
docker-cli:
	docker exec -it $(CONTAINER) supabase-manager $(ARGS)

docker-users:
	docker exec $(CONTAINER) supabase-manager user list

# make docker-user U=admin@example.com
docker-user:
	docker exec $(CONTAINER) supabase-manager user get $(U)

# make docker-passwd U=admin@example.com
docker-passwd:
	docker exec -it $(CONTAINER) supabase-manager user set-password $(U)

# Copies ./data and the secrets from ./.env into the data volume. Stop both the local server and
# the container first, and keep PROJECTS_ROOT the same so stored project paths stay valid.
docker-import-local:
	docker volume create $(DATA_VOLUME) >/dev/null
	docker run --rm -v $(DATA_VOLUME):/data -v $(CURDIR):/src:ro alpine sh -c '\
		cp -a /src/data/. /data/ && \
		if [ -f /src/.env ]; then grep -E "^(JWT_SECRET|ENCRYPTION_KEY)=" /src/.env > /data/.env; chmod 600 /data/.env; fi'

# Writes ./backups/supabase-manager-data-<date>.tgz (database, generated secrets, managed CLI).
docker-backup:
	mkdir -p backups
	docker run --rm -v $(DATA_VOLUME):/data:ro -v $(CURDIR)/backups:/backup alpine \
		tar -czf /backup/$(DATA_VOLUME)-$$(date +%Y%m%d-%H%M%S).tgz -C /data .

compose-up:
	PUID=$(PUID) PGID=$(PGID) DOCKER_GID=$(DOCKER_GID) PROJECTS_ROOT=$(PROJECTS_ROOT) ADDR=$(ADDR) \
		docker compose up -d --build

compose-down:
	docker compose down

# ==============================================================================
# KUBERNETES
# ==============================================================================

k8s-apply:
	kubectl apply -f k8s/general.yaml
	kubectl apply -f k8s/manager.yaml
