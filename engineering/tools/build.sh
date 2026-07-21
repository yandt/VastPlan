#!/usr/bin/env bash
# 构建内核与插件。
# 内核版本单一真源 = core/kernels/<name>/VERSION（ADR-0017 §1），经 ldflags 注入，
# 禁止在代码里硬编码版本。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
OUT_DIR="${OUT_DIR:-bin}"
mkdir -p "$OUT_DIR"
export CGO_ENABLED="${CGO_ENABLED:-0}"
BUILD_FLAGS=(-trimpath -buildvcs=false)
COMMON_LDFLAGS="-s -w -buildid="

# ── 内核 ──────────────────────────────────────────────
BACKEND_VERSION="$(tr -d '[:space:]' < core/kernels/backend/VERSION)"
go build "${BUILD_FLAGS[@]}" -ldflags "${COMMON_LDFLAGS} -X main.version=${BACKEND_VERSION}" -o "$OUT_DIR/backend-kernel" ./core/kernels/backend
echo "已构建 $OUT_DIR/backend-kernel  (backend@${BACKEND_VERSION})"

# ── 插件 ──────────────────────────────────────────────
# 插件版本单一真源 = vastplan.plugin.json#version
for dir in extensions/plugins/*/; do
  id="$(basename "$dir")"
  [ -f "$dir/backend/main.go" ] || continue
  ver="$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$dir/vastplan.plugin.json" | head -1)"
  go build "${BUILD_FLAGS[@]}" -ldflags "$COMMON_LDFLAGS -X main.pluginVersion=${ver}" -o "$OUT_DIR/${id}" "./${dir}backend"
  echo "已构建 $OUT_DIR/${id}  (${id}@${ver})"
done
