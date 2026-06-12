#!/usr/bin/env bash
# 文件作用：开发环境一键安装本地 Node Browser Worker 到 GoodHR Go 本地程序运行目录。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKER_DIR="$ROOT_DIR/worker-node"
AGENT_PORT="${GOODHR_AGENT_PORT:-95271}"
BASE_URL="${GOODHR_LOCAL_AGENT_URL:-http://127.0.0.1:$AGENT_PORT}"
NPM_REGISTRY="${GOODHR_NPM_REGISTRY:-https://registry.npmmirror.com}"
LOG_DIR="$ROOT_DIR/logs"
AGENT_BIN="$LOG_DIR/goodhr-local-agent-dev"
RUNTIME_WORKER_DIR="${GOODHR_RUNTIME_WORKER_DIR:-$HOME/Library/Application Support/GoodHR/runtime/browser-worker}"

# log 输出脚本状态。
# 参数为要显示的中文消息。
log() {
  printf '[GoodHR] %s\n' "$*"
}

# port_pid 返回占用本地端口的进程 ID。
# 无参数。
port_pid() {
  lsof -ti "tcp:$AGENT_PORT" -sTCP:LISTEN 2>/dev/null | head -n 1 || true
}

# wait_port_free 等待本地程序端口释放。
# 无参数。
wait_port_free() {
  for _ in $(seq 1 50); do
    if [ -z "$(port_pid)" ]; then
      return 0
    fi
    sleep 0.2
  done
  log "$AGENT_PORT 端口一直没有释放，请先手动关闭占用进程"
  exit 1
}

# is_goodhr_agent_pid 判断 pid 是否是 GoodHR 本地程序。
# 参数为进程 ID。
is_goodhr_agent_pid() {
  local pid="${1:-}"
  local command_text
  if [ -z "$pid" ]; then
    return 1
  fi
  command_text="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  case "$command_text" in
    *goodhr-local-agent*|*GoodHRLocalAgent*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

# verify_runtime_worker 验证运行目录中的 Worker 依赖是否完整。
# 无参数。
verify_runtime_worker() {
  local dependency_path
  dependency_path="$RUNTIME_WORKER_DIR/node_modules/cloakbrowser/package.json"
  if [ ! -f "$dependency_path" ]; then
    log "Worker 依赖未安装完整：$dependency_path"
    exit 1
  fi
}

# ensure_worker_dependencies 确保 Node Worker 运行依赖已经安装。
# 无参数。
ensure_worker_dependencies() {
  if [ -d "$WORKER_DIR/node_modules/cloakbrowser" ]; then
    log "Node Worker 依赖已存在"
    return 0
  fi
  if ! command -v npm >/dev/null 2>&1; then
    log "未找到 npm，无法安装 Node Worker 依赖。请先安装 Node.js。"
    exit 1
  fi
  log "未找到 node_modules/cloakbrowser，准备安装 Node Worker 依赖"
  log "npm registry：$NPM_REGISTRY"
  (
    cd "$WORKER_DIR"
    npm install --omit=dev --registry="$NPM_REGISTRY"
  )
}

# build_agent_binary 构建开发用本地程序二进制。
# 无参数。
build_agent_binary() {
  log "正在构建 Go 本地程序"
  (
    cd "$ROOT_DIR"
    go build -o "$AGENT_BIN" ./cmd/goodhr-local-agent
  )
}

# install_worker_direct 直接把本地 Worker 源码复制到运行目录。
# 无参数。
install_worker_direct() {
  log "正在复制本地 Worker 到运行目录：$RUNTIME_WORKER_DIR"
  mkdir -p "$(dirname "$RUNTIME_WORKER_DIR")"
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete "$WORKER_DIR/" "$RUNTIME_WORKER_DIR/"
  else
    rm -rf "$RUNTIME_WORKER_DIR"
    mkdir -p "$RUNTIME_WORKER_DIR"
    cp -R "$WORKER_DIR/." "$RUNTIME_WORKER_DIR/"
  fi
  verify_runtime_worker
}

# start_agent_foreground 前台启动 Go 本地程序。
# 无参数。
start_agent_foreground() {
  build_agent_binary
  log "正在前台启动 Go 本地程序，按 Ctrl+C 可停止"
  log "控制台地址：$BASE_URL/admin/"
  "$AGENT_BIN" -port "$AGENT_PORT" -open-console=false
}

# stop_agent 停止指定 pid 的本地程序。
# 参数为进程 ID。
stop_agent() {
  local pid="${1:-}"
  if [ -z "$pid" ]; then
    return 0
  fi
  if ! is_goodhr_agent_pid "$pid"; then
    log "$AGENT_PORT 端口被其他程序占用，避免误杀进程 pid=$pid"
    log "请先手动释放 $AGENT_PORT，或设置 GOODHR_AGENT_PORT 指定其他端口启动"
    exit 1
  fi
  log "正在停止旧本地程序 pid=$pid"
  kill "$pid" >/dev/null 2>&1 || true
  for _ in $(seq 1 50); do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      wait_port_free
      return 0
    fi
    sleep 0.2
  done
  log "旧本地程序未正常退出，强制停止 pid=$pid"
  kill -9 "$pid" >/dev/null 2>&1 || true
  wait_port_free
}

if [ ! -d "$WORKER_DIR" ]; then
  log "Node Worker 源码目录不存在：$WORKER_DIR"
  exit 1
fi

log "准备安装本地 Node Worker"
log "Local Agent：$BASE_URL"
log "Worker 源码：$WORKER_DIR"

ensure_worker_dependencies
install_worker_direct

if [ "${GOODHR_INSTALL_ONLY:-0}" = "1" ]; then
  log "Worker 已安装完成，本次按 GOODHR_INSTALL_ONLY=1 跳过启动本地程序"
  exit 0
fi

OLD_PID="$(port_pid)"
if [ -n "$OLD_PID" ]; then
  stop_agent "$OLD_PID"
fi

log "Worker 已安装完成"
start_agent_foreground
