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

if [[ ! -f "${ENV_FILE}" ]]; then
  echo "Error: Cluster environment file not found at ${ENV_FILE}" >&2
  exit 1
fi

# Source cluster configuration
# shellcheck source=../components/cluster/cluster.env
source "${ENV_FILE}"

PROFILE="${PROFILE:-open-iceberg}"
ADDONS="${ADDONS:-default-storageclass storage-provisioner}"
FULL_STACK_VAL="${FULL_STACK:-0}"
RECREATE_VAL="${RECREATE:-0}"

# Finding 2c: Non-fatal version skew preflight check
KUBECTL_CLIENT_VER=$(kubectl version --client -o json 2>/dev/null | jq -r '.clientVersion.minor // empty' 2>/dev/null || true)
if [[ -z "${KUBECTL_CLIENT_VER}" ]]; then
  KUBECTL_CLIENT_VER=$(kubectl version --client 2>/dev/null | grep -i "Client Version" | sed -E 's/.*v[0-9]+\.([0-9]+).*/\1/' || true)
fi
SERVER_MINOR_VER=$(echo "${KUBERNETES_VERSION:-v1.30.0}" | sed -E 's/v?[0-9]+\.([0-9]+).*/\1/')

if [[ -n "${KUBECTL_CLIENT_VER}" && -n "${SERVER_MINOR_VER}" ]]; then
  CLIENT_MINOR_NUM=$(echo "${KUBECTL_CLIENT_VER}" | sed 's/[^0-9]//g')
  SERVER_MINOR_NUM=$(echo "${SERVER_MINOR_VER}" | sed 's/[^0-9]//g')
  if [[ -n "${CLIENT_MINOR_NUM}" && -n "${SERVER_MINOR_NUM}" ]]; then
    SKEW=$(( CLIENT_MINOR_NUM > SERVER_MINOR_NUM ? CLIENT_MINOR_NUM - SERVER_MINOR_NUM : SERVER_MINOR_NUM - CLIENT_MINOR_NUM ))
    if (( SKEW > 1 )); then
      echo "WARNING: kubectl client minor version (${CLIENT_MINOR_NUM}) and target Kubernetes server minor version (${SERVER_MINOR_NUM}) skew exceeds +/-1 minor version (skew=${SKEW})."
    fi
  fi
fi

# Finding 1: Check existing profile sizing vs requested sizing
IS_RUNNING=false
LIVE_CPUS=""
LIVE_MEMORY=""

if minikube status -p "${PROFILE}" >/dev/null 2>&1; then
  IS_RUNNING=true
fi

