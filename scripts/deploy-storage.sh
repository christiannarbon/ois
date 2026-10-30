#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=../components/cluster/cluster.env
source "${REPO_ROOT}/components/cluster/cluster.env"
# shellcheck source=../components/versions.env
source "${REPO_ROOT}/components/versions.env"
CTX="${PROFILE}"
NS=storage
ENV_FILE="${REPO_ROOT}/.env"

for bin in kubectl helm; do
  command -v "$bin" >/dev/null || { echo "ERROR: '$bin' not found on PATH" >&2; exit 1; }
done
kubectl config get-contexts "$CTX" >/dev/null 2>&1 \
  || { echo "ERROR: kube context '$CTX' not found; run scripts/cluster-up.sh first" >&2; exit 1; }
[[ -f "$ENV_FILE" ]] || { echo "ERROR: $ENV_FILE missing; copy .env.example and set credentials" >&2; exit 1; }

ACCESS_KEY="$(grep -E '^RUSTFS_ROOT_USER=' "$ENV_FILE" | head -1 | cut -d= -f2- | tr -d '"')"
SECRET_KEY="$(grep -E '^RUSTFS_ROOT_PASSWORD=' "$ENV_FILE" | head -1 | cut -d= -f2- | tr -d '"')"
if [[ -z "$ACCESS_KEY" || -z "$SECRET_KEY" || "$SECRET_KEY" == change-me* \
      || "$ACCESS_KEY" == rustfsadmin || "$SECRET_KEY" == rustfsadmin \
      || ${#ACCESS_KEY} -lt 3 || ${#SECRET_KEY} -lt 8 ]]; then
  echo "ERROR: set RUSTFS_ROOT_USER and RUSTFS_ROOT_PASSWORD in .env (user >= 3 chars, password >= 8 chars, not a placeholder)" >&2
  exit 1
fi

kubectl --context "$CTX" apply -f "${REPO_ROOT}/components/namespaces.yaml" >/dev/null

TMP="$(mktemp)"; chmod 600 "$TMP"; trap 'rm -f "$TMP"' EXIT
apply_secret() {
  cat > "$TMP"
  kubectl --context "$CTX" -n "$NS" create secret generic "$1" --from-env-file="$TMP" \
    --dry-run=client -o yaml | kubectl --context "$CTX" apply -f - >/dev/null
  : > "$TMP"
}
printf 'RUSTFS_ACCESS_KEY=%s\nRUSTFS_SECRET_KEY=%s\n' "$ACCESS_KEY" "$SECRET_KEY" | apply_secret rustfs-root
printf 'access-key=%s\nsecret-key=%s\n' "$ACCESS_KEY" "$SECRET_KEY" | apply_secret ois-storage-credentials
echo "Secrets rustfs-root and ois-storage-credentials applied in namespace $NS"

helm upgrade --install rustfs rustfs \
  --repo "$RUSTFS_CHART_REPO" --version "$RUSTFS_CHART_VERSION" \
  --kube-context "$CTX" --namespace "$NS" \
  -f "${REPO_ROOT}/components/storage/values.yaml" \
  --wait --timeout 5m

kubectl --context "$CTX" apply -f "${REPO_ROOT}/components/storage/service.yaml" >/dev/null

kubectl --context "$CTX" -n "$NS" wait --for=condition=Ready pod \
  -l app.kubernetes.io/name=rustfs,app.kubernetes.io/instance=rustfs --timeout=180s
echo "RustFS is ready in namespace $NS"
