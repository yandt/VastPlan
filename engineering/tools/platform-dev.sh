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
  $0 bootstrap [--debug] [--fresh] [--no-hot] [--timeout 秒]
  $0 down
  $0 status
  $0 logs [--follow] [--lines 行数]
  $0 doctor
  $0 publish-test <插件制品.tar.gz> [--backend-target deployment/unit] [--frontend-target portal-id] [--frontend-scope application-plugin|platform-profile-plugin]
  $0 clean
  $0 help

命令:
  up         只启动内核并恢复已有期望态，不执行任何发布（默认命令）
  restart    优雅停止后按无发布模式重新启动
  bootstrap  显式发布/更新平台基础组合后启动；不会发布示例业务服务
  down       优雅停止当前平台及其受管子进程
  status     显示编排器与开发网关状态
  logs       显示最近日志；加 --follow/-f 持续跟踪
  doctor     检查依赖、运行状态和固定端口
  publish-test 以 testing channel 签名并上传唯一 dev.* 预发布制品；可选提交 Backend Test Release
  clean      平台停止后删除 .vastplan/dev-platform 运行数据

up/restart 参数:
  --debug, -d       前台运行并实时显示日志，Ctrl+C 停止
  --fresh           启动前删除旧运行数据和构建缓存
  --no-hot          关闭默认启用的前端插件事务式热替换
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
EOF
}

validate_uint() {
  local label="$1"
  local value="$2"
  case "$value" in
    ''|*[!0-9]*) fail "$label 必须是正整数: $value"; return 1 ;;
  esac
  if [ "$value" -le 0 ]; then
    fail "$label 必须大于 0: $value"
    return 1
  fi
}

validate_port() {
  local label="$1"
  local value="$2"
  validate_uint "$label" "$value" || return 1
  if [ "$value" -gt 65535 ]; then
    fail "$label 超出 TCP 端口范围: $value"
    return 1
  fi
}

validate_configuration() {
	case "$ARTIFACT_PROTOCOL" in
	  local-test|remote-compat) ;;
	  *) fail "开发仓库协议只允许 local-test 或 remote-compat: $ARTIFACT_PROTOCOL"; return 1 ;;
	esac
  validate_port "开发网关端口" "$GATEWAY_PORT"
  validate_port "Node Portal Kernel 端口" "$PORTAL_PORT"
  validate_port "制品服务端口" "$ARTIFACT_PORT"
  validate_port "Seed 制品仓库端口" "$SEED_ARTIFACT_PORT"
  validate_port "Vault 桩端口" "$VAULT_PORT"
  if [ "$GATEWAY_PORT" = "$PORTAL_PORT" ] ||
     [ "$GATEWAY_PORT" = "$ARTIFACT_PORT" ] ||
     [ "$GATEWAY_PORT" = "$SEED_ARTIFACT_PORT" ] ||
     [ "$GATEWAY_PORT" = "$VAULT_PORT" ] ||
     [ "$PORTAL_PORT" = "$ARTIFACT_PORT" ] ||
     [ "$PORTAL_PORT" = "$SEED_ARTIFACT_PORT" ] ||
     [ "$PORTAL_PORT" = "$VAULT_PORT" ] ||
     [ "$ARTIFACT_PORT" = "$SEED_ARTIFACT_PORT" ] ||
     [ "$ARTIFACT_PORT" = "$VAULT_PORT" ] ||
     [ "$SEED_ARTIFACT_PORT" = "$VAULT_PORT" ]; then
    fail "开发服务端口必须互不相同"
    return 1
  fi
}

ensure_state_dirs() {
  mkdir -p "$STATE_ROOT" "$GO_CACHE"
  printf 'VastPlan platform-dev state v1\n' > "$STATE_MARKER"
}

process_command() {
  ps -p "$1" -o command= 2>/dev/null || true
}

is_platform_process() {
  local pid="$1"
  local command
  command="$(process_command "$pid")"
  case "$command" in
    "$BIN"|"$BIN "*) return 0 ;;
    *) return 1 ;;
  esac
}

