#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=script/docker-common.sh
source "$ROOT_DIR/script/docker-common.sh"

service="${1:-}"
if [[ -z "$service" ]]; then
	echo "Usage: $0 <service>" >&2
	exit 1
fi

"$ROOT_DIR/script/docker-build.sh" "$service"

version="${VERSION:-$(resolve_service_version "$service" "$ROOT_DIR")}"
image="$(docker_image_name "$service" "$(docker_tag)")"
version_image="$(docker_image_name "$service" "$version")"

docker tag "$version_image" "$image"

echo "Pushing ${image}"
docker push "$image"

echo "Pushing ${version_image}"
docker push "$version_image"

echo "Published ${image} and ${version_image}"

# The manager starts proxy containers from images with its own version tag.
if [[ "$service" == "manager" ]]; then
	VERSION="$version" "$ROOT_DIR/script/docker-proxy-images.sh" publish
fi

# Publish the matching git tag too (make docker-publish / docker-deploy / release-*).
tag="$(service_tag_prefix "$service")${version}"
if [[ "${PUSH_TAG:-1}" == "1" ]] && git -C "$ROOT_DIR" rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
	remote="${GIT_REMOTE:-origin}"
	if git -C "$ROOT_DIR" remote get-url "$remote" >/dev/null 2>&1; then
		echo "Pushing git tag ${tag} to ${remote}"
		git -C "$ROOT_DIR" push "$remote" "refs/tags/$tag"
	else
		echo "Warning: git remote '$remote' not found; tag ${tag} not pushed" >&2
	fi
fi