if [[ "${IS_RUNNING}" = true ]]; then
  if command -v jq >/dev/null 2>&1; then
    PROFILE_JSON=$(minikube profile list -o json 2>/dev/null || true)
    if [[ -n "${PROFILE_JSON}" ]]; then
      LIVE_CPUS=$(echo "${PROFILE_JSON}" | jq -r --arg p "${PROFILE}" '.valid[]? | select(.Name==$p) | .Config.CPUs // empty' 2>/dev/null || true)
      LIVE_MEMORY=$(echo "${PROFILE_JSON}" | jq -r --arg p "${PROFILE}" '.valid[]? | select(.Name==$p) | .Config.Memory // empty' 2>/dev/null || true)
    fi
  fi

  if [[ -z "${LIVE_CPUS}" || -z "${LIVE_MEMORY}" ]]; then
    if command -v python3 >/dev/null 2>&1; then
      LIVE_CPUS=$(python3 -c "import sys, json, subprocess; data=json.loads(subprocess.check_output(['minikube','profile','list','-o','json'])); p=[x['Config'] for x in data.get('valid',[]) if x.get('Name')=='${PROFILE}']; print(p[0]['CPUs'] if p else '')" 2>/dev/null || true)
      LIVE_MEMORY=$(python3 -c "import sys, json, subprocess; data=json.loads(subprocess.check_output(['minikube','profile','list','-o','json'])); p=[x['Config'] for x in data.get('valid',[]) if x.get('Name')=='${PROFILE}']; print(p[0]['Memory'] if p else '')" 2>/dev/null || true)
    fi
  fi

  if [[ -n "${LIVE_CPUS}" && -n "${LIVE_MEMORY}" ]]; then
    if [[ "${LIVE_CPUS}" != "${CPUS}" || "${LIVE_MEMORY}" != "${MEMORY}" ]]; then
      if [[ "${RECREATE_VAL}" = "1" || "${RECREATE_VAL}" = "true" ]]; then
        echo "RECREATE=1 specified. Destroying existing cluster '${PROFILE}' (${LIVE_CPUS} CPUs / ${LIVE_MEMORY}MB)..."
        "${SCRIPT_DIR}/cluster-down.sh"
        IS_RUNNING=false
      else
        echo "=======================================================================" >&2
        echo "ERROR: cluster '${PROFILE}' is running with ${LIVE_CPUS} CPUs / ${LIVE_MEMORY}MB but FULL_STACK=${FULL_STACK_VAL} requests ${CPUS} CPUs / ${MEMORY}MB." >&2
        echo "Minikube cannot resize an existing cluster. To switch profiles run:" >&2
        echo "  ./scripts/cluster-down.sh && FULL_STACK=${FULL_STACK_VAL} ./scripts/cluster-up.sh" >&2
        echo "(this DESTROYS the current cluster and all its data)" >&2
        echo "Or run with RECREATE=1 FULL_STACK=${FULL_STACK_VAL} ./scripts/cluster-up.sh to recreate automatically." >&2
        echo "=======================================================================" >&2
        exit 1
      fi
    fi
  fi
fi

echo "======================================================================="
echo "Provisioning Minikube Cluster: ${PROFILE}"
if [[ "${IS_RUNNING}" = true && -n "${LIVE_CPUS}" ]]; then
  echo "Live Cluster Sizing: ${LIVE_CPUS} CPUs / ${LIVE_MEMORY}MB RAM"
else
  echo "Requested Sizing Profile: FULL_STACK=${FULL_STACK_VAL} (CPUs=${CPUS}, Memory=${MEMORY}MB, Disk=${DISK_SIZE})"
fi
echo "======================================================================="

if [[ "${IS_RUNNING}" = true ]]; then
  echo "Cluster '${PROFILE}' is already running with matching sizing."
else
  echo "Starting cluster '${PROFILE}' with parameters: ${MINIKUBE_START_ARGS}"
  # Note: MINIKUBE_START_ARGS is intentionally unquoted to expand arguments
  # shellcheck disable=SC2086
  minikube start ${MINIKUBE_START_ARGS}
fi

# Enable required StorageClass addons
for addon in ${ADDONS}; do
  echo "Enabling addon '${addon}' on profile '${PROFILE}'..."
  minikube addons enable "${addon}" -p "${PROFILE}"
done

# Ensure active kubectl context is open-iceberg
kubectl config use-context "${PROFILE}" >/dev/null 2>&1 || true

# Finding 3: Probe node readiness and default StorageClass assertion
echo "Waiting for cluster nodes to become Ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=300s

echo "Verifying default StorageClass..."
DEFAULT_SCS=$(kubectl get storageclass -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{"\n"}{end}' 2>/dev/null | grep -v '^$' || true)
DEFAULT_SC_COUNT=$(echo "${DEFAULT_SCS}" | grep -c . || echo 0)

if [[ "${DEFAULT_SC_COUNT}" -ne 1 ]]; then
  echo "ERROR: Expected exactly 1 default StorageClass, found ${DEFAULT_SC_COUNT}:" >&2
  if [[ -n "${DEFAULT_SCS}" ]]; then
    echo "${DEFAULT_SCS}" >&2
  else
    echo "(none)" >&2
  fi
  exit 1
fi

# Finding 6a: Correct quoting for context fallback
CURRENT_CTX=$(kubectl config current-context 2>/dev/null || echo "${PROFILE}")

echo "======================================================================="
echo "Cluster '${PROFILE}' is ready."
echo "Active context: ${CURRENT_CTX}"
echo "======================================================================="
