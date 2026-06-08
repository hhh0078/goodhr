#!/usr/bin/env bash
# 文件作用：开发环境一键安装本地 Node Browser Worker 到 GoodHR Go 本地程序运行目录。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKER_DIR="$ROOT_DIR/worker-node"
BASE_URL="${GOODHR_LOCAL_AGENT_URL:-http://127.0.0.1:9001}"
AGENT_PID=""
NPM_REGISTRY="${GOODHR_NPM_REGISTRY:-https://registry.npmmirror.com}"
LOG_DIR="$ROOT_DIR/logs"
AGENT_LOG="$LOG_DIR/local-agent-dev.log"
PID_FILE="$LOG_DIR/local-agent-dev.pid"
STARTED_FOR_INSTALL=0

# log 输出脚本状态。
# 参数为要显示的中文消息。
log() {
  printf '[GoodHR] %s\n' "$*"
}

# agent_health_ok 判断本地程序 health 是否可访问。
# 无参数，成功返回 0。
agent_health_ok() {
  curl -fsS "$BASE_URL/health" >/dev/null 2>&1
}

# port_pid 返回占用本地端口的进程 ID。
# 无参数。
port_pid() {
  lsof -ti tcp:9001 -sTCP:LISTEN 2>/dev/null | head -n 1 || true
}

# wait_agent_ready 等待本地程序启动完成。
# 无参数。
wait_agent_ready() {
  for _ in $(seq 1 40); do
    if agent_health_ok; then
      return 0
    fi
    sleep 0.25
  done
  log "本地程序没有在预期时间内启动成功：$BASE_URL"
  exit 1
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

# start_agent_background 在后台启动 Go 本地程序。
# 无参数。
start_agent_background() {
  mkdir -p "$LOG_DIR"
  : >"$AGENT_LOG"
  log "正在后台启动 Go 本地程序，日志：$AGENT_LOG"
  (
    cd "$ROOT_DIR"
    go run ./cmd/goodhr-local-agent
  ) >>"$AGENT_LOG" 2>&1 &
  AGENT_PID="$!"
  printf '%s\n' "$AGENT_PID" >"$PID_FILE"
  wait_agent_ready
}

# stop_agent 停止指定 pid 的本地程序。
# 参数为进程 ID。
stop_agent() {
  local pid="${1:-}"
  if [ -z "$pid" ]; then
    return 0
  fi
  log "正在停止旧本地程序 pid=$pid"
  kill "$pid" >/dev/null 2>&1 || true
  for _ in $(seq 1 20); do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  log "旧本地程序未正常退出，强制停止 pid=$pid"
  kill -9 "$pid" >/dev/null 2>&1 || true
}

if [ ! -d "$WORKER_DIR" ]; then
  log "Node Worker 源码目录不存在：$WORKER_DIR"
  exit 1
fi

log "准备安装本地 Node Worker"
log "Local Agent：$BASE_URL"
log "Worker 源码：$WORKER_DIR"

ensure_worker_dependencies

if agent_health_ok; then
  log "检测到本地程序已运行，将直接安装 Worker"
else
  log "未检测到本地程序，临时启动 Go 本地程序"
  STARTED_FOR_INSTALL=1
  start_agent_background
fi

log "正在安装本地 Worker 到运行目录"
curl -fsS -X POST "$BASE_URL/api/v1/runtime/install-local-worker" \
  -H 'Content-Type: application/json' \
  -d "{\"source_dir\":\"$WORKER_DIR\"}"
printf '\n'

log "Worker 已安装完成"

OLD_PID="$(port_pid)"
if [ -n "$OLD_PID" ]; then
  stop_agent "$OLD_PID"
fi

start_agent_background

log "已完成：Worker 已更新，Go 本地程序已重启"
log "控制台地址：$BASE_URL/admin/"
log "本地程序日志：$AGENT_LOG"
if [ "$STARTED_FOR_INSTALL" = "1" ]; then
  log "本次脚本自动完成临时启动、安装、重启，无需再手动 go run"
fi
