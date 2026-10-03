#!/usr/bin/env bash
# Fetch the latest stable Go release from go.dev and bump every go.mod, go.work,
# and golang Docker base image in the repo to that version.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GO_VERSION_URL="${GO_VERSION_URL:-https://go.dev/VERSION?m=text}"
GO_DL_JSON_URL="${GO_DL_JSON_URL:-https://go.dev/dl/?mode=json}"
GO_SRC_URL_TMPL="${GO_SRC_URL_TMPL:-https://dl.google.com/go/go%s.src.tar.gz}"

usage() {
	cat <<'EOF'
Usage: script/upgrade-go.sh

Fetches the latest stable Go version from go.dev, validates it against the
official download list / source tarball, then updates:

  - every go.mod  (go X.Y.Z directive)
  - every go.work (go X.Y.Z directive)
  - every Dockerfile FROM golang:X.Y.Z[-variant] line

Run via: make upgrade
EOF
}

http_get() {
	local url="$1"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL \
			-A "supabase-manager-upgrade-go/1.0 (+https://go.dev)" \
			-H "Accept: text/plain, application/json, */*" \
			"$url"
		return
	fi
	if command -v wget >/dev/null 2>&1; then
		wget -qO- --user-agent="supabase-manager-upgrade-go/1.0" "$url"
		return
	fi
	echo "Error: need curl or wget to fetch Go version from go.dev" >&2
	return 1
}

http_head_ok() {
	local url="$1"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSIL -A "supabase-manager-upgrade-go/1.0" -o /dev/null "$url"
		return
	fi
	if command -v wget >/dev/null 2>&1; then
		wget --spider -q --user-agent="supabase-manager-upgrade-go/1.0" "$url"
		return
	fi
	return 1
}

