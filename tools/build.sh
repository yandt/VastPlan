#!/usr/bin/env bash
# 构建内核与插件。
# 内核版本单一真源 = kernels/<name>/VERSION（ADR-0017 §1），经 ldflags 注入，
# 禁止在代码里硬编码版本。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
mkdir -p bin

# ── 内核 ──────────────────────────────────────────────
BACKEND_VERSION="$(tr -d '[:space:]' < kernels/backend/VERSION)"
go build -ldflags "-X main.version=${BACKEND_VERSION}" -o bin/backend-kernel ./kernels/backend
echo "已构建 bin/backend-kernel  (backend@${BACKEND_VERSION})"

# ── 插件 ──────────────────────────────────────────────
# 插件版本单一真源 = vastplan.plugin.json#version
for dir in plugins/*/; do
  id="$(basename "$dir")"
  [ -f "$dir/backend/main.go" ] || continue
  ver="$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$dir/vastplan.plugin.json" | head -1)"
  go build -o "bin/${id}" "./${dir}backend"
  echo "已构建 bin/${id}  (${id}@${ver})"
done
