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
K=(kubectl --context "$CTX" -n "$NS")

for pin in RUSTFS_CHART_REPO RUSTFS_CHART_VERSION RUSTFS_APP_VERSION BUSYBOX_VERSION AWSCLI_IMAGE; do
  [[ -n "${!pin:-}" ]] || { echo "ERROR: $pin not pinned in components/versions.env" >&2; exit 1; }
done

for bin in kubectl helm; do
  command -v "$bin" >/dev/null || { echo "ERROR: '$bin' not found on PATH" >&2; exit 1; }
done
kubectl config get-contexts "$CTX" >/dev/null 2>&1 \
  || { echo "ERROR: kube context '$CTX' not found; run scripts/cluster-up.sh first" >&2; exit 1; }
[[ -f "$ENV_FILE" ]] || { echo "ERROR: $ENV_FILE missing; copy .env.example and set credentials" >&2; exit 1; }

# Prints the value of KEY from .env. Accepts KEY=value, KEY="value" or KEY='value'; never echoes the value on error.
env_value() {
  local key="$1" lines line val
  lines="$(grep -E "^[[:space:]]*(export[[:space:]]+)?${key}[[:space:]]*=" "$ENV_FILE" || true)"
  [[ -n "$lines" ]] || return 0
  [[ "$(printf '%s\n' "$lines" | wc -l)" -eq 1 ]] || { echo "ERROR: $key is set more than once in .env" >&2; return 1; }
  line="${lines%$'\r'}"
  [[ "$line" == "${key}="* ]] \
    || { echo "ERROR: write $key as ${key}=value in .env (no 'export', no spaces around '=')" >&2; return 1; }
  val="${line#"${key}"=}"
  if [[ "$val" =~ ^\"(.*)\"$ || "$val" =~ ^\'(.*)\'$ ]]; then
    val="${BASH_REMATCH[1]}"
  elif [[ "$val" == [\"\']* || "$val" =~ [[:space:]] ]]; then
    echo "ERROR: $key in .env has an unclosed quote or unquoted spaces (an inline '# comment' counts); quote the whole value" >&2
    return 1
  fi
  printf '%s' "$val"
}

ACCESS_KEY="$(env_value RUSTFS_ROOT_USER)"
SECRET_KEY="$(env_value RUSTFS_ROOT_PASSWORD)"
if [[ -z "$ACCESS_KEY" || -z "$SECRET_KEY" ]]; then
  echo "ERROR: set RUSTFS_ROOT_USER and RUSTFS_ROOT_PASSWORD in .env" >&2
  exit 1
fi
if [[ "$SECRET_KEY" == change-me* || "$ACCESS_KEY" == rustfsadmin || "$SECRET_KEY" == rustfsadmin \
      || ${#ACCESS_KEY} -lt 3 || ${#SECRET_KEY} -lt 8 ]]; then
  echo "ERROR: RustFS credentials in .env are too weak (user >= 3 chars, password >= 8 chars, not a placeholder or rustfsadmin)" >&2
  exit 1
fi

kubectl --context "$CTX" apply -f "${REPO_ROOT}/components/namespaces.yaml" >/dev/null

secret_version() { "${K[@]}" get secret "$1" -o jsonpath='{.metadata.resourceVersion}' 2>/dev/null || true; }
ROOT_BEFORE="$(secret_version rustfs-root)"
RUNNING_BEFORE="$("${K[@]}" get deploy rustfs -o name 2>/dev/null || true)"

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
  --set image.rustfs.tag="$RUSTFS_APP_VERSION" \
  --set image.initImage.tag="$BUSYBOX_VERSION" \
  --history-max 5 \
  --wait --timeout 5m

kubectl --context "$CTX" apply -f "${REPO_ROOT}/components/storage/service.yaml" >/dev/null

# envFrom is read only at container start, so new credentials need a restart.
if [[ -n "$RUNNING_BEFORE" && -n "$ROOT_BEFORE" && "$ROOT_BEFORE" != "$(secret_version rustfs-root)" ]]; then
  echo "Credentials changed; restarting RustFS"
  "${K[@]}" rollout restart deploy/rustfs >/dev/null
  "${K[@]}" rollout status deploy/rustfs --timeout=180s
fi

"${K[@]}" wait --for=condition=Ready pod \
  -l app.kubernetes.io/name=rustfs,app.kubernetes.io/instance=rustfs --timeout=180s
echo "RustFS is ready in namespace $NS"

echo "Ensuring buckets..."
"${K[@]}" delete job ois-storage-buckets --ignore-not-found --wait=true >/dev/null
sed "s#__AWSCLI_IMAGE__#${AWSCLI_IMAGE}#" "${REPO_ROOT}/components/storage/buckets-job.yaml" \
  | kubectl --context "$CTX" apply -f - >/dev/null
JOB_STATE=""
for _ in $(seq 1 90); do
  JOB_STATE="$("${K[@]}" get job ois-storage-buckets \
    -o jsonpath='{.status.conditions[?(@.status=="True")].type}' 2>/dev/null || true)"
  [[ "$JOB_STATE" == *Complete* || "$JOB_STATE" == *Failed* ]] && break
  sleep 2
done
if [[ "$JOB_STATE" != *Complete* ]]; then
  echo "ERROR: bucket Job did not complete (state: ${JOB_STATE:-timeout}); logs follow" >&2
  "${K[@]}" logs job/ois-storage-buckets >&2 || true
  exit 1
fi
"${K[@]}" logs job/ois-storage-buckets | grep '^bucket '
