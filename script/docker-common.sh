#!/usr/bin/env bash

# Shared helpers for Supabase Manager Docker image builds and versioning.

docker_registry() {
	printf '%s' "${DOCKER_REGISTRY:-izetmolla}"
}

# Image: $(registry)/$(prefix)-$(service):tag → izetmolla/supabase-manager:latest
docker_image_prefix() {
	printf '%s' "${DOCKER_IMAGE_PREFIX:-supabase}"
}

docker_tag() {
	printf '%s' "${DOCKER_TAG:-latest}"
}

docker_platform() {
	printf '%s' "${DOCKER_PLATFORM:-linux/amd64}"
}

# Services that have a Dockerfile under services/<name>/.
docker_services() {
	local root svc
	root="$(repo_root)"
	for svc in $(ls -1 "$root/services" 2>/dev/null | LC_ALL=C sort); do
		if [[ -f "$root/services/$svc/Dockerfile" ]]; then
			printf '%s\n' "$svc"
		fi
	done
}

service_dir() {
	local root
	root="$(repo_root)"
	if [[ -d "$root/services/$1" ]]; then
		printf 'services/%s' "$1"
		return 0
	fi
	return 1
}

service_image_suffix() {
	if [[ -f "$(repo_root)/services/$1/Dockerfile" ]]; then
		printf '%s' "$1"
		return 0
	fi
	return 1
}

service_tag_prefix() {
	if [[ -f "$(repo_root)/services/$1/Dockerfile" ]]; then
		printf '%s/v' "$1"
		return 0
	fi
	return 1
}

service_k8s_deployment() {
	case "$1" in
		manager) printf 'supabase-manager' ;;
		*) printf '%s' "$1" ;;
	esac
}

# Resolve kubectl and KUBECONFIG for cluster deploy scripts.
setup_kubectl() {
	if [[ -z "${KUBECONFIG:-}" ]]; then
		for cfg in \
			/root/.kube/containerws/configs/*.yaml \
			/root/.kube/config \
			"$HOME"/.kube/config
		do
			[[ -f "$cfg" ]] || continue
			export KUBECONFIG="$cfg"
			break
		done
	fi

	if [[ -n "${KUBECTL:-}" && -x "${KUBECTL}" ]]; then
		export KUBECTL
		return 0
	fi

	if command -v kubectl >/dev/null 2>&1; then
		KUBECTL="$(command -v kubectl)"
	elif [[ -x /usr/local/bin/kubectl ]]; then
		KUBECTL=/usr/local/bin/kubectl
	elif [[ -x /usr/bin/kubectl ]]; then
		KUBECTL=/usr/bin/kubectl
	else
		echo "kubectl not found on PATH or in /usr/local/bin." >&2
		echo "Install kubectl, or set KUBECTL=/path/to/kubectl" >&2
		echo "To only build+push (skip rollout): make docker-publish service=<svc>" >&2
		return 1
	fi
	export KUBECTL
}

repo_root() {
	printf '%s' "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
}

default_version() {
	local root="$1"
	if [[ -f "$root/VERSION" ]]; then
		tr -d '[:space:]' <"$root/VERSION"
		return 0
	fi
	git -C "$root" describe --tags --always --dirty 2>/dev/null || printf '0.0.0-dev'
}

git_commit_sha() {
	git -C "${1:-.}" rev-parse --short HEAD 2>/dev/null || printf 'unknown'
}

resolve_service_version() {
	local service="$1"
	local root="${2:-$(repo_root)}"
	local prefix version

	prefix="$(service_tag_prefix "$service")" || return 1
	# X.Y.Z only on the tagged commit; later commits become X.Y.Z-<n>-g<sha>, so publishing them
	# never overwrites a release image (and the panel's update check ignores them).
	version="$(git -C "$root" tag --points-at HEAD -l "${prefix}*" 2>/dev/null |
		sed "s|^${prefix}||" | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' |
		sort -t. -k1,1n -k2,2n -k3,3n | tail -n 1 || true)"
	if [[ -z "$version" ]]; then
		version="$(git -C "$root" describe --tags --match="${prefix}*" 2>/dev/null | sed "s|^${prefix}||" || true)"
	fi
	if [[ -z "$version" ]]; then
		version="$(default_version "$root")"
	fi
	printf '%s' "$version"
}

docker_image_name() {
	local service="$1"
	local tag="${2:-$(docker_tag)}"
	local suffix

	suffix="$(service_image_suffix "$service")" || return 1
	printf '%s/%s-%s:%s' \
		"$(docker_registry)" \
		"$(docker_image_prefix)" \
		"$suffix" \
		"$tag"
}