discover_platform_pid() {
  local pid command
  while read -r pid command; do
    case "$command" in
      "$BIN"|"$BIN "*)
        if kill -0 "$pid" 2>/dev/null; then
          printf '%s' "$pid"
          return 0
        fi
        ;;
    esac
  done < <(ps -axo pid=,command= 2>/dev/null || true)
  return 1
}

running_pid() {
  local pid=""
  if [ -f "$PID_FILE" ]; then
    pid="$(tr -d '[:space:]' < "$PID_FILE")"
    case "$pid" in
      ''|*[!0-9]*)
        warn "已清理无效 PID 文件: $PID_FILE" >&2
        rm -f "$PID_FILE"
        pid=""
        ;;
    esac
    if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$PID_FILE"
      pid=""
    fi
    if [ -n "$pid" ] && ! is_platform_process "$pid"; then
      warn "PID $pid 已被其他进程复用；仅清理 PID 文件，不会终止该进程" >&2
      rm -f "$PID_FILE"
      pid=""
    fi
  fi
  if [ -z "$pid" ]; then
    pid="$(discover_platform_pid || true)"
    if [ -n "$pid" ]; then
      ensure_state_dirs
      printf '%s\n' "$pid" > "$PID_FILE"
      warn "已从进程表恢复缺失的 PID 文件（pid=${pid}）" >&2
    fi
  fi
  if [ -z "$pid" ]; then
    return 1
  fi
  printf '%s' "$pid"
}

