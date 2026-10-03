#!/usr/bin/env bash
# Run a service under services/<name> with air (live reload) and the root .air.toml.
# Air runs from the repo root so relative paths in .env (data/manager.db, data/bin) resolve there,
# exactly like the production binary run from its working directory.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

AIR_CONFIG="${AIR_CONFIG:-$ROOT/.air.toml}"
ENV_FILE="${ENV_FILE:-$ROOT/.env}"

usage() {
	cat <<'EOF'
Usage: script/run-service.sh <service>

Runs services/<service> with air (live reload) from the repo root, loading the
repo-root .env (if present) and using the repo-root .air.toml.

Examples:
  script/run-service.sh manager

Or via Make (dynamic from services/* folder names):
  make run-manager
EOF
}

list_services() {
	local d
	for d in "$ROOT"/services/*/; do
		[[ -d "$d" ]] || continue
		[[ -f "$d/cmd/main.go" ]] || continue
		basename "$d"
	done | LC_ALL=C sort
}

main() {
	local service="${1:-}"
	case "$service" in
		""|-h|--help|help)
			usage
			echo ""
			echo "Available services:"
			local s
			for s in $(list_services); do
				echo "  make run-$s"
			done
			return 0
			;;
	esac

	local dir="$ROOT/services/$service"
	if [[ ! -d "$dir" ]]; then
		echo "Error: unknown service '$service' (expected folder services/$service)" >&2
		echo "" >&2
		echo "Available:" >&2
		list_services | sed 's/^/  /' >&2
		return 1
	fi

	if [[ ! -f "$dir/cmd/main.go" ]]; then
		echo "Error: missing $dir/cmd/main.go" >&2
		return 1
	fi

	if [[ ! -f "$AIR_CONFIG" ]]; then
		echo "Error: air config not found at $AIR_CONFIG" >&2
		return 1
	fi

	local air_bin
	air_bin="$(command -v air || true)"
	if [[ -z "$air_bin" && -x "$(go env GOPATH)/bin/air" ]]; then
		air_bin="$(go env GOPATH)/bin/air"
	fi
	if [[ -z "$air_bin" ]]; then
		echo "Error: air not installed." >&2
		echo "Install: go install github.com/air-verse/air@latest" >&2
		echo "Or run without live reload: make dev-api" >&2
		return 1
	fi

	# The embedded UI must exist for the build; a placeholder is enough with the Vite dev server.
	if [[ ! -f "$dir/web/dist/index.html" && -f "$dir/web/embed.go" ]]; then
		mkdir -p "$dir/web/dist"
		echo '<!doctype html><p>Run "make web" to build the UI.</p>' >"$dir/web/dist/index.html"
	fi

	echo "=== run-$service ==="
	echo "  service : services/$service"
	echo "  entry   : services/$service/cmd"
	echo "  env     : $( [[ -f "$ENV_FILE" ]] && echo "$ENV_FILE" || echo "(none — defaults)" )"
	echo "  air     : $AIR_CONFIG"
	echo ""

	if [[ -f "$ENV_FILE" ]]; then
		set -a
		# shellcheck disable=SC1090
		source "$ENV_FILE"
		set +a
	fi

	exec "$air_bin" -c "$AIR_CONFIG" \
		--build.cmd "go build -o ./tmp/$service ./services/$service/cmd" \
		--build.bin "./tmp/$service" \
		--build.entrypoint "./tmp/$service"
}

main "$@"
