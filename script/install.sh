#!/usr/bin/env bash
# Supabase Manager installer.
#
#   curl -fsSL https://raw.githubusercontent.com/izetmolla/supabase-manager/main/script/install.sh | sudo bash
#   curl -fsSL https://raw.githubusercontent.com/izetmolla/supabase-manager/main/script/install.sh | sudo bash -s update
#   curl -fsSL https://raw.githubusercontent.com/izetmolla/supabase-manager/main/script/install.sh | sudo bash -s uninstall
#
# Settings (environment variables, saved to $CONFIG_DIR/install.env and reused by `update`):
#   ADDR            listen address              (default 127.0.0.1:8080; 0.0.0.0:8080 exposes it)
#   PROJECTS_ROOT   where projects are stored   (default /etc/supabase-manager/projects)
#   VERSION         image tag                   (default latest)
#   PUID / PGID     user the manager runs as    (default 1000 / 1000)
#   TZ              timezone                    (default UTC)
#   SECURE_COOKIES  true when served over HTTPS (default false)
#
# Everything lives inside main(), so a partially downloaded script never runs.

main() {
	set -euo pipefail

	CONFIG_DIR="/etc/supabase-manager"
	CONFIG_FILE="$CONFIG_DIR/install.env"
	CONTAINER_NAME="supabase-manager"
	DATA_VOLUME="supabase-manager-data"
	IMAGE_REPO="izetmolla/supabase-manager"
	DOCKER_SOCKET="/var/run/docker.sock"

	local command="${1:-install}"
	case "$command" in
	install) install_manager ;;
	update | upgrade) update_manager ;;
	uninstall | remove) uninstall_manager ;;
	-h | --help | help) usage ;;
	*)
		usage
		exit 1
		;;
	esac
}

usage() {
	cat <<EOF
Usage: install.sh [install|update|uninstall]

  install    Install Docker if needed, then pull and start Supabase Manager (default)
  update     Pull the latest image and re-create the container (data is kept)
  uninstall  Remove the container (PURGE=1 also deletes the data volume and $CONFIG_DIR)
EOF
}

# ---- output ------------------------------------------------------------------------------------

if [ -t 1 ]; then
	GREEN=$'\033[0;32m' YELLOW=$'\033[1;33m' RED=$'\033[0;31m' BLUE=$'\033[0;34m' BOLD=$'\033[1m' NC=$'\033[0m'
else
	GREEN="" YELLOW="" RED="" BLUE="" BOLD="" NC=""
fi

info() { printf '%s==>%s %s\n' "$BLUE" "$NC" "$*"; }
ok() { printf '%s✓%s %s\n' "$GREEN" "$NC" "$*"; }
warn() { printf '%s!%s %s\n' "$YELLOW" "$NC" "$*" >&2; }
die() {
	printf '%sError:%s %s\n' "$RED" "$NC" "$*" >&2
	exit 1
}

command_exists() { command -v "$1" >/dev/null 2>&1; }

# ---- checks ------------------------------------------------------------------------------------

check_root() {
	[ "$(id -u)" = "0" ] || die "run this script as root, e.g. curl -fsSL <url> | sudo bash"
}

check_os() {
	[ "$(uname -s)" = "Linux" ] || die "Supabase Manager can only be installed on Linux"
	case "$(uname -m)" in
	x86_64 | amd64 | aarch64 | arm64) ;;
	*) die "unsupported architecture $(uname -m) (amd64 and arm64 are supported)" ;;
	esac
}

check_not_container() {
	if [ -f /.dockerenv ] || grep -qa 'container=' /proc/1/environ 2>/dev/null; then
		die "this script must run on the host, not inside a container"
	fi
}

# ---- packages ----------------------------------------------------------------------------------

OS_ID="" OS_LIKE="" OS_NAME="" PKG_MANAGER="" PKG_INDEX_UPDATED=0

