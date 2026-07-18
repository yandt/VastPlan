#!/usr/bin/env bash
# 契约与协议 codegen：contracts/proto/ 是单一真源（ADR-0016 §6），生成物入 core/shared/go。
# 依赖：protoc、protoc-gen-go、protoc-gen-go-grpc（go install 到 $(go env GOPATH)/bin）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GO_PATH="$(go env GOPATH)"
export PATH="$GO_PATH/bin:$PATH"

OUT="$ROOT/core/shared/go"
PY_OUT="$ROOT/extensions/sdk/python"
mkdir -p "$OUT"
mkdir -p "$PY_OUT"

protoc \
  -I "$ROOT/contracts/proto" \
  --go_out="$OUT" --go_opt=module=cdsoft.com.cn/VastPlan/core/shared/go \
  --go-grpc_out="$OUT" --go-grpc_opt=module=cdsoft.com.cn/VastPlan/core/shared/go \
	"$ROOT/contracts/proto/contract/v1/contract.proto" \
	"$ROOT/contracts/proto/addressing/v1/addressing.proto" \
	"$ROOT/contracts/proto/pluginhost/v1/pluginhost.proto"

# Python 使用 protoc 内建生成器；gRPC 客户端薄封装由 extensions/sdk/python/pluginhost/v1/
# 下的稳定文件提供，避免 codegen 依赖 grpcio-tools。
protoc \
  -I "$ROOT/contracts/proto" \
  --python_out="$PY_OUT" \
	"$ROOT/contracts/proto/contract/v1/contract.proto" \
	"$ROOT/contracts/proto/pluginhost/v1/pluginhost.proto"

echo "codegen 完成 → $OUT, $PY_OUT"
