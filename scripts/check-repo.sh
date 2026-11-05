#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "$REPO_ROOT"

SHELLCHECK_IMAGE="koalaman/shellcheck:v0.10.0"
YAMLLINT_IMAGE="cytopia/yamllint:1.26"
FAILED=0
pass() { echo "PASS $1"; }
fail() { echo "FAIL $1" >&2; FAILED=1; }

run_tool() {
  local bin="$1" image="$2"; shift 2
  if command -v "$bin" >/dev/null; then
    "$bin" "$@"
  elif command -v docker >/dev/null; then
    docker run --rm -v "$REPO_ROOT:/mnt:ro" -w /mnt "$image" "$@"
  else
    echo "ERROR: need '$bin' or docker" >&2; return 1
  fi
}

if run_tool shellcheck "$SHELLCHECK_IMAGE" scripts/*.sh; then pass "shellcheck"; else fail "shellcheck"; fi
if run_tool yamllint "$YAMLLINT_IMAGE" -c .yamllint.yaml components/; then pass "yamllint"; else fail "yamllint"; fi

bad="$(grep -nvE '^[[:space:]]*(#|$)' components/versions.env | grep -vE '^[0-9]+:[A-Z][A-Z0-9_]*="[^"$`]*"$' || true)"
if [[ -z "$bad" ]]; then pass "versions.env format"; else fail "versions.env format:"$'\n'"$bad"; fi

ids="$(grep -rnE --exclude=check-repo.sh 'OPEN-ICE|DR-[0-9]' docs/tech docs/user components scripts cmd internal || true)"
if [[ -z "$ids" ]]; then pass "no plan ids in docs, components, scripts, code"; else fail "plan ids found:"$'\n'"$ids"; fi

broken_links() {
  python3 -I - <<'PY'
import pathlib, re
bad = []
for md in pathlib.Path(".").rglob("*.md"):
    if any(part in {"my_notes", ".git", "node_modules"} for part in md.parts):
        continue
    for target in re.findall(r"\]\(([^)\s]+)\)", md.read_text(encoding="utf-8")):
        if re.match(r"^[a-z]+:", target) or target.startswith("#"):
            continue
        path = target.split("#", 1)[0]
        if path and not (md.parent / path).exists():
            bad.append(f"{md}: {target}")
print("\n".join(bad))
PY
}
links="$(broken_links)"
if [[ -z "$links" ]]; then pass "relative markdown links"; else fail "broken links:"$'\n'"$links"; fi

if [[ "${OFFLINE:-0}" == "1" ]]; then
  echo "SKIP helm render (OFFLINE=1)"
else
  # shellcheck source=../components/versions.env
  source components/versions.env
  rendered=true
  R="$(helm template rustfs rustfs --repo "$RUSTFS_CHART_REPO" --version "$RUSTFS_CHART_VERSION" --namespace storage \
    -f components/storage/values.yaml --set image.rustfs.tag="$RUSTFS_APP_VERSION" \
    --set image.initImage.tag="$BUSYBOX_VERSION" 2>&1)" || rendered=false
  if $rendered; then
    problems=""
    images="$(sed -nE 's/^ *image: "?([^"]+)"?$/\1/p' <<<"$R" | sort -u)"
    while read -r img; do
      [[ "$img" =~ :[^:/]+$ && "$img" != *:latest && "$img" != *:stable ]] || problems+="unpinned image $img; "
    done <<<"$images"
    grep -q "rustfs/rustfs:${RUSTFS_APP_VERSION}\"" <<<"$R" || problems+="rustfs image is not ${RUSTFS_APP_VERSION}; "
    grep -q "replicas: 1$" <<<"$R" || problems+="not single replica; "
    grep -q "storageClassName: \"\?standard" <<<"$R" || problems+="storageClass is not standard; "
    grep -q "rustfs-headless" <<<"$R" && problems+="headless service rendered; "
    if [[ -z "$problems" ]]; then pass "helm render (rustfs ${RUSTFS_CHART_VERSION})"; else fail "helm render: $problems"; fi
  else
    fail "helm render: $R"
  fi
fi

exit "$FAILED"
