#!/usr/bin/env bash
# 一键启动/停止完整的本地平台管理中心。生产部署不得使用此开发编排器。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STATE_ROOT="$ROOT/.vastplan/dev-platform"
BIN="$STATE_ROOT/platformdev"
PID_FILE="$STATE_ROOT/platformdev.pid"
LOG_FILE="$STATE_ROOT/platformdev.log"
STATUS_URL="http://127.0.0.1:18080/__vastplan_dev/status"
PORTAL_URL="http://127.0.0.1:18080/operations"

mkdir -p "$STATE_ROOT" "$STATE_ROOT/go-cache"

running_pid() {
  if [ ! -f "$PID_FILE" ]; then
    return 1
  fi
  local pid
  pid="$(tr -d '[:space:]' < "$PID_FILE")"
  if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
    return 1
  fi
  printf '%s' "$pid"
}

case "${1:-up}" in
  up)
    if pid="$(running_pid)"; then
      echo "平台管理中心已经运行 pid=$pid"
      echo "$PORTAL_URL"
      exit 0
    fi
    echo "正在构建本地开发编排器..."
    (cd "$ROOT" && env GOCACHE="$STATE_ROOT/go-cache" go build -trimpath -buildvcs=false -o "$BIN" ./engineering/tools/platformdev)
    echo "正在后台构建并启动完整平台，首次运行通常需要 1-3 分钟..."
    nohup "$BIN" -root "$ROOT" -state-root "$STATE_ROOT" >"$LOG_FILE" 2>&1 &
    pid=$!
    printf '%s\n' "$pid" > "$PID_FILE"
    for _ in $(seq 1 300); do
      if ! kill -0 "$pid" 2>/dev/null; then
        echo "平台管理中心启动失败，最近日志：" >&2
        tail -n 80 "$LOG_FILE" >&2 || true
        exit 1
      fi
      if curl --silent --fail "$STATUS_URL" >/dev/null 2>&1; then
        echo "平台管理中心已就绪：$PORTAL_URL"
        echo "日志：$LOG_FILE"
        exit 0
      fi
      sleep 1
    done
    echo "等待平台管理中心就绪超时，查看日志：$LOG_FILE" >&2
    exit 1
    ;;
  down)
    if ! pid="$(running_pid)"; then
      echo "平台管理中心未运行"
      exit 0
    fi
    kill -TERM "$pid"
    for _ in $(seq 1 30); do
      if ! kill -0 "$pid" 2>/dev/null; then
        echo "平台管理中心已停止"
        exit 0
      fi
      sleep 1
    done
    kill -KILL "$pid"
    echo "平台管理中心未及时退出，已强制停止"
    ;;
  status)
    if pid="$(running_pid)"; then
      echo "平台管理中心进程运行中 pid=$pid"
      curl --silent --show-error "$STATUS_URL" || true
      echo
    else
      echo "平台管理中心未运行"
      exit 1
    fi
    ;;
  logs)
    tail -n 100 -f "$LOG_FILE"
    ;;
  *)
    echo "用法: $0 {up|down|status|logs}" >&2
    exit 2
    ;;
esac
