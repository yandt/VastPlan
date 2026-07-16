#!/usr/bin/env bash
# 安装本地 git 钩子（一次性）。
# 钩子与 CI 同规则：本地早拦一秒，胜过 PR 上红一次。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

git config core.hooksPath .githooks
chmod +x .githooks/*

echo "已启用本地钩子（core.hooksPath=.githooks）："
for hook in .githooks/*; do
  [ -f "$hook" ] || continue
  printf '  - %s\n' "${hook#.githooks/}"
done
