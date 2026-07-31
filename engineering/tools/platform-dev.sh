#!/usr/bin/env bash
# 安全启动/停止完整的本地平台管理中心。生产部署不得使用此开发编排器。
set -euo pipefail

umask 077

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEFAULT_STATE_ROOT="$ROOT/.vastplan/dev-platform"
STATE_ROOT="${VASTPLAN_DEV_STATE_ROOT:-$DEFAULT_STATE_ROOT}"
case "$STATE_ROOT" in
  /*) ;;
  *) STATE_ROOT="$ROOT/$STATE_ROOT" ;;
esac
BIN="$STATE_ROOT/platformdev"
PID_FILE="$STATE_ROOT/platformdev.pid"
LOG_FILE="$STATE_ROOT/platformdev.log"
GO_CACHE="$STATE_ROOT/go-cache"
STATE_MARKER="$STATE_ROOT/.vastplan-platform-dev-state"

GATEWAY_PORT="${VASTPLAN_DEV_GATEWAY_PORT:-18080}"
PORTAL_PORT="${VASTPLAN_DEV_PORTAL_PORT:-18444}"
ARTIFACT_PORT="${VASTPLAN_DEV_ARTIFACT_PORT:-18443}"
ARTIFACT_PROTOCOL="${VASTPLAN_DEV_ARTIFACT_PROTOCOL:-local-test}"
SEED_ARTIFACT_PORT="${VASTPLAN_DEV_SEED_ARTIFACT_PORT:-18442}"
VAULT_PORT="${VASTPLAN_DEV_VAULT_PORT:-18200}"
RECOVERY_PORT="${VASTPLAN_DEV_RECOVERY_PORT:-18441}"
STATUS_URL="http://127.0.0.1:$GATEWAY_PORT/__vastplan_dev/status"
PORTAL_URL="http://127.0.0.1:$GATEWAY_PORT/operations"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  RED=$'\033[0;31m'
  GREEN=$'\033[0;32m'
  YELLOW=$'\033[1;33m'
  BLUE=$'\033[0;34m'
  CYAN=$'\033[0;36m'
  DIM=$'\033[2m'
  NC=$'\033[0m'
else
  RED=""
  GREEN=""
  YELLOW=""
  BLUE=""
  CYAN=""
  DIM=""
  NC=""
fi

info() { printf '%s%s%s\n' "$CYAN" "$*" "$NC"; }
success() { printf '%s✓ %s%s\n' "$GREEN" "$*" "$NC"; }
warn() { printf '%s⚠ %s%s\n' "$YELLOW" "$*" "$NC"; }
fail() { printf '%s✗ %s%s\n' "$RED" "$*" "$NC" >&2; }

usage() {
  cat <<EOF
VastPlan 本地平台管理中心

用法:
  $0 up [--debug] [--fresh] [--no-hot] [--timeout 秒]
  $0 restart [--debug] [--fresh] [--no-hot] [--timeout 秒]
  $0 bootstrap [--rebuild-seed] [--debug] [--fresh] [--no-hot] [--timeout 秒]
  $0 down
  $0 status
  $0 logs [--follow] [--lines 行数]
  $0 doctor
  $0 publish-test <插件制品.tar.gz> [--backend-target deployment/unit] [--frontend-target portal-id] [--frontend-scope application-plugin|platform-profile-plugin]
  $0 dev-plugin <插件ID或目录> --backend-target deployment/unit [--backend-binding id]
  $0 plugin-library install --remote-profile 文件 --remote-token-file 文件 --remote-trust 文件 --plugin ID --version 版本 --target 内核 [--channel stable]
  $0 plugin-release <plan|prepare|execute> <Release Spec YAML> [--out Release Plan JSON]
  $0 sync-contracts [--write]
  $0 clean
  $0 help

命令:
  up         只启动内核并恢复已有期望态，不执行任何发布（默认命令）
  restart    优雅停止后按无发布模式重新启动
  bootstrap  显式发布/更新平台基础组合后启动；默认复用 stable LKG，不发布示例业务服务
  down       优雅停止当前平台及其受管子进程
  status     显示编排器与开发网关状态
  logs       显示最近日志；加 --follow/-f 持续跟踪
  doctor     检查依赖、运行状态和固定端口
  publish-test 以 testing channel 签名并上传唯一 dev.* 预发布制品；可选提交 Backend Test Release
  dev-plugin  监听一个 Backend 插件，增量构建 workspace 候选并通过 Test Release 热切换
  plugin-library 从 remote.v1 下载精确插件并导入本地插件库，不直接激活服务
  plugin-release 从插件 Manifest 与 Contract Registry 生成影响计划；开发 execute 复用 local-test/Test Release 热切换
  sync-contracts 检查 Contract Registry 全链条；--write 只更新机械派生文件
  clean      平台停止后删除 .vastplan/dev-platform 运行数据

up/restart 参数:
  --debug, -d       前台运行并实时显示日志，Ctrl+C 停止
  --fresh           启动前删除旧运行数据和构建缓存
  --no-hot          关闭默认启用的前端插件事务式热替换
  --rebuild-seed     仅 bootstrap：按新 stable refs 重建并晋级 Seed Runtime
  --timeout 秒      启动等待时间，默认 ${VASTPLAN_DEV_TIMEOUT:-300} 秒

环境变量:
  VASTPLAN_DEV_STATE_ROOT      覆盖本地运行目录
  VASTPLAN_DEV_TIMEOUT         覆盖默认启动超时
  VASTPLAN_DEV_GATEWAY_PORT    开发网关端口（默认 18080）
  VASTPLAN_DEV_PORTAL_PORT     Node Portal Kernel 内部端口（默认 18444）
  VASTPLAN_DEV_ARTIFACT_PROTOCOL 开发仓库协议：local-test（默认）或仅诊断用 remote-compat
  VASTPLAN_DEV_ARTIFACT_PORT   remote-compat 制品服务内部端口（默认 18443）
  VASTPLAN_DEV_SEED_ARTIFACT_PORT Seed 制品仓库端口（默认 18442）
  VASTPLAN_DEV_VAULT_PORT      Vault 桩内部端口（默认 18200）
  VASTPLAN_DEV_RECOVERY_PORT   Kernel Recovery 只读端口（默认 18441）
EOF
}

# Helpers share only the explicitly initialized variables above; command dispatch
# and user-facing lifecycle orchestration remain in this entrypoint.
source "$ROOT/engineering/tools/platform-dev-support.sh"
source "$ROOT/engineering/tools/platform-dev-plugin-workflows.sh"

start_runtime() {
  local debug="$1"
  local fresh="$2"
  local timeout="$3"
  local pid owned status

  validate_configuration
  validate_uint "启动超时" "$timeout"
  if pid="$(running_pid)"; then
    success "平台管理中心已经运行 pid=$pid"
    printf '%s\n' "$PORTAL_URL"
    return 0
  fi

  owned="$(owned_runtime_pids | unique_pids || true)"
  if [ -n "$owned" ]; then
    warn "发现上次异常退出留下的本项目进程，正在按运行目录安全清理"
    terminate_owned_pids "$owned"
  fi
  if [ "$fresh" = true ]; then
    clean_state
  fi
  ensure_state_dirs
  check_dependencies
  check_ports_free
	align_node_dependencies
  build_orchestrator
  build_portal_host
  runtime_arguments "$debug"
  # 开发工作区中的 Runtime Host 由 pnpm 以稳定命令名链接到此目录。
  # 只扩展当前编排器及其子进程的 PATH，不写入用户全局环境。
  export PATH="$ROOT/node_modules/.bin:$PATH"
  export VASTPLAN_NODE_WORKER_HOST="$ROOT/core/runtimehosts/node-worker/host.mjs"
  export VASTPLAN_PYTHON_SUBINTERPRETER_HOST="$ROOT/core/runtimehosts/python-subinterpreter/host.py"
  : > "$LOG_FILE"

  if [ "$debug" = true ]; then
    if [ "$APPLY_PLATFORM" = true ]; then
      info "前台执行平台基础发布并启动内核；按 Ctrl+C 优雅停止"
    else
      info "前台启动内核并恢复已有期望态（零发布）；按 Ctrl+C 优雅停止"
    fi
    info "Portal 就绪后地址: $PORTAL_URL"
    set +e
    "$BIN" "${RUNTIME_ARGS[@]}" 2>&1 | tee "$LOG_FILE"
    status="${PIPESTATUS[0]}"
    set -e
    rm -f "$PID_FILE"
    if [ "$status" -ne 0 ]; then
      fail "平台管理中心退出，状态码: $status"
      return "$status"
    fi
    success "平台管理中心已停止"
    return 0
  fi

  if [ "$APPLY_PLATFORM" = true ]; then
    info "后台执行平台基础发布并启动；首次运行通常需要 1–3 分钟..."
  else
    info "后台启动内核并恢复已有期望态（零发布）..."
  fi
  nohup "$BIN" "${RUNTIME_ARGS[@]}" > "$LOG_FILE" 2>&1 &
  pid=$!
  printf '%s\n' "$pid" > "$PID_FILE"
  if ! wait_until_ready "$pid" "$timeout"; then
    stop_runtime || true
    return 1
  fi
  success "平台管理中心已就绪: $PORTAL_URL"
  printf '日志: %s\n' "$LOG_FILE"
}

show_status() {
  local pid
  if ! pid="$(running_pid)"; then
    info "平台管理中心未运行"
    return 1
  fi
  success "平台管理中心进程运行中 pid=$pid"
  if ! curl --silent --show-error --fail "$STATUS_URL"; then
    warn "进程存在，但开发网关尚未就绪；查看 '$0 logs'"
    return 1
  fi
  printf '\nPortal: %s\n' "$PORTAL_URL"
}

show_logs() {
  local follow="$1"
  local lines="$2"
  if [ ! -f "$LOG_FILE" ]; then
    fail "日志文件不存在: $LOG_FILE"
    return 1
  fi
  if [ "$follow" = true ]; then
    tail -n "$lines" -f "$LOG_FILE"
  else
    tail -n "$lines" "$LOG_FILE"
  fi
}

doctor() {
  local failed=0 pid owned
  info "VastPlan 本地开发环境检查"
  printf '项目目录: %s\n运行目录: %s\n' "$ROOT" "$STATE_ROOT"
  if check_dependencies; then
    success "基础命令完整"
    printf '  Go:   %s\n' "$(go version 2>/dev/null)"
    printf '  Node: %s\n' "$(node --version 2>/dev/null)"
    printf '  pnpm: %s\n' "$(pnpm --version 2>/dev/null)"
  else
    failed=1
  fi
  if pid="$(running_pid)"; then
    success "平台管理中心运行中 pid=$pid"
    if curl --silent --fail "$STATUS_URL" >/dev/null 2>&1; then
      success "开发网关已就绪: $PORTAL_URL"
    else
      warn "编排器存在，但开发网关尚未就绪"
      failed=1
    fi
  else
    info "平台管理中心未运行"
    owned="$(owned_runtime_pids | unique_pids || true)"
    if [ -n "$owned" ]; then
      warn "发现本项目残留进程: $(printf '%s' "$owned" | tr '\n' ' ')"
      failed=1
    elif ! check_ports_free; then
      failed=1
    else
      success "固定开发端口可用"
    fi
  fi
  if [ "$failed" -ne 0 ]; then
    fail "环境检查发现问题"
    return 1
  fi
  success "环境检查通过"
}

DEBUG_MODE=false
FRESH_MODE=false
START_TIMEOUT="${VASTPLAN_DEV_TIMEOUT:-300}"
LOG_FOLLOW=false
LOG_LINES=100
HOT_MODE=true
APPLY_PLATFORM=false
REBUILD_SEED=false

parse_start_options() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --debug|-d) DEBUG_MODE=true ;;
      --fresh) FRESH_MODE=true ;;
      --no-hot) HOT_MODE=false ;;
      --rebuild-seed) REBUILD_SEED=true ;;
      --timeout)
        if [ "$#" -lt 2 ]; then
          fail "--timeout 缺少秒数"
          return 2
        fi
        START_TIMEOUT="$2"
        shift
        ;;
      --help|-h) usage; exit 0 ;;
      *) fail "未知参数: $1"; usage >&2; return 2 ;;
    esac
    shift
  done
  validate_uint "启动超时" "$START_TIMEOUT"
}

parse_log_options() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --follow|-f) LOG_FOLLOW=true ;;
      --lines|-n)
        if [ "$#" -lt 2 ]; then
          fail "$1 缺少行数"
          return 2
        fi
        LOG_LINES="$2"
        shift
        ;;
      --help|-h) usage; exit 0 ;;
      *) fail "未知参数: $1"; usage >&2; return 2 ;;
    esac
    shift
  done
  validate_uint "日志行数" "$LOG_LINES"
}

COMMAND="${1:-up}"
if [ "$#" -gt 0 ]; then
  case "$1" in
    -*) COMMAND="up" ;;
    *) shift ;;
  esac
fi

case "$COMMAND" in
  up)
    parse_start_options "$@"
    [ "$REBUILD_SEED" = false ] || { fail "--rebuild-seed 只能用于 bootstrap"; exit 2; }
    start_runtime "$DEBUG_MODE" "$FRESH_MODE" "$START_TIMEOUT"
    ;;
  restart)
    parse_start_options "$@"
    [ "$REBUILD_SEED" = false ] || { fail "--rebuild-seed 只能用于 bootstrap"; exit 2; }
    stop_runtime
    start_runtime "$DEBUG_MODE" "$FRESH_MODE" "$START_TIMEOUT"
    ;;
  bootstrap)
    APPLY_PLATFORM=true
    parse_start_options "$@"
    stop_runtime
    start_runtime "$DEBUG_MODE" "$FRESH_MODE" "$START_TIMEOUT"
    ;;
  down)
    [ "$#" -eq 0 ] || { fail "down 不接受参数"; exit 2; }
    stop_runtime
    ;;
  status)
    [ "$#" -eq 0 ] || { fail "status 不接受参数"; exit 2; }
    show_status
    ;;
  logs)
    parse_log_options "$@"
    show_logs "$LOG_FOLLOW" "$LOG_LINES"
    ;;
  doctor)
    [ "$#" -eq 0 ] || { fail "doctor 不接受参数"; exit 2; }
    validate_configuration
    doctor
    ;;
  publish-test)
    [ "$#" -ge 1 ] || { fail "publish-test 需要一个插件 .tar.gz 文件"; exit 2; }
    publish_test_artifact "$@"
    ;;
  dev-plugin)
    [ "$#" -ge 1 ] || { fail "dev-plugin 需要插件 ID 或目录"; exit 2; }
    selector="$1"
    shift
    develop_backend_plugin "$selector" "$@"
    ;;
  plugin-library)
    [ "${1:-}" = "install" ] || { fail "plugin-library 当前只支持 install"; exit 2; }
    shift
    install_remote_plugin "$@"
    ;;
  plugin-release)
    [ "$#" -ge 2 ] || { fail "plugin-release 需要 <plan|prepare|execute> 与 Release Spec YAML"; exit 2; }
    release_plugins "$@"
    ;;
  sync-contracts)
    sync_contracts "$@"
    ;;
  clean)
    [ "$#" -eq 0 ] || { fail "clean 不接受参数"; exit 2; }
    clean_state
    ;;
  help|--help|-h)
    usage
    ;;
  *)
    fail "未知命令: $COMMAND"
    usage >&2
    exit 2
    ;;
esac
