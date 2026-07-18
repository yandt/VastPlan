#!/usr/bin/env bash
# 在同一台机器上采样 Backend 核心 benchmark；--compare <git-ref> 与基线提交比较。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CURRENT="$(mktemp)"
BASE="$(mktemp)"
WORKTREE=""
cleanup() {
  if [[ -n "$WORKTREE" ]]; then git -C "$ROOT" worktree remove --force "$WORKTREE" >/dev/null 2>&1 || true; fi
  rm -f "$CURRENT" "$BASE"
}
trap cleanup EXIT
run_bench() { (cd "$1" && go test -run='^$' -bench='^BenchmarkBackend_' -benchmem -count=5 ./...) | tee "$2"; }
if [[ "${1:-}" == "--compare" ]]; then
  [[ -n "${2:-}" ]] || { echo "缺少 base git ref" >&2; exit 2; }
  WORKTREE="$(mktemp -d)"; git -C "$ROOT" worktree add --detach "$WORKTREE" "$2" >/dev/null
  run_bench "$WORKTREE" "$BASE"
fi
run_bench "$ROOT" "$CURRENT"
if [[ -n "$WORKTREE" ]]; then go run "$ROOT/engineering/tools/benchcompare" -base "$BASE" -current "$CURRENT"; fi
