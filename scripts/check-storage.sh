#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=../components/cluster/cluster.env
source "${REPO_ROOT}/components/cluster/cluster.env"
CTX="${PROFILE}"; NS=storage
LOCAL_PORT="${LOCAL_PORT:-}"
BUCKETS=(warehouse flink)
K=(kubectl --context "$CTX" -n "$NS")
POD_SELECTOR="app.kubernetes.io/name=rustfs,app.kubernetes.io/instance=rustfs"

command -v kubectl >/dev/null || { echo "ERROR: 'kubectl' not found on PATH" >&2; exit 1; }
command -v aws >/dev/null || { echo "ERROR: 'aws' not found on PATH; install AWS CLI v2 (e.g. brew install awscli)" >&2; exit 1; }
aws --version 2>&1 | grep -q '^aws-cli/2\.' || { echo "ERROR: AWS CLI v2 required" >&2; exit 1; }

WORK="$(mktemp -d)"; PF_PID=""; PORT=""; KEY="check-storage/$(date +%s)-$$"
cleanup() {
  set +e
  if [[ -n "${AWS_ACCESS_KEY_ID:-}" && -n "$PORT" ]]; then
    for b in "${BUCKETS[@]}"; do
      for size in small large; do
        aws s3api delete-object --bucket "$b" --key "$KEY/$size" >/dev/null 2>&1 \
          || echo "WARN: could not delete s3://$b/$KEY/$size; the next run removes it" >&2
      done
    done
  fi
  [[ -n "$PF_PID" ]] && kill "$PF_PID" 2>/dev/null && wait "$PF_PID" 2>/dev/null
  rm -f "$WORK/config" "$WORK/pf.log" "$WORK/small" "$WORK/large" "$WORK/back"
  rmdir "$WORK" 2>/dev/null
}
trap cleanup EXIT

restarts() {
  "${K[@]}" get pod -l "$POD_SELECTOR" -o jsonpath='{range .items[*]}{.metadata.name}={.status.containerStatuses[0].restartCount}{" "}{end}'
}

secret() { "${K[@]}" get secret ois-storage-credentials -o "jsonpath={.data.$1}" | base64 --decode; }
AWS_ACCESS_KEY_ID="$(secret access-key)"; AWS_SECRET_ACCESS_KEY="$(secret secret-key)"
[[ -n "$AWS_ACCESS_KEY_ID" && -n "$AWS_SECRET_ACCESS_KEY" ]] \
  || { echo "ERROR: Secret ois-storage-credentials not found in $NS (run scripts/deploy-storage.sh)" >&2; exit 1; }
export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY
export AWS_DEFAULT_REGION=us-east-1 AWS_SHARED_CREDENTIALS_FILE=/dev/null
export AWS_CONFIG_FILE="$WORK/config"
printf '[default]\ns3 =\n    addressing_style = path\n' > "$AWS_CONFIG_FILE"

RESTARTS_BEFORE="$(restarts)"
[[ -n "$RESTARTS_BEFORE" ]] || { echo "ERROR: no RustFS pod in $NS (run scripts/deploy-storage.sh)" >&2; exit 1; }

# Requests go out only after kubectl itself reports the forward, so nothing else on the port can receive them.
"${K[@]}" port-forward --address 127.0.0.1 svc/rustfs "${LOCAL_PORT}:9000" >"$WORK/pf.log" 2>&1 &
PF_PID=$!
for _ in $(seq 1 30); do
  PORT="$(sed -nE 's/^Forwarding from 127\.0\.0\.1:([0-9]+) -> 9000$/\1/p' "$WORK/pf.log" | head -1)"
  [[ -n "$PORT" ]] && break
  kill -0 "$PF_PID" 2>/dev/null || { echo "ERROR: port-forward failed: $(cat "$WORK/pf.log")" >&2; exit 1; }
  sleep 1
done
[[ -n "$PORT" ]] || { echo "ERROR: port-forward to svc/rustfs did not start within 30s" >&2; exit 1; }
export AWS_ENDPOINT_URL="http://127.0.0.1:${PORT}"

for b in "${BUCKETS[@]}"; do
  aws s3 rm "s3://$b/check-storage/" --recursive --only-show-errors >/dev/null 2>&1 || true
done

head -c 4096 /dev/urandom > "$WORK/small"
head -c $((32 * 1024 * 1024)) /dev/urandom > "$WORK/large"
for b in "${BUCKETS[@]}"; do
  aws s3api head-bucket --bucket "$b" >/dev/null 2>&1 \
    || { echo "FAIL $b: bucket missing or unreachable (run scripts/deploy-storage.sh)" >&2; exit 1; }
  for size in small large; do
    aws s3 cp "$WORK/$size" "s3://$b/$KEY/$size" --only-show-errors >/dev/null \
      || { echo "FAIL $b: $size upload failed" >&2; exit 1; }
    aws s3 ls "s3://$b/$KEY/$size" >/dev/null || { echo "FAIL $b: $size object not listed" >&2; exit 1; }
    aws s3 cp "s3://$b/$KEY/$size" "$WORK/back" --only-show-errors >/dev/null \
      || { echo "FAIL $b: $size download failed" >&2; exit 1; }
    cmp -s "$WORK/$size" "$WORK/back" || { echo "FAIL $b: $size downloaded bytes differ" >&2; exit 1; }
    aws s3api delete-object --bucket "$b" --key "$KEY/$size" >/dev/null
  done
  echo "PASS $b"
done

RESTARTS_AFTER="$(restarts)"
[[ "$RESTARTS_BEFORE" == "$RESTARTS_AFTER" ]] \
  || { echo "FAIL rustfs restarted during the check ($RESTARTS_BEFORE -> $RESTARTS_AFTER); check its memory limit" >&2; exit 1; }
echo "Storage check passed."