detect_os() {
	if [ -r /etc/os-release ]; then
		# shellcheck disable=SC1091
		. /etc/os-release
		OS_ID="${ID:-}"
		OS_LIKE="${ID_LIKE:-}"
		OS_NAME="${PRETTY_NAME:-${NAME:-$OS_ID}}"
	fi
	OS_ID="$(printf '%s' "$OS_ID" | tr '[:upper:]' '[:lower:]')"
	OS_LIKE="$(printf '%s' "$OS_LIKE" | tr '[:upper:]' '[:lower:]')"

	if command_exists apt-get; then
		PKG_MANAGER=apt
	elif command_exists dnf; then
		PKG_MANAGER=dnf
	elif command_exists yum; then
		PKG_MANAGER=yum
	elif command_exists zypper; then
		PKG_MANAGER=zypper
	elif command_exists apk; then
		PKG_MANAGER=apk
	elif command_exists pacman; then
		PKG_MANAGER=pacman
	fi
	ok "Detected ${OS_NAME:-unknown Linux} (package manager: ${PKG_MANAGER:-none})"
}

# os_is NAME...: true if ID or ID_LIKE from /etc/os-release matches one of the names.
os_is() {
	local name
	for name in "$@"; do
		case " $OS_ID $OS_LIKE " in
		*" $name "*) return 0 ;;
		esac
	done
	return 1
}

pkg_update_index() {
	[ "$PKG_INDEX_UPDATED" = "1" ] && return
	info "Updating the package index"
	case "$PKG_MANAGER" in
	apt) DEBIAN_FRONTEND=noninteractive apt-get update -qq ;;
	dnf) dnf makecache -q -y >/dev/null ;;
	yum) yum makecache -q -y >/dev/null ;;
	zypper) zypper --non-interactive --quiet refresh ;;
	apk) apk update -q ;;
	pacman) pacman -Sy --noconfirm >/dev/null ;;
	esac
	PKG_INDEX_UPDATED=1
}

# pkg_install PKG...: installs the packages, or upgrades them when already installed.
pkg_install() {
	[ "$#" -gt 0 ] || return 0
	pkg_update_index
	info "Installing/updating: $*"
	case "$PKG_MANAGER" in
	apt) DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends "$@" ;;
	dnf)
		dnf install -y -q "$@"
		dnf upgrade -y -q "$@" >/dev/null 2>&1 || true
		;;
	yum)
		yum install -y -q "$@"
		yum update -y -q "$@" >/dev/null 2>&1 || true
		;;
	zypper) zypper --non-interactive --quiet install --no-recommends "$@" ;;
	apk) apk add --no-cache --upgrade "$@" ;;
	# Arch does not support partial upgrades, so installing anything means a full -Syu.
	pacman) pacman -Syu --needed --noconfirm "$@" ;;
	*) die "no supported package manager found; install these by hand and re-run: $*" ;;
	esac
}

# Installs (or updates) everything this script and Docker's installer rely on.
install_prerequisites() {
	local pkgs=()
	case "$PKG_MANAGER" in
	apt) pkgs=(curl ca-certificates gnupg iproute2) ;;
	dnf | yum) pkgs=(curl ca-certificates iproute) ;;
	apk) pkgs=(bash curl ca-certificates iproute2) ;;
	zypper | pacman) pkgs=(curl ca-certificates iproute2) ;;
	"")
		local cmd missing=()
		for cmd in curl awk grep stat seq; do
			command_exists "$cmd" || missing+=("$cmd")
		done
		[ "${#missing[@]}" -eq 0 ] || die "no supported package manager found and missing: ${missing[*]}"
		warn "no supported package manager found; skipping prerequisite updates"
		return
		;;
	esac
	# Base tools are only added when missing: minimal RHEL-like images ship coreutils-single,
	# which conflicts with the coreutils package.
	command_exists stat && command_exists seq || pkgs+=(coreutils)
	command_exists grep || pkgs+=(grep)
	command_exists awk || pkgs+=(gawk)
	command_exists tar || pkgs+=(tar)
	# Amazon Linux 2023 ships curl-minimal, which conflicts with the full curl package.
	if [ "$PKG_MANAGER" = "dnf" ] && rpm -q curl-minimal >/dev/null 2>&1; then
		pkgs=("${pkgs[@]/#curl/curl-minimal}")
	fi
	pkg_install "${pkgs[@]}"
	command_exists update-ca-certificates && update-ca-certificates >/dev/null 2>&1 || true
	ok "Prerequisites are installed and up to date"
}

