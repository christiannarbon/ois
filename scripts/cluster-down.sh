#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ENV_FILE="${REPO_ROOT}/components/cluster/cluster.env"

# Finding 6d: Preflight PATH checks
if ! command -v minikube >/dev/null 2>&1; then
  echo "ERROR: 'minikube' binary not found on PATH. Please install minikube." >&2
  exit 1
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "ERROR: 'kubectl' binary not found on PATH. Please install kubectl." >&2
  exit 1
fi

if [[ -f "${ENV_FILE}" ]]; then
  # shellcheck source=../components/cluster/cluster.env
  source "${ENV_FILE}"
fi

PROFILE="${PROFILE:-open-iceberg}"

echo "======================================================================="
echo "Teardown Minikube Cluster: ${PROFILE}"
echo "======================================================================="

# Finding 6b: Exact profile existence check
PROFILE_EXISTS=false
if minikube status -p "${PROFILE}" >/dev/null 2>&1; then
  PROFILE_EXISTS=true
elif minikube profile list -o json 2>/dev/null | grep -q "\"Name\":\s*\"${PROFILE}\""; then
  PROFILE_EXISTS=true
elif minikube profile list 2>/dev/null | grep -E -q "(^|\s)${PROFILE}(\s|$)"; then
  PROFILE_EXISTS=true
fi

if [[ "${PROFILE_EXISTS}" = true ]]; then
  echo "Deleting Minikube profile '${PROFILE}'..."
  minikube delete -p "${PROFILE}"
else
  echo "Minikube profile '${PROFILE}' is not present."
fi

# Clean up only this specific cluster's kubeconfig context and cluster definitions
echo "Cleaning up kubeconfig context for '${PROFILE}'..."
kubectl config delete-context "${PROFILE}" >/dev/null 2>&1 || true
kubectl config delete-cluster "${PROFILE}" >/dev/null 2>&1 || true
kubectl config unset "users.${PROFILE}" >/dev/null 2>&1 || true
kubectl config unset "contexts.${PROFILE}" >/dev/null 2>&1 || true

echo "======================================================================="
echo "Teardown of cluster '${PROFILE}' complete."
echo "======================================================================="
