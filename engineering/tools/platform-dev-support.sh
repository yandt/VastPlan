#!/usr/bin/env bash
# Shared validation, process ownership, dependency, build and readiness helpers
# for platform-dev.sh. This file is sourced and is not a standalone command.

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
  validate_port "Kernel Recovery 端口" "$RECOVERY_PORT"
  if [ "$GATEWAY_PORT" = "$PORTAL_PORT" ] ||
     [ "$GATEWAY_PORT" = "$ARTIFACT_PORT" ] ||
     [ "$GATEWAY_PORT" = "$SEED_ARTIFACT_PORT" ] ||
     [ "$GATEWAY_PORT" = "$VAULT_PORT" ] ||
     [ "$PORTAL_PORT" = "$ARTIFACT_PORT" ] ||
     [ "$PORTAL_PORT" = "$SEED_ARTIFACT_PORT" ] ||
     [ "$PORTAL_PORT" = "$VAULT_PORT" ] ||
     [ "$ARTIFACT_PORT" = "$SEED_ARTIFACT_PORT" ] ||
     [ "$ARTIFACT_PORT" = "$VAULT_PORT" ] ||
     [ "$SEED_ARTIFACT_PORT" = "$VAULT_PORT" ] ||
     [ "$GATEWAY_PORT" = "$RECOVERY_PORT" ] ||
     [ "$PORTAL_PORT" = "$RECOVERY_PORT" ] ||
     [ "$ARTIFACT_PORT" = "$RECOVERY_PORT" ] ||
     [ "$SEED_ARTIFACT_PORT" = "$RECOVERY_PORT" ] ||
     [ "$VAULT_PORT" = "$RECOVERY_PORT" ]; then
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

platform_process_executable() {
  local pid="$1"
  if ! command -v lsof >/dev/null 2>&1; then
    return 1
  fi
  lsof -a -p "$pid" -d txt -Fn 2>/dev/null | awk '/^n/ { print substr($0, 2); exit }'
}

is_platform_process() {
  local pid="$1"
  local command executable
  command="$(process_command "$pid")"
  if [ -n "$command" ]; then
    case "$command" in
      "$BIN"|"$BIN "*) return 0 ;;
      *) return 1 ;;
    esac
  fi
  # Some managed/sandboxed shells allow signalling the process but deny ps.
  # Verify the executable through its open text image before trusting the PID.
  executable="$(platform_process_executable "$pid" || true)"
  [ "$executable" = "$BIN" ]
}

platform_process_alive() {
  local pid="$1"
  kill -0 "$pid" 2>/dev/null || is_platform_process "$pid"
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
  if command -v lsof >/dev/null 2>&1; then
    while read -r pid; do
      if [ -n "$pid" ] && is_platform_process "$pid"; then
        printf '%s' "$pid"
        return 0
      fi
    done < <(lsof -nP -iTCP:"$GATEWAY_PORT" -sTCP:LISTEN -t 2>/dev/null | unique_pids || true)
  fi
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
    if [ -n "$pid" ] && ! platform_process_alive "$pid"; then
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
Kernel-Recovery $RECOVERY_PORT
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
  local debug="$1"
  local detach=true
  if [ "$debug" = true ]; then
    detach=false
  fi
  RUNTIME_ARGS=(
    -root "$ROOT"
    -state-root "$STATE_ROOT"
    -listen "127.0.0.1:$GATEWAY_PORT"
    -portal-listen "127.0.0.1:$PORTAL_PORT"
    -artifact-listen "127.0.0.1:$ARTIFACT_PORT"
	-artifact-protocol "$ARTIFACT_PROTOCOL"
    -seed-artifact-listen "127.0.0.1:$SEED_ARTIFACT_PORT"
    -vault-listen "127.0.0.1:$VAULT_PORT"
    -recovery-listen "127.0.0.1:$RECOVERY_PORT"
	-hot="$HOT_MODE"
	-auto-login="$AUTO_LOGIN"
	-detach="$detach"
	-apply-platform="$APPLY_PLATFORM"
	-rebuild-seed="$REBUILD_SEED"
  )
}

build_seedaccessctl() {
  local output="$STATE_ROOT/seedaccessctl"
  info "[准备] 构建 Seed 管理员本机工具..."
  if ! (cd "$ROOT" && GOCACHE="$GO_CACHE" go build -o "$output" ./engineering/tools/seedaccessctl); then
    fail "Seed 管理员工具构建失败"
    return 1
  fi
  chmod 700 "$output"
}

manage_seed_admin() {
  local action="${1:-}"
  shift || true
  local operator="" password_file=""
  case "$action" in
    init|status) ;;
    *) fail "seed-admin 只支持 init 或 status"; return 2 ;;
  esac
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --operator) [ "$#" -ge 2 ] || { fail "--operator 缺少账号"; return 2; }; operator="$2"; shift ;;
      --password-file) [ "$#" -ge 2 ] || { fail "--password-file 缺少文件"; return 2; }; password_file="$2"; shift ;;
      *) fail "未知 seed-admin 参数: $1"; return 2 ;;
    esac
    shift
  done
  if [ "$action" = init ] && [ -z "$operator" ]; then
    fail "seed-admin init 必须提供 --operator"
    return 2
  fi
  if [ "$action" = init ] && [ -z "$password_file" ] && [ ! -t 0 ]; then
    fail "非交互环境必须显式提供 --password-file"
    return 2
  fi
  ensure_state_dirs
  mkdir -p "$STATE_ROOT/state/authentication"
  build_seedaccessctl
  local temporary_password_file=""
  if [ "$action" = init ] && [ -z "$password_file" ]; then
    if ! temporary_password_file="$("$ROOT/engineering/tools/create-seed-password-file.sh")"; then
      fail "创建临时 Seed 密码文件失败"
      return 1
    fi
    password_file="$temporary_password_file"
  fi
  local args=(-state-file "$STATE_ROOT/state/authentication/seed-access.json" -output human)
  [ -z "$operator" ] || args+=(-operator "$operator")
  [ -z "$password_file" ] || args+=(-password-file "$password_file")
  local status=0
  if "$STATE_ROOT/seedaccessctl" "${args[@]}" "$action"; then
    status=0
  else
    status=$?
  fi
  [ -z "$temporary_password_file" ] || rm -f -- "$temporary_password_file"
  if [ "$status" -eq 0 ] && [ "$action" = init ]; then
    printf '\n下一步，发布并启动平台基础组合：\n  %s bootstrap --rebuild-seed\n' "$ROOT/engineering/tools/platform-dev.sh"
  fi
  return "$status"
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
