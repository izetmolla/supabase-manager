#!/usr/bin/env bash
# docker-proxy-images.sh — build or publish the Proxy Manager images (nginx and Traefik with
# sm-proxy-agent). They carry the manager's version, which the manager uses as its default tag.
#
#   ./script/docker-proxy-images.sh build      build :$VERSION and :$DOCKER_TAG locally
#   ./script/docker-proxy-images.sh publish    build and push both tags, plus :latest (PUSH_LATEST=0 skips it)
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=script/docker-common.sh
source "$ROOT_DIR/script/docker-common.sh"

action="${1:-build}"
version="${VERSION:-$(resolve_service_version manager "$ROOT_DIR")}"
commit="${COMMIT_SHA:-$(git_commit_sha "$ROOT_DIR")}"

proxy_image() {
	printf '%s/%s-manager-proxy-%s:%s' "$(docker_registry)" "$(docker_image_prefix)" "$1" "$2"
}

for kind in nginx traefik; do
	image="$(proxy_image "$kind" "$(docker_tag)")"
	version_image="$(proxy_image "$kind" "$version")"
	echo "==> Building ${version_image}"
	DOCKER_BUILDKIT=1 docker build \
		--network="${DOCKER_BUILD_NETWORK:-host}" \
		--platform "$(docker_platform)" \
		--build-arg "VERSION=${version}" \
		--build-arg "COMMIT_SHA=${commit}" \
		--target "$kind" \
		-f "$ROOT_DIR/services/proxy-agent/proxy.Dockerfile" \
		-t "$image" \
		-t "$version_image" \
		"$ROOT_DIR"
	if [[ "$action" == "publish" ]]; then
		echo "Pushing ${image} and ${version_image}"
		docker push "$image"
		docker push "$version_image"
		latest_image="$(proxy_image "$kind" latest)"
		if [[ "${PUSH_LATEST:-1}" == "1" && "$latest_image" != "$image" && "$latest_image" != "$version_image" ]]; then
			docker tag "$version_image" "$latest_image"
			echo "Pushing ${latest_image}"
			docker push "$latest_image"
		fi
	fi
done
