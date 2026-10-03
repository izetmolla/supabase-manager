#!/usr/bin/env bash
# release.sh — bump the version of a service, tag it in git, publish the Docker image and push the tag.
#
# Usage:
#   ./script/release.sh <patch|minor|major|X.Y.Z> [service]     tag + publish image + push tag
#   PUBLISH=0 ./script/release.sh minor                         tag + push tag only
#   ./script/release.sh --current [service]                     print the current release version
#
# Tags are <service>/vX.Y.Z (e.g. manager/v1.4.0); images get :X.Y.Z and :latest.
#
# Environment:
#   PUBLISH=0        skip building and pushing the Docker image
#   PUSH_TAG=0       create the tag locally only
#   GIT_REMOTE       remote to push the tag to (default: origin)
#   ALLOW_DIRTY=1    allow uncommitted changes (the tag then does not match what was built)
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=script/docker-common.sh
source "$ROOT_DIR/script/docker-common.sh"

GIT_REMOTE="${GIT_REMOTE:-origin}"

usage() {
	sed -n '2,15p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

# Highest X.Y.Z among the service's tags, 0.0.0 when there are none.
current_version() {
	local prefix="$1" v
	v="$(git -C "$ROOT_DIR" tag -l "${prefix}*" |
		sed "s|^${prefix}||" |
		grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' |
		sort -t. -k1,1n -k2,2n -k3,3n |
		tail -n 1 || true)"
	printf '%s' "${v:-0.0.0}"
}

next_version() {
	local current="$1" bump="$2" major minor patch
	IFS=. read -r major minor patch <<<"$current"
	case "$bump" in
		major) printf '%d.0.0' $((major + 1)) ;;
		minor) printf '%d.%d.0' "$major" $((minor + 1)) ;;
		patch) printf '%d.%d.%d' "$major" "$minor" $((patch + 1)) ;;
		*)
			if [[ "$bump" =~ ^v?([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
				printf '%s' "${BASH_REMATCH[1]}"
			else
				echo "Error: expected patch, minor, major or X.Y.Z, got '$bump'" >&2
				return 1
			fi
			;;
	esac
}

main() {
	local bump="${1:-}" service="${2:-manager}"
	if [[ -z "$bump" || "$bump" == "-h" || "$bump" == "--help" ]]; then
		usage
		exit 0
	fi

	local prefix
	prefix="$(service_tag_prefix "$service")" || {
		echo "Error: unknown service '$service'" >&2
		exit 1
	}
	git -C "$ROOT_DIR" rev-parse --git-dir >/dev/null 2>&1 || {
		echo "Error: $ROOT_DIR is not inside a git repository" >&2
		exit 1
	}

	if [[ "$bump" == "--current" ]]; then
		current_version "$prefix"
		echo
		exit 0
	fi

	local current version tag
	current="$(current_version "$prefix")"
	version="$(next_version "$current" "$bump")"
	tag="${prefix}${version}"

	if git -C "$ROOT_DIR" rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
		echo "Error: tag $tag already exists" >&2
		exit 1
	fi
	if [[ "${ALLOW_DIRTY:-0}" != "1" && -n "$(git -C "$ROOT_DIR" status --porcelain -- .)" ]]; then
		echo "Error: uncommitted changes in $ROOT_DIR; commit them first (or ALLOW_DIRTY=1)" >&2
		git -C "$ROOT_DIR" status --short -- . | head -n 20 >&2
		exit 1
	fi
	if [[ "${PUSH_TAG:-1}" == "1" ]] && ! git -C "$ROOT_DIR" remote get-url "$GIT_REMOTE" >/dev/null 2>&1; then
		echo "Error: git remote '$GIT_REMOTE' not found (set GIT_REMOTE or PUSH_TAG=0)" >&2
		exit 1
	fi

	echo "==> ${service}: ${current} -> ${version} (tag ${tag} on $(git_commit_sha "$ROOT_DIR"))"
	git -C "$ROOT_DIR" tag -a "$tag" -m "${service} ${version}"

	if [[ "${PUBLISH:-1}" == "1" ]]; then
		# docker-publish.sh pushes the tag after the images. Drop the tag again when the image
		# cannot be published, so the version can be retried.
		if ! VERSION="$version" "$ROOT_DIR/script/docker-publish.sh" "$service"; then
			git -C "$ROOT_DIR" tag -d "$tag" >/dev/null
			echo "Error: publish failed; removed tag $tag" >&2
			exit 1
		fi
	elif [[ "${PUSH_TAG:-1}" == "1" ]]; then
		echo "==> Pushing tag ${tag} to ${GIT_REMOTE}"
		git -C "$ROOT_DIR" push "$GIT_REMOTE" "refs/tags/$tag"
	fi

	echo "Released ${service} ${version}"
}

main "$@"