# port_in_use PORT: true if something other than our container listens on PORT.
port_in_use() {
	local port="$1"
	if container_running; then
		return 1
	fi
	if command_exists ss; then
		ss -Htln 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]${port}\$"
	elif command_exists netstat; then
		netstat -tln 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]${port}\$"
	elif command_exists lsof; then
		lsof -iTCP:"$port" -sTCP:LISTEN -n -P >/dev/null 2>&1
	else
		return 1
	fi
}

check_port() {
	local port="${ADDR##*:}"
	[[ "$port" =~ ^[0-9]+$ ]] || die "ADDR must look like host:port (got '$ADDR')"
	if port_in_use "$port"; then
		die "port $port is already in use; free it or set another one, e.g. ADDR=127.0.0.1:8090"
	fi
	ok "Port $port is free"
}

# ---- docker ------------------------------------------------------------------------------------

# Docker CE from Docker's CentOS repository, for RHEL rebuilds that get.docker.com rejects.
install_docker_rhel_repo() {
	info "Adding the Docker CE repository"
	curl -fsSL https://download.docker.com/linux/centos/docker-ce.repo -o /etc/yum.repos.d/docker-ce.repo
	PKG_INDEX_UPDATED=0
	# podman/buildah conflict with containerd.io on RHEL-like systems.
	"$PKG_MANAGER" remove -y -q podman-docker runc >/dev/null 2>&1 || true
	pkg_install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
}

install_docker_engine() {
	case "$OS_ID" in
	ubuntu | debian | raspbian | centos | rhel | fedora)
		info "Installing Docker (get.docker.com)"
		if curl -fsSL https://get.docker.com | sh; then
			return
		fi
		warn "get.docker.com failed, falling back to the distribution packages"
		;;
	amzn)
		pkg_install docker
		return
		;;
	esac

	case "$PKG_MANAGER" in
	apt) pkg_install docker.io ;;
	dnf | yum)
		if os_is rhel centos fedora; then
			install_docker_rhel_repo
		else
			pkg_install docker
		fi
		;;
	zypper) pkg_install docker ;;
	apk)
		# docker lives in the community repository, which is disabled on some installs.
		if [ -f /etc/apk/repositories ] && ! grep -Eq '^[^#].*/community/?$' /etc/apk/repositories; then
			sed -i -E 's|^#[[:space:]]*(.*/community/?)$|\1|' /etc/apk/repositories
			PKG_INDEX_UPDATED=0
		fi
		pkg_install docker docker-cli-compose
		;;
	pacman) pkg_install docker docker-compose ;;
	*)
		info "Unknown distribution, trying get.docker.com"
		curl -fsSL https://get.docker.com | sh
		;;
	esac
}

install_docker() {
	if command_exists docker; then
		ok "Docker is installed ($(docker --version 2>/dev/null | head -n1))"
	else
		install_docker_engine
		command_exists docker || die "Docker installation failed; install it by hand (https://docs.docker.com/engine/install/) and re-run"
		ok "Docker installed ($(docker --version 2>/dev/null | head -n1))"
	fi

	if ! docker info >/dev/null 2>&1; then
		info "Starting the Docker daemon"
		if command_exists systemctl; then
			systemctl enable --now docker >/dev/null 2>&1 || true
		elif command_exists service; then
			service docker start >/dev/null 2>&1 || true
		elif command_exists rc-service; then
			rc-update add docker default >/dev/null 2>&1 || true
			rc-service docker start >/dev/null 2>&1 || true
		fi
		local i
		for i in $(seq 1 30); do
			docker info >/dev/null 2>&1 && break
			sleep 1
		done
	fi
	docker info >/dev/null 2>&1 || die "the Docker daemon is not running; start it and run the script again"
	[ -S "$DOCKER_SOCKET" ] || die "Docker socket not found at $DOCKER_SOCKET"
	ok "Docker daemon is running"
}

