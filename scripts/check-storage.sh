#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=../components/cluster/cluster.env
source "${REPO_ROOT}/components/cluster/cluster.env"
CTX="${PROFILE}"; NS=storage
LOCAL_PORT="${LOCAL_PORT:-19000}"
BUCKETS=(warehouse flink)
K=(kubectl --context "$CTX" -n "$NS")

command -v kubectl >/dev/null || { echo "ERROR: 'kubectl' not found on PATH" >&2; exit 1; }
command -v curl >/dev/null || { echo "ERROR: 'curl' not found on PATH" >&2; exit 1; }
command -v aws >/dev/null || { echo "ERROR: 'aws' not found on PATH; install AWS CLI v2 (e.g. brew install awscli)" >&2; exit 1; }
aws --version 2>&1 | grep -q '^aws-cli/2\.' || { echo "ERROR: AWS CLI v2 required" >&2; exit 1; }

WORK="$(mktemp -d)"; PF_PID=""; KEY="check-storage/$(date +%s)-$$"
cleanup() {
  set +e
  if [[ -n "${AWS_ACCESS_KEY_ID:-}" && -n "$PF_PID" ]]; then
    for b in "${BUCKETS[@]}"; do aws s3api delete-object --bucket "$b" --key "$KEY" >/dev/null 2>&1; done
  fi
  [[ -n "$PF_PID" ]] && kill "$PF_PID" 2>/dev/null && wait "$PF_PID" 2>/dev/null
  rm -f "$WORK/config" "$WORK/pf.log" "$WORK/payload" "$WORK/back"
  rmdir "$WORK" 2>/dev/null
}
trap cleanup EXIT

secret() { "${K[@]}" get secret ois-storage-credentials -o "jsonpath={.data.$1}" | base64 --decode; }
AWS_ACCESS_KEY_ID="$(secret access-key)"; AWS_SECRET_ACCESS_KEY="$(secret secret-key)"
[[ -n "$AWS_ACCESS_KEY_ID" && -n "$AWS_SECRET_ACCESS_KEY" ]] \
  || { echo "ERROR: Secret ois-storage-credentials not found in $NS (run scripts/deploy-storage.sh)" >&2; exit 1; }
export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY
export AWS_DEFAULT_REGION=us-east-1 AWS_SHARED_CREDENTIALS_FILE=/dev/null
export AWS_CONFIG_FILE="$WORK/config"
printf '[default]\ns3 =\n    addressing_style = path\n' > "$AWS_CONFIG_FILE"
export AWS_ENDPOINT_URL="http://127.0.0.1:${LOCAL_PORT}"

"${K[@]}" port-forward svc/rustfs "${LOCAL_PORT}:9000" >"$WORK/pf.log" 2>&1 &
PF_PID=$!
ready=false
for _ in $(seq 1 30); do
  curl -s -o /dev/null "http://127.0.0.1:${LOCAL_PORT}/" && { ready=true; break; }
  kill -0 "$PF_PID" 2>/dev/null || { echo "ERROR: port-forward failed: $(cat "$WORK/pf.log")" >&2; exit 1; }
  sleep 1
done
$ready || { echo "ERROR: RustFS not reachable on 127.0.0.1:${LOCAL_PORT}" >&2; exit 1; }

head -c 4096 /dev/urandom > "$WORK/payload"
for b in "${BUCKETS[@]}"; do
  aws s3api head-bucket --bucket "$b" >/dev/null 2>&1 \
    || { echo "FAIL $b: bucket missing or unreachable (run scripts/deploy-storage.sh)" >&2; exit 1; }
  aws s3 cp "$WORK/payload" "s3://$b/$KEY" >/dev/null
  aws s3 ls "s3://$b/$KEY" >/dev/null || { echo "FAIL $b: object not listed" >&2; exit 1; }
  aws s3 cp "s3://$b/$KEY" "$WORK/back" >/dev/null
  cmp -s "$WORK/payload" "$WORK/back" || { echo "FAIL $b: downloaded bytes differ" >&2; exit 1; }
  aws s3api delete-object --bucket "$b" --key "$KEY" >/dev/null
  echo "PASS $b"
done
echo "Storage check passed."