normalize_go_version() {
	local raw="$1"
	raw="${raw#go}"
	raw="${raw%%[[:space:]]*}"
	if [[ ! "$raw" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
		echo "Error: invalid Go version '$1'" >&2
		return 1
	fi
	echo "$raw"
}

version_from_dl_json() {
	local json="$1"
	if command -v python3 >/dev/null 2>&1; then
		python3 -c '
import json, sys
data = json.load(sys.stdin)
stable = next((x for x in data if x.get("stable")), None)
item = stable or (data[0] if data else None)
if not item:
    sys.exit(1)
print(item["version"])
' <<<"$json"
		return
	fi
	# No python: first version field in the JSON array.
	printf '%s' "$json" | grep -oE '"version": "go[0-9]+\.[0-9]+(\.[0-9]+)?"' | head -n 1 | sed -E 's/.*"//; s/"$//'
}

fetch_latest_go_version() {
	local raw version json

	echo "Fetching latest Go version from $GO_VERSION_URL ..." >&2
	if raw="$(http_get "$GO_VERSION_URL" 2>/dev/null)"; then
		raw="$(printf '%s\n' "$raw" | head -n 1 | tr -d '\r')"
		if version="$(normalize_go_version "$raw" 2>/dev/null)"; then
			echo "$version"
			return 0
		fi
	fi

	echo "VERSION endpoint failed; falling back to $GO_DL_JSON_URL ..." >&2
	json="$(http_get "$GO_DL_JSON_URL")" || {
		echo "Error: could not reach go.dev to resolve the latest Go version" >&2
		return 1
	}

	raw="$(version_from_dl_json "$json")" || {
		echo "Error: could not parse latest Go version from go.dev/dl JSON" >&2
		return 1
	}
	normalize_go_version "$raw"
}

validate_go_version() {
	local version="$1"
	local url
	url="$(printf "$GO_SRC_URL_TMPL" "$version")"

	echo "Validating go${version} against official downloads ($url) ..."
	if http_head_ok "$url"; then
		echo "  ok — go${version} is published"
		return 0
	fi

	echo "  source HEAD failed; checking dl JSON listing ..."
	local json
	json="$(http_get "$GO_DL_JSON_URL")" || return 1
	if printf '%s' "$json" | grep -q "\"version\": \"go${version}\""; then
		echo "  ok — go${version} found in go.dev/dl listing"
		return 0
	fi

	echo "Error: go${version} was not found on the official Go download site" >&2
	return 1
}

update_go_directive_file() {
	local file="$1"
	local version="$2"
	local before after

	before="$(grep -E '^go [0-9]+\.[0-9]+(\.[0-9]+)?$' "$file" || true)"
	if [[ -z "$before" ]]; then
		echo "  skip $file (no go directive)"
		return 0
	fi

	after="go $version"
	if [[ "$before" == "$after" ]]; then
		echo "  ok   $file (already $after)"
		return 0
	fi

	awk -v ver="$version" '
		BEGIN { done=0 }
		/^go [0-9]+\.[0-9]+(\.[0-9]+)?$/ && !done {
			print "go " ver
			done=1
			next
		}
		{ print }
	' "$file" >"${file}.tmp"
	mv "${file}.tmp" "$file"
	echo "  bump $file: $before → $after"
}

update_dockerfile_go_image() {
	local file="$1"
	local version="$2"
	local before after

	before="$(grep -nE 'FROM[[:space:]]+golang:[0-9]+\.[0-9]+(\.[0-9]+)?(-[A-Za-z0-9._]+)?' "$file" || true)"
	if [[ -z "$before" ]]; then
		echo "  skip $file (no golang: base image)"
		return 0
	fi

	awk -v ver="$version" '
		{
			line = $0
			if (match(line, /FROM[[:space:]]+golang:[0-9]+\.[0-9]+(\.[0-9]+)?(-[A-Za-z0-9._]+)?/)) {
				old = substr(line, RSTART, RLENGTH)
				new = old
				sub(/golang:[0-9]+\.[0-9]+(\.[0-9]+)?/, "golang:" ver, new)
				sub(old, new, line)
			}
			print line
		}
	' "$file" >"${file}.tmp"

	if cmp -s "$file" "${file}.tmp"; then
		rm -f "${file}.tmp"
		echo "  ok   $file (already golang:${version}…)"
		return 0
	fi

	mv "${file}.tmp" "$file"
	after="$(grep -nE 'FROM[[:space:]]+golang:' "$file" || true)"
	echo "  bump $file:"
	while IFS= read -r row; do
		[[ -n "$row" ]] && echo "       was: $row"
	done <<<"$before"
	while IFS= read -r row; do
		[[ -n "$row" ]] && echo "       now: $row"
	done <<<"$after"
}

main() {
	case "${1:-}" in
		-h|--help|help)
			usage
			return 0
			;;
	esac

	local version
	version="$(fetch_latest_go_version)"
	echo "Latest stable Go: $version"
	validate_go_version "$version"

	echo ""
	echo "=== go.mod / go.work ==="
	local file rel
	while IFS= read -r -d '' file; do
		rel="${file#"$ROOT"/}"
		update_go_directive_file "$rel" "$version"
	done < <(find "$ROOT" \( -name go.mod -o -name go.work \) \
		-not -path '*/vendor/*' \
		-not -path '*/.git/*' \
		-print0 | sort -z)

	echo ""
	echo "=== Dockerfiles (golang: images) ==="
	local found_docker=0
	while IFS= read -r -d '' file; do
		found_docker=1
		rel="${file#"$ROOT"/}"
		update_dockerfile_go_image "$rel" "$version"
	done < <(find "$ROOT" \( -name Dockerfile -o -name 'Dockerfile.*' \) \
		-not -path '*/vendor/*' \
		-not -path '*/.git/*' \
		-print0 | sort -z)

	if [[ "$found_docker" -eq 0 ]]; then
		echo "  (no Dockerfiles found)"
	fi

	echo ""
	echo "=== go build cache ==="
	# After a patch bump (e.g. 1.27.0 → 1.27.1), stale GOCACHE objects cause:
	#   compile: version "go1.27.0" does not match go tool version "go1.27.1"
	if command -v go >/dev/null 2>&1; then
		go clean -cache
		echo "  cleaned GOCACHE ($(go env GOCACHE))"
	else
		echo "  (go not on PATH — skip cache clean; run: go clean -cache)"
	fi

	echo ""
	echo "upgrade completed → Go $version"
	echo "If /usr/local/go/VERSION still differs from go$version, reinstall the toolchain"
	echo "or keep running go clean -cache after GOTOOLCHAIN downloads a newer patch."
}

main "$@"
