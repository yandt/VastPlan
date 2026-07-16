#!/usr/bin/env bash
# 在两个独立输出目录构建并逐字节比较，证明同源码/工具链/目标平台可复现。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIRST="$(mktemp -d)"; SECOND="$(mktemp -d)"
trap 'rm -rf "$FIRST" "$SECOND"' EXIT
(cd "$ROOT" && OUT_DIR="$FIRST" ./tools/build.sh >/dev/null)
(cd "$ROOT" && OUT_DIR="$SECOND" ./tools/build.sh >/dev/null)

first_names="$(find "$FIRST" -maxdepth 1 -type f -exec basename {} \; | LC_ALL=C sort)"
second_names="$(find "$SECOND" -maxdepth 1 -type f -exec basename {} \; | LC_ALL=C sort)"
if [[ "$first_names" != "$second_names" ]]; then
  echo "两次构建的制品集合不一致" >&2
  diff -u <(printf '%s\n' "$first_names") <(printf '%s\n' "$second_names") || true
  exit 1
fi

status=0
while IFS= read -r -d '' binary; do
  name="$(basename "$binary")"
  if ! cmp -s "$FIRST/$name" "$SECOND/$name"; then echo "不可复现: $name" >&2; status=1; else shasum -a 256 "$FIRST/$name"; fi
done < <(find "$FIRST" -maxdepth 1 -type f -print0 | LC_ALL=C sort -z)
exit "$status"
