#!/usr/bin/env bash
# docker-deploy.sh — build, publish, then restart a Kubernetes deployment.
#
# Usage:
#   ./script/docker-deploy.sh manager
#   NAMESPACE=supabase-manager ./script/docker-deploy.sh manager
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=script/docker-common.sh
source "$ROOT_DIR/script/docker-common.sh"

SERVICE="${1:-}"
NAMESPACE="${NAMESPACE:-supabase-manager}"

if [[ -z "$SERVICE" ]]; then
	echo "Usage: $0 <service>" >&2
	exit 1
fi

DEPLOYMENT="${DEPLOYMENT:-$(service_k8s_deployment "$SERVICE")}"

setup_kubectl

echo "=== 1/3 Build & publish ${SERVICE} ==="
"$ROOT_DIR/script/docker-publish.sh" "$SERVICE"

echo "=== 2/3 Restart deployment ${NAMESPACE}/${DEPLOYMENT} ==="
"$KUBECTL" rollout restart "deployment/${DEPLOYMENT}" -n "${NAMESPACE}"

echo "=== 3/3 Wait for rollout ==="
"$KUBECTL" rollout status "deployment/${DEPLOYMENT}" -n "${NAMESPACE}" --timeout=180s
"$KUBECTL" get pods -n "${NAMESPACE}" -l "app=${DEPLOYMENT}" -o wide

echo "Deployed ${DEPLOYMENT} in ${NAMESPACE}"
