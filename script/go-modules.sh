#!/usr/bin/env bash
# Run fmt / tidy / update / fix / lint across Go modules under shared/, pkg/, and services/.
# Only directories that contain a go.mod are considered. Modules with no .go files
# are skipped (avoids `go: warning: "./..." matched no packages`).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GOLANGCI_CONFIG="${GOLANGCI_CONFIG:-$ROOT/.golangci.yml}"
GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-v2.14.0}"
SECTIONS=(shared pkg services)

ensure_golangci_lint() {
	local bin_dir gopath
	gopath="$(go env GOPATH 2>/dev/null || true)"
	bin_dir="${gopath:+$gopath/bin}"

	# Prefer an already-installed binary; ensure GOPATH/bin is searchable.
	if [[ -n "$bin_dir" && -d "$bin_dir" && ":$PATH:" != *":$bin_dir:"* ]]; then
		export PATH="$bin_dir:$PATH"
	fi

	if command -v golangci-lint >/dev/null 2>&1; then
		return 0
	fi

	if [[ -z "$bin_dir" ]]; then
		echo "Error: cannot install golangci-lint (go env GOPATH is empty)" >&2
		return 1
	fi

	echo "golangci-lint not found — installing ${GOLANGCI_LINT_VERSION} into $bin_dir"
	mkdir -p "$bin_dir"
	curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$bin_dir" "$GOLANGCI_LINT_VERSION"
	export PATH="$bin_dir:$PATH"

	if ! command -v golangci-lint >/dev/null 2>&1; then
		echo "Error: golangci-lint install finished but binary not on PATH" >&2
		echo "Expected: $bin_dir/golangci-lint" >&2
		return 1
	fi

	golangci-lint version
}

usage() {
	cat <<'EOF'
Usage: script/go-modules.sh <command>

Commands:
  list     List discovered Go modules by section
  fmt      Run go fmt ./... in each module
  tidy     Run go mod tidy in each module
  update   Run go get -u ./... then go mod tidy in each module
  fix      Run go fix ./... in each module
  vet      Run go vet ./... in each module
  lint     Run golangci-lint with root .golangci.yml in each module

Looks under shared/, pkg/, and services/ for folders that contain go.mod.
Modules with go.mod but no .go source files are skipped for fmt/tidy/update/fix/vet/lint.
EOF
}

# Print relative module paths (shared first, then pkg, then services).
discover_modules() {
	local section
	for section in "${SECTIONS[@]}"; do
		if [[ ! -d "$ROOT/$section" ]]; then
			continue
		fi
		find "$ROOT/$section" -name go.mod 2>/dev/null \
			| sed "s|^$ROOT/||; s|/go.mod\$||" \
			| LC_ALL=C sort
	done
}

section_of() {
	case "$1" in
		shared|shared/*) echo "shared" ;;
		pkg|pkg/*) echo "pkg" ;;
		services|services/*) echo "services" ;;
		*) echo "other" ;;
	esac
}

has_go_sources() {
	local mod="$1"
	find "$ROOT/$mod" -name '*.go' -not -path '*/vendor/*' -print -quit 2>/dev/null | grep -q .
}

list_modules() {
	local mods
	mapfile -t mods < <(discover_modules)
	if [[ ${#mods[@]} -eq 0 ]]; then
		echo "  (none — no go.mod under services/, shared/, or pkg/)"
		return 0
	fi

	local last="" mod section
	for mod in "${mods[@]}"; do
		section="$(section_of "$mod")"
		if [[ "$section" != "$last" ]]; then
			echo ""
			echo "  [$section]"
			last="$section"
		fi
		if has_go_sources "$mod"; then
			echo "    $mod"
		else
			echo "    $mod  (no Go packages — skipped by fmt/tidy/update/fix/lint)"
		fi
	done
	echo ""
}

# After a Go patch bump, GOCACHE may still hold objects from the previous
# toolchain (compile: version "go1.27.0" does not match go tool version "go1.27.1").
# Clear once per active toolchain version.
ensure_go_cache_matches_toolchain() {
	command -v go >/dev/null 2>&1 || return 0
	local active cache stamp
	active="$(go version 2>/dev/null | awk '{print $3}')" || return 0
	[[ -n "$active" ]] || return 0
	cache="$(go env GOCACHE 2>/dev/null || true)"
	[[ -n "$cache" ]] || return 0
	stamp="$cache/.supabase-manager-go-version"
	if [[ -f "$stamp" ]] && [[ "$(cat "$stamp" 2>/dev/null)" == "$active" ]]; then
		return 0
	fi
	echo "  go toolchain $active — cleaning stale GOCACHE"
	go clean -cache || true
	mkdir -p "$cache"
	printf '%s\n' "$active" >"$stamp"
}

run_on_modules() {
	local action="$1"
	local mods
	mapfile -t mods < <(discover_modules)

	if [[ ${#mods[@]} -eq 0 ]]; then
		echo "No Go modules found under services/, shared/, or pkg/"
		return 0
	fi

	if [[ "$action" == "lint" ]]; then
		ensure_golangci_lint || return 1
		if [[ ! -f "$GOLANGCI_CONFIG" ]]; then
			echo "Error: missing $GOLANGCI_CONFIG"
			return 1
		fi
		echo "=== lint (config: $GOLANGCI_CONFIG) ==="
	else
		echo "=== $action ==="
	fi

	ensure_go_cache_matches_toolchain

	local last="" mod section failed=0
	for mod in "${mods[@]}"; do
		section="$(section_of "$mod")"
		if [[ "$section" != "$last" ]]; then
			echo ""
			echo "[section: $section]"
			last="$section"
		fi

		echo "  -> $mod"

		if ! has_go_sources "$mod"; then
			echo "     (skip — no Go packages)"
			continue
		fi

		case "$action" in
			fmt)
				(cd "$ROOT/$mod" && go fmt ./...) || failed=1
				;;
			tidy)
				(cd "$ROOT/$mod" && go mod tidy) || failed=1
				;;
			update)
				if ! (cd "$ROOT/$mod" && go get -u ./... && go mod tidy); then
					failed=1
				fi
				;;
			fix)
				(cd "$ROOT/$mod" && go fix ./...) || failed=1
				;;
			vet)
				(cd "$ROOT/$mod" && go vet ./...) || failed=1
				;;
			lint)
				if ! (cd "$ROOT/$mod" && golangci-lint run -c "$GOLANGCI_CONFIG" ./...); then
					failed=1
				fi
				;;
			*)
				echo "Unknown action: $action" >&2
				return 1
				;;
		esac
	done

	echo ""
	if [[ "$failed" -ne 0 ]]; then
		echo "$action failed!"
		return 1
	fi
	echo "$action completed!"
}

main() {
	local cmd="${1:-}"
	case "$cmd" in
		""|-h|--help|help)
			usage
			echo ""
			echo "Where commands run (by section):"
			list_modules
			;;
		list)
			echo "Discovered Go modules:"
			list_modules
			;;
		fmt|tidy|update|fix|vet|lint)
			run_on_modules "$cmd"
			;;
		*)
			echo "Error: unknown command '$cmd'" >&2
			echo "" >&2
			usage >&2
			return 1
			;;
	esac
}

main "$@"
