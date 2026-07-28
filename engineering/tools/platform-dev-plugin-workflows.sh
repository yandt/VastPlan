#!/usr/bin/env bash
# Explicit development artifact and plugin workflows for platform-dev.sh.
# This file is sourced and is not a standalone command.

publish_test_artifact() {
  local package_file="$1"
  shift
  case "$package_file" in
    /*) ;;
    *) package_file="$PWD/$package_file" ;;
  esac
  ensure_state_dirs
  (cd "$ROOT" && env GOCACHE="$GO_CACHE" go run ./engineering/tools/testpublish \
    -package "$package_file" -state-root "$STATE_ROOT" -status-url "$STATUS_URL" "$@")
}

develop_backend_plugin() {
  local selector="$1"
  shift
  local pid
  if ! pid="$(running_pid)"; then
    fail "平台管理中心尚未运行；请先执行 '$0 up' 或 '$0 bootstrap'"
    return 1
  fi
  if ! curl --silent --fail "$STATUS_URL" >/dev/null 2>&1; then
    fail "平台管理中心尚未就绪"
    return 1
  fi
  ensure_state_dirs
  info "启动 Backend Plugin Dev Controller；Ctrl+C 只停止监听，不停止平台"
  (cd "$ROOT" && env GOCACHE="$GO_CACHE" go run ./engineering/tools/plugindev \
    -root "$ROOT" -state-root "$STATE_ROOT" -status-url "$STATUS_URL" -plugin "$selector" "$@")
}

release_plugins() {
  local operation="$1"
  local spec_file="$2"
  shift 2
  case "$operation" in
    plan|prepare)
      (cd "$ROOT" && env GOCACHE="$GO_CACHE" go run ./engineering/tools/pluginrelease "$operation" -root "$ROOT" -file "$spec_file" "$@")
      ;;
    execute)
      ensure_state_dirs
      (cd "$ROOT" && env GOCACHE="$GO_CACHE" go run ./engineering/tools/pluginrelease execute \
        -root "$ROOT" -file "$spec_file" -state-root "$STATE_ROOT" -status-url "$STATUS_URL" "$@")
      ;;
    *)
      fail "plugin-release 操作只允许 plan、prepare 或 execute"
      return 2
      ;;
  esac
}

sync_contracts() {
  (cd "$ROOT" && env GOCACHE="$GO_CACHE" go run ./engineering/tools/pluginrelease contracts -root "$ROOT" "$@")
}

clean_state() {
  local owned
  owned="$(owned_runtime_pids | unique_pids || true)"
  if [ -n "$owned" ]; then
    fail "仍有 VastPlan 本地受管进程运行，请先执行 '$0 down'"
    return 1
  fi
  case "$STATE_ROOT" in
    ""|"/"|"$ROOT")
      fail "拒绝清理不安全的运行目录: $STATE_ROOT"
      return 1
      ;;
  esac
  if [ ! -e "$STATE_ROOT" ]; then
    info "本地运行数据不存在，无需清理"
    return 0
  fi
  if [ "$STATE_ROOT" != "$DEFAULT_STATE_ROOT" ] && [ ! -f "$STATE_MARKER" ]; then
    fail "拒绝清理未标记的自定义运行目录: $STATE_ROOT"
    return 1
  fi
  rm -rf "$STATE_ROOT"
  success "已删除本地运行数据: $STATE_ROOT"
}