container_exists() { docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; }

container_running() {
	command_exists docker &&
		[ "$(docker container inspect -f '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null)" = "true" ]
}

# ---- configuration -----------------------------------------------------------------------------

# Environment variables win over the saved file, which wins over the defaults.
load_config() {
	local env_addr="${ADDR:-}" env_root="${PROJECTS_ROOT:-}" env_version="${VERSION:-}"
	local env_puid="${PUID:-}" env_pgid="${PGID:-}" env_tz="${TZ:-}" env_secure="${SECURE_COOKIES:-}"

	if [ -f "$CONFIG_FILE" ]; then
		# shellcheck disable=SC1090
		. "$CONFIG_FILE"
	fi

	ADDR="${env_addr:-${ADDR:-127.0.0.1:8080}}"
	PROJECTS_ROOT="${env_root:-${PROJECTS_ROOT:-/etc/supabase-manager/projects}}"
	VERSION="${env_version:-${VERSION:-latest}}"
	PUID="${env_puid:-${PUID:-1000}}"
	PGID="${env_pgid:-${PGID:-1000}}"
	TZ="${env_tz:-${TZ:-UTC}}"
	SECURE_COOKIES="${env_secure:-${SECURE_COOKIES:-false}}"
	IMAGE="$IMAGE_REPO:$VERSION"
}

save_config() {
	mkdir -p "$CONFIG_DIR"
	cat >"$CONFIG_FILE" <<EOF
# Supabase Manager install settings, used by: install.sh update
ADDR=$ADDR
PROJECTS_ROOT=$PROJECTS_ROOT
VERSION=$VERSION
PUID=$PUID
PGID=$PGID
TZ=$TZ
SECURE_COOKIES=$SECURE_COOKIES
EOF
	chmod 0600 "$CONFIG_FILE"
	ok "Settings saved to $CONFIG_FILE"
}

# ---- run ---------------------------------------------------------------------------------------

pull_image() {
	info "Pulling $IMAGE"
	docker pull "$IMAGE"
}

# Creates PROJECTS_ROOT and the data volume and hands them to PUID:PGID. Only the top level of
# PROJECTS_ROOT is chowned: project folders may hold database files owned by container users.
prepare_storage() {
	info "Preparing $PROJECTS_ROOT and volume $DATA_VOLUME"
	mkdir -p "$PROJECTS_ROOT"
	docker volume create "$DATA_VOLUME" >/dev/null
	docker run --rm \
		-v "$DATA_VOLUME":/data \
		-v "$PROJECTS_ROOT":/projects \
		alpine:3 sh -c "mkdir -p /data/home /data/bin && chown -R $PUID:$PGID /data && chown $PUID:$PGID /projects && chmod 0755 /projects"
}

start_container() {
	local docker_gid
	docker_gid="$(stat -c %g "$DOCKER_SOCKET")"

	if container_exists; then
		info "Removing the existing container (data is kept)"
		docker rm -f "$CONTAINER_NAME" >/dev/null
	fi

	info "Starting $CONTAINER_NAME"
	docker run -d --name "$CONTAINER_NAME" --restart unless-stopped \
		--init --network host \
		--user "$PUID:$PGID" --group-add "$docker_gid" \
		-e ADDR="$ADDR" \
		-e PROJECTS_ROOT="$PROJECTS_ROOT" \
		-e SECURE_COOKIES="$SECURE_COOKIES" \
		-e TZ="$TZ" \
		-v "$DATA_VOLUME":/data \
		-v "$PROJECTS_ROOT":"$PROJECTS_ROOT" \
		-v "$DOCKER_SOCKET":/var/run/docker.sock \
		"$IMAGE" >/dev/null
}

