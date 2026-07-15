#!/usr/bin/env bash
# 统一测试入口（ADR-0018）。build tag 易被遗忘，故由本脚本封装两档运行。
#
#   ./tools/test.sh          单元测试（快，日常）
#   ./tools/test.sh --e2e    单元 + E2E（含跨进程真实链路，较慢）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "── 单元测试 ──"
go test ./...

if [[ "${1:-}" == "--e2e" ]]; then
  echo
  echo "── E2E（跨进程真实链路）──"
  go test -tags=e2e ./e2e/...
fi
