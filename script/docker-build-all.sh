#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=script/docker-common.sh
source "$ROOT_DIR/script/docker-common.sh"

echo "Building all Supabase Manager Docker images..."
echo "Registry: $(docker_registry)"
echo "Prefix:   $(docker_image_prefix)"
echo "Tag:      $(docker_tag)"
echo

while IFS= read -r service; do
	[[ -n "$service" ]] || continue
	"$ROOT_DIR/script/docker-build.sh" "$service"
	echo
done < <(docker_services)

echo "All Docker images built successfully."
