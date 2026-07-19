#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT/engineering/tools/platform-dev.sh"
TEMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/vastplan-platform-dev-test.XXXXXX")"
STATE_ROOT="$TEMP_ROOT/state"

cleanup() {
  rm -rf "$TEMP_ROOT"
}
trap cleanup EXIT

fail() {
  printf 'platform-dev_test: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local value="$1"
  local expected="$2"
  case "$value" in
    *"$expected"*) ;;
    *) fail "输出缺少 '$expected': $value" ;;
  esac
}

bash -n "$SCRIPT"

help_output="$(NO_COLOR=1 "$SCRIPT" help)"
assert_contains "$help_output" "up [--debug] [--fresh]"
assert_contains "$help_output" "doctor"
assert_contains "$help_output" "logs [--follow]"

invalid_output="$TEMP_ROOT/invalid-timeout.log"
if NO_COLOR=1 VASTPLAN_DEV_STATE_ROOT="$STATE_ROOT" "$SCRIPT" up --timeout invalid >"$invalid_output" 2>&1; then
  fail "非法启动超时应被拒绝"
fi
assert_contains "$(<"$invalid_output")" "启动超时 必须是正整数"

duplicate_output="$TEMP_ROOT/duplicate-port.log"
if NO_COLOR=1 \
  VASTPLAN_DEV_STATE_ROOT="$STATE_ROOT" \
  VASTPLAN_DEV_GATEWAY_PORT=19080 \
  VASTPLAN_DEV_PORTAL_PORT=19080 \
  VASTPLAN_DEV_ARTIFACT_PORT=19443 \
  VASTPLAN_DEV_VAULT_PORT=19200 \
  "$SCRIPT" doctor >"$duplicate_output" 2>&1; then
  fail "重复端口应被拒绝"
fi
assert_contains "$(<"$duplicate_output")" "开发服务端口必须互不相同"

mkdir -p "$STATE_ROOT"
printf '999999\n' > "$STATE_ROOT/platformdev.pid"
status_output="$TEMP_ROOT/status.log"
if NO_COLOR=1 VASTPLAN_DEV_STATE_ROOT="$STATE_ROOT" "$SCRIPT" status >"$status_output" 2>&1; then
  fail "无运行进程时 status 应返回非零"
fi
assert_contains "$(<"$status_output")" "平台管理中心未运行"
if [ -e "$STATE_ROOT/platformdev.pid" ]; then
  fail "status 应清理失效 PID 文件"
fi

UNMARKED_ROOT="$TEMP_ROOT/unmarked"
mkdir -p "$UNMARKED_ROOT"
printf 'do-not-delete\n' > "$UNMARKED_ROOT/marker"
unmarked_output="$TEMP_ROOT/unmarked-clean.log"
if NO_COLOR=1 VASTPLAN_DEV_STATE_ROOT="$UNMARKED_ROOT" "$SCRIPT" clean >"$unmarked_output" 2>&1; then
  fail "clean 应拒绝未标记的自定义运行目录"
fi
assert_contains "$(<"$unmarked_output")" "拒绝清理未标记的自定义运行目录"
if [ ! -e "$UNMARKED_ROOT/marker" ]; then
  fail "clean 不得删除未标记的自定义运行目录"
fi

printf 'temporary\n' > "$STATE_ROOT/marker"
printf 'VastPlan platform-dev state v1\n' > "$STATE_ROOT/.vastplan-platform-dev-state"
clean_output="$(NO_COLOR=1 VASTPLAN_DEV_STATE_ROOT="$STATE_ROOT" "$SCRIPT" clean)"
assert_contains "$clean_output" "已删除本地运行数据"
if [ -e "$STATE_ROOT" ]; then
  fail "clean 应删除隔离运行目录"
fi

printf 'platform-dev_test: PASS\n'
