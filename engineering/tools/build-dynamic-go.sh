#!/usr/bin/env bash
# 在当前原生平台共同构建 dynamic-go Backend、bootstrap-policy 进程入口与 .so 制品。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
OUT_DIR="${OUT_DIR:-bin/dynamic-go}"
mkdir -p "$OUT_DIR"

export CGO_ENABLED=1
case "$(go env GOOS)" in
  linux|darwin|freebsd) ;;
  *) echo "dynamic-go 不支持当前平台 $(go env GOOS)/$(go env GOARCH)" >&2; exit 1 ;;
esac

fingerprint="$(go run ./engineering/tools/dynamicgofingerprint -root .)"
version="$(tr -d '[:space:]' < core/kernels/backend/VERSION)"
plugin_id="com.vastplan.foundation.security.bootstrap-policy"

go build -trimpath -buildvcs=false \
  -ldflags "-s -w -buildid= -X main.version=${version} -X main.dynamicGoHostFingerprint=${fingerprint}" \
  -o "$OUT_DIR/backend-kernel" ./core/kernels/backend

go build -trimpath -buildvcs=false \
  -ldflags "-s -w -buildid=" \
  -o "$OUT_DIR/${plugin_id}" \
  ./extensions/plugins/com.vastplan.foundation.security.bootstrap-policy/backend

go build -trimpath -buildvcs=false -buildmode=plugin \
  -ldflags "-s -w -X main.dynamicGoBuildFingerprint=${fingerprint}" \
  -o "$OUT_DIR/${plugin_id}.so" \
  ./extensions/plugins/com.vastplan.foundation.security.bootstrap-policy/dynamic

go run ./engineering/tools/pluginpackage \
  -source extensions/plugins/com.vastplan.foundation.security.bootstrap-policy \
  -backend-bin "$OUT_DIR/${plugin_id}" \
  -dynamic-go-bin "$OUT_DIR/${plugin_id}.so" \
  -dynamic-go-fingerprint "$fingerprint" \
  -out "$OUT_DIR/${plugin_id}.tar.gz"

printf 'dynamic-go 构建完成: %s\n构建指纹: %s\n' "$OUT_DIR" "$fingerprint"
