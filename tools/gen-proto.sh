#!/usr/bin/env bash
# 契约与协议 codegen：proto/ 是单一真源（ADR-0016 §6），生成物入 shared/go。
# 依赖：protoc、protoc-gen-go、protoc-gen-go-grpc（go install 到 $(go env GOPATH)/bin）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_PATH="$(go env GOPATH)"
export PATH="$GO_PATH/bin:$PATH"

OUT="$ROOT/shared/go"
mkdir -p "$OUT"

protoc \
  -I "$ROOT/proto" \
  --go_out="$OUT" --go_opt=module=cdsoft.com.cn/VastPlan/shared/go \
  --go-grpc_out="$OUT" --go-grpc_opt=module=cdsoft.com.cn/VastPlan/shared/go \
	"$ROOT/proto/contract/v1/contract.proto" \
	"$ROOT/proto/addressing/v1/addressing.proto" \
	"$ROOT/proto/pluginhost/v1/pluginhost.proto"

echo "codegen 完成 → $OUT"