owned_runtime_pids() {
  local pid command
  while read -r pid command; do
    case "$command" in
      "$BIN"|"$BIN "*|"$STATE_ROOT"/runs/*)
        if kill -0 "$pid" 2>/dev/null; then
          printf '%s\n' "$pid"
        fi
        ;;
    esac
  done < <(ps -axo pid=,command= 2>/dev/null || true)
}

unique_pids() {
  awk 'NF && !seen[$1]++ { print $1 }'
}

terminate_owned_pids() {
  local pids="$1"
  local pid elapsed remaining
  [ -n "$pids" ] || return 0
  while read -r pid; do
    [ -n "$pid" ] && kill -TERM "$pid" 2>/dev/null || true
  done <<EOF
$pids
EOF
  elapsed=0
  while [ "$elapsed" -lt 12 ]; do
    remaining=""
    while read -r pid; do
      if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        remaining="$remaining $pid"
      fi
    done <<EOF
$pids
EOF
    [ -z "$remaining" ] && return 0
    sleep 1
    elapsed=$((elapsed + 1))
  done
  while read -r pid; do
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      warn "进程 $pid 未及时退出，执行强制停止"
      kill -KILL "$pid" 2>/dev/null || true
    fi
  done <<EOF
$pids
EOF
}

stop_runtime() {
  local pid owned
  pid="$(running_pid || true)"
  if [ -n "$pid" ]; then
    info "正在停止平台管理中心 pid=$pid ..."
    kill -TERM "$pid" 2>/dev/null || true
    local elapsed=0
    while [ "$elapsed" -lt 30 ]; do
      if ! kill -0 "$pid" 2>/dev/null; then
        rm -f "$PID_FILE"
        success "平台管理中心已停止"
        return 0
      fi
      sleep 1
      elapsed=$((elapsed + 1))
    done
    warn "编排器未在 30 秒内退出，清理其受管进程"
  fi

  owned="$(owned_runtime_pids | unique_pids || true)"
  if [ -n "$owned" ]; then
    terminate_owned_pids "$owned"
    rm -f "$PID_FILE"
    success "VastPlan 本地受管进程已停止"
    return 0
  fi
  rm -f "$PID_FILE"
  if [ -z "$pid" ]; then
    info "平台管理中心未运行"
  else
    success "平台管理中心已停止"
  fi
}

check_dependencies() {
  local missing=0
  local command
  for command in go node pnpm curl cc ps awk tail nohup tee; do
    if ! command -v "$command" >/dev/null 2>&1; then
      fail "缺少命令: $command"
      missing=1
    fi
  done
  if [ "$missing" -ne 0 ]; then
    return 1
  fi
  if [ ! -f "$ROOT/go.mod" ] || [ ! -f "$ROOT/package.json" ]; then
    fail "项目根目录缺少 go.mod 或 package.json: $ROOT"
    return 1
  fi
}

align_node_dependencies() {
  info "[准备] 按 pnpm-lock.yaml 离线对齐 Node 工作区依赖..."
  if ! (cd "$ROOT" && pnpm install --offline --frozen-lockfile); then
    fail "离线依赖对齐失败；请先在项目根目录运行 'pnpm install --frozen-lockfile' 下载锁定依赖"
    return 1
  fi
  success "Node 工作区依赖与锁文件一致"
}

port_in_use() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"$port" -sTCP:LISTEN -t >/dev/null 2>&1
    return $?
  fi
  if command -v ss >/dev/null 2>&1; then
    ss -ltn 2>/dev/null | awk -v port=":$port" '$4 ~ port "$" { found=1 } END { exit !found }'
    return $?
  fi
  if command -v nc >/dev/null 2>&1; then
    nc -z 127.0.0.1 "$port" >/dev/null 2>&1
    return $?
  fi
  return 2
}

port_owner_description() {
  local port="$1"
  local pids pid command descriptions=""
  if ! command -v lsof >/dev/null 2>&1; then
    return 0
  fi
  pids="$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null | unique_pids || true)"
  while read -r pid; do
    [ -n "$pid" ] || continue
    command="$(process_command "$pid")"
    descriptions="$descriptions pid=$pid (${command:-unknown})"
  done <<EOF
$pids
EOF
  printf '%s' "$descriptions"
}

check_ports_free() {
  local failed=0
  local name port rc owner
  while read -r name port; do
    if port_in_use "$port"; then
      owner="$(port_owner_description "$port")"
      fail "$name 端口 $port 已被占用。$owner"
      failed=1
    else
      rc=$?
      if [ "$rc" -eq 2 ]; then
        warn "缺少 lsof/ss/nc，无法预检端口 $port"
      fi
    fi
  done <<EOF
开发网关 $GATEWAY_PORT
Portal-Kernel $PORTAL_PORT
制品服务 $ARTIFACT_PORT
Seed制品仓库 $SEED_ARTIFACT_PORT
Vault-Transit桩 $VAULT_PORT
EOF
  if [ "$failed" -ne 0 ]; then
    fail "不会自动终止端口占用者；请确认进程后手工处理，或先运行 '$0 down' 清理本项目残留"
    return 1
  fi
}

orchestrator_needs_build() {
  local source_root
  if [ ! -x "$BIN" ]; then
    return 0
  fi
  if [ "$ROOT/go.mod" -nt "$BIN" ] || [ "$ROOT/go.sum" -nt "$BIN" ]; then
    return 0
  fi
  for source_root in "$ROOT/core" "$ROOT/contracts" "$ROOT/engineering/tools/platformdev"; do
    if find "$source_root" -type f -name '*.go' -newer "$BIN" -print -quit | grep -q .; then
      return 0
    fi
  done
  return 1
}

build_orchestrator() {
  local temporary
  ensure_state_dirs
  if ! orchestrator_needs_build; then
    success "复用未变化的开发编排器: $BIN"
    return 0
  fi
  info "[准备] 构建本地开发编排器..."
  temporary="$BIN.tmp.$$"
  rm -f "$temporary"
  if ! (cd "$ROOT" && env GOCACHE="$GO_CACHE" go build -trimpath -buildvcs=false -o "$temporary" ./engineering/tools/platformdev); then
    rm -f "$temporary"
    fail "开发编排器构建失败"
    return 1
  fi
  chmod 700 "$temporary"
  mv "$temporary" "$BIN"
  success "开发编排器构建完成"
}

portal_host_needs_build() {
  local output="$ROOT/core/kernels/frontend-host/dist/portal-host.cjs"
  local worker="$ROOT/core/kernels/frontend-host/dist/server-generation-worker.cjs"
  if [ ! -f "$output" ] || [ ! -f "$worker" ]; then
    return 0
  fi
  if [ "$ROOT/core/kernels/frontend-host/package.json" -nt "$output" ] || [ "$ROOT/core/kernels/frontend-host/build.mjs" -nt "$output" ] || [ "$ROOT/pnpm-lock.yaml" -nt "$output" ]; then
    return 0
  fi
  find "$ROOT/core/kernels/frontend-host/src" "$ROOT/extensions/sdk/node/addressing/src" "$ROOT/extensions/sdk/ts/frontend-engine-contract/src" -type f \( -name '*.ts' -o -name '*.json' \) -newer "$output" -print -quit | grep -q .
}

build_portal_host() {
  if ! portal_host_needs_build; then
    success "复用未变化的 Node Portal Kernel"
    return 0
  fi
  info "[准备] 构建 Node Portal Kernel..."
  if ! (cd "$ROOT" && pnpm build:portal-host); then
    fail "Node Portal Kernel 构建失败"
    return 1
  fi
  success "Node Portal Kernel 构建完成"
}

runtime_arguments() {
  RUNTIME_ARGS=(
    -root "$ROOT"
    -state-root "$STATE_ROOT"
    -listen "127.0.0.1:$GATEWAY_PORT"
    -portal-listen "127.0.0.1:$PORTAL_PORT"
    -artifact-listen "127.0.0.1:$ARTIFACT_PORT"
	-artifact-protocol "$ARTIFACT_PROTOCOL"
    -seed-artifact-listen "127.0.0.1:$SEED_ARTIFACT_PORT"
    -vault-listen "127.0.0.1:$VAULT_PORT"
	-hot="$HOT_MODE"
	-apply-platform="$APPLY_PLATFORM"
  )
}

show_recent_log() {
  if [ -f "$LOG_FILE" ]; then
    printf '\n%s最近日志:%s\n' "$BLUE" "$NC" >&2
    tail -n 80 "$LOG_FILE" >&2 || true
  fi
}

wait_until_ready() {
  local pid="$1"
  local timeout="$2"
  local elapsed=0 last_progress="" progress=""
  while [ "$elapsed" -lt "$timeout" ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$PID_FILE"
      fail "平台管理中心启动失败"
      show_recent_log
      return 1
    fi
    if curl --silent --fail "$STATUS_URL" 2>/dev/null | grep -Eq '"ready"[[:space:]]*:[[:space:]]*true'; then
      return 0
    fi
    if [ -f "$LOG_FILE" ]; then
      progress="$(grep -E '\[[1-6]/6\]' "$LOG_FILE" 2>/dev/null | tail -n 1 || true)"
      if [ -n "$progress" ] && [ "$progress" != "$last_progress" ]; then
        printf '%s%s%s\n' "$DIM" "$progress" "$NC"
        last_progress="$progress"
      fi
    fi
    if [ "$elapsed" -gt 0 ] && [ $((elapsed % 15)) -eq 0 ]; then
      info "仍在启动中：${elapsed}/${timeout} 秒"
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  fail "等待平台管理中心就绪超时（${timeout} 秒）"
  show_recent_log
  return 1
}

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
  runtime_arguments
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

parse_start_options() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --debug|-d) DEBUG_MODE=true ;;
      --fresh) FRESH_MODE=true ;;
      --no-hot) HOT_MODE=false ;;
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
    start_runtime "$DEBUG_MODE" "$FRESH_MODE" "$START_TIMEOUT"
    ;;
  restart)
    parse_start_options "$@"
    stop_runtime
    start_runtime "$DEBUG_MODE" "$FRESH_MODE" "$START_TIMEOUT"
    ;;
  bootstrap)
    parse_start_options "$@"
    APPLY_PLATFORM=true
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