wait_healthy() {
	info "Waiting for Supabase Manager to become healthy"
	local i status
	for i in $(seq 1 60); do
		status="$(docker container inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$CONTAINER_NAME" 2>/dev/null || true)"
		case "$status" in
		healthy)
			ok "Supabase Manager is healthy"
			return
			;;
		exited | dead | unhealthy)
			docker logs --tail 50 "$CONTAINER_NAME" >&2 || true
			die "the container is $status (logs above)"
			;;
		esac
		sleep 2
	done
	warn "still not healthy after 2 minutes; check: docker logs -f $CONTAINER_NAME"
}

public_ip() {
	local ip
	ip="$(curl -4fsS --max-time 5 https://ifconfig.io 2>/dev/null || true)"
	[ -n "$ip" ] || ip="$(curl -4fsS --max-time 5 https://icanhazip.com 2>/dev/null || true)"
	[ -n "$ip" ] || ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
	printf '%s' "${ip:-<server-ip>}"
}

print_done() {
	local host="${ADDR%:*}" port="${ADDR##*:}" url
	echo
	printf '%s%sSupabase Manager is running.%s\n\n' "$GREEN" "$BOLD" "$NC"
	if [ "$host" = "127.0.0.1" ] || [ "$host" = "localhost" ]; then
		url="http://127.0.0.1:$port"
		echo "  Local URL:  $url"
		echo "  It listens on localhost only. From your computer, open an SSH tunnel:"
		echo "      ssh -L $port:127.0.0.1:$port root@$(public_ip)"
		echo "  then browse to $url"
		echo "  (or re-run with ADDR=0.0.0.0:$port to expose it; put HTTPS in front if you do)"
	else
		if [ "$host" = "0.0.0.0" ] || [ -z "$host" ]; then host="$(public_ip)"; fi
		url="http://$host:$port"
		echo "  URL:  $url"
	fi
	echo
	echo "  Open it and create the first admin account."
	echo "  Projects:  $PROJECTS_ROOT"
	echo "  Data:      docker volume $DATA_VOLUME"
	echo "  Logs:      docker logs -f $CONTAINER_NAME"
	echo "  Update:    curl -fsSL https://raw.githubusercontent.com/izetmolla/supabase-manager/main/script/install.sh | sudo bash -s update"
	echo
}

# ---- commands ----------------------------------------------------------------------------------

install_manager() {
	check_root
	check_os
	check_not_container
	detect_os
	install_prerequisites
	install_docker
	load_config
	check_port
	save_config
	pull_image
	prepare_storage
	start_container
	wait_healthy
	print_done
}

update_manager() {
	check_root
	command_exists docker || die "Docker is not installed; run the installer first"
	detect_os
	install_prerequisites
	install_docker
	load_config
	if [ ! -f "$CONFIG_FILE" ]; then
		warn "$CONFIG_FILE not found, using defaults and current environment"
		save_config
	fi
	pull_image
	prepare_storage
	start_container
	wait_healthy
	docker image prune -f --filter "label=org.opencontainers.image.title=supabase-manager" >/dev/null 2>&1 || true
	print_done
}

uninstall_manager() {
	check_root
	command_exists docker || die "Docker is not installed"
	load_config
	if container_exists; then
		docker rm -f "$CONTAINER_NAME" >/dev/null
		ok "Container $CONTAINER_NAME removed"
	else
		warn "container $CONTAINER_NAME not found"
	fi
	if [ "${PURGE:-0}" = "1" ]; then
		docker volume rm "$DATA_VOLUME" >/dev/null 2>&1 && ok "Volume $DATA_VOLUME removed" || true
		rm -rf "$CONFIG_DIR"
		ok "Settings removed ($CONFIG_DIR)"
		case "$PROJECTS_ROOT" in
		"$CONFIG_DIR"/*) ;;
		*) warn "custom projects folder $PROJECTS_ROOT was kept; delete it by hand if you want" ;;
		esac
		warn "running Supabase project containers were not touched; remove them with docker if needed"
	else
		echo "Data volume $DATA_VOLUME and $PROJECTS_ROOT were kept (PURGE=1 removes them)."
	fi
}

main "$@"
