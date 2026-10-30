#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
NAMESPACES_MANIFEST="${REPO_ROOT}/components/namespaces.yaml"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "ERROR: 'kubectl' binary not found on PATH." >&2
  exit 1
fi

if [[ ! -f "${NAMESPACES_MANIFEST}" ]]; then
  echo "ERROR: Namespaces manifest not found at ${NAMESPACES_MANIFEST}" >&2
  exit 1
fi

echo "======================================================================="
echo "Provisioning open-iceberg-stack per-component namespaces"
echo "======================================================================="

kubectl apply -f "${NAMESPACES_MANIFEST}"

echo "======================================================================="
echo "Namespaces provisioned successfully:"
kubectl get ns -l app.kubernetes.io/part-of=open-iceberg-stack
echo "======================================================================="
