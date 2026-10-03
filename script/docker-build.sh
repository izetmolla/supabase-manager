#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=script/docker-common.sh
source "$ROOT_DIR/script/docker-common.sh"

usage() {
	cat <<EOF
Usage: ./script/docker-build.sh <service>

Services:
$(docker_services | sed 's/^/  /')

Environment variables:
  DOCKER_REGISTRY      (default: izetmolla)
  DOCKER_IMAGE_PREFIX  (default: supabase → izetmolla/supabase-<service>)
  DOCKER_TAG           (default: latest; also tags :\$VERSION from git/VERSION)
  DOCKER_PLATFORM      (default: linux/amd64)
  DOCKER_BUILD_NETWORK (default: host; use bridge if host networking is unavailable)
  SUPABASE_CLI_VERSION (optional; Supabase CLI bundled into the image, Dockerfile default otherwise)
  VERSION              (optional override)
  COMMIT_SHA           (optional override)
EOF
}

build_service() {
	local service="$1"
	local service_path
	service_path="$(service_dir "$service")" || {
		echo "Error: unknown service '$service'" >&2
		usage >&2
		exit 1
	}
	local dockerfile="$ROOT_DIR/$service_path/Dockerfile"
	local version commit image version_image

	if ! service_image_suffix "$service" >/dev/null; then
		echo "Error: unknown service '$service'" >&2
		usage >&2
		exit 1
	fi

	if [[ ! -f "$dockerfile" ]]; then
		echo "Error: Dockerfile not found at $dockerfile" >&2
		exit 1
	fi

	version="${VERSION:-$(resolve_service_version "$service" "$ROOT_DIR")}"
	commit="${COMMIT_SHA:-$(git_commit_sha "$ROOT_DIR")}"
	image="$(docker_image_name "$service" "$(docker_tag)")"
	version_image="$(docker_image_name "$service" "$version")"

	local extra_args=()
	if [[ -n "${SUPABASE_CLI_VERSION:-}" ]]; then
		extra_args+=(--build-arg "SUPABASE_CLI_VERSION=${SUPABASE_CLI_VERSION}")
	fi

	echo "==> Building ${image} (version=${version} commit=${commit})"
	# BuildKit enables go module / build cache mounts in the Dockerfiles.
	DOCKER_BUILDKIT=1 docker build \
		--network="${DOCKER_BUILD_NETWORK:-host}" \
		--platform "$(docker_platform)" \
		--build-arg "VERSION=${version}" \
		--build-arg "COMMIT_SHA=${commit}" \
		"${extra_args[@]}" \
		-f "$dockerfile" \
		-t "$image" \
		-t "$version_image" \
		"$ROOT_DIR"

	echo "==> Built ${image} and ${version_image}"
}

main() {
	local service="${1:-}"

	if [[ -z "$service" || "$service" == "-h" || "$service" == "--help" ]]; then
		usage
		exit 0
	fi

	build_service "$service"
}

main "$@"
