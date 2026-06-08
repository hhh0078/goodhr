#!/usr/bin/env bash
# 文件作用：开发环境一键安装本地 Node Browser Worker 到 GoodHR Go 本地程序运行目录。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKER_DIR="$ROOT_DIR/worker-node"
BASE_URL="${GOODHR_LOCAL_AGENT_URL:-http://127.0.0.1:9001}"
AGENT_PID=""

# log 输出脚本状态。
# 参数为要显示的中文消息。
log() {
  printf '[GoodHR] %s\n' "$*"
}

# cleanup 清理脚本临时启动的本地程序。
# 无参数。
cleanup() {
  if [ -n "$AGENT_PID" ]; then
    log "正在停止脚本临时启动的本地程序 pid=$AGENT_PID"
    kill "$AGENT_PID" >/dev/null 2>&1 || true
    wait "$AGENT_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# agent_health_ok 判断本地程序 health 是否可访问。
# 无参数，成功返回 0。
agent_health_ok() {
  curl -fsS "$BASE_URL/health" >/dev/null 2>&1
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

if [ ! -d "$WORKER_DIR" ]; then
  log "Node Worker 源码目录不存在：$WORKER_DIR"
  exit 1
fi

log "准备安装本地 Node Worker"
log "Local Agent：$BASE_URL"
log "Worker 源码：$WORKER_DIR"

if agent_health_ok; then
  log "检测到本地程序已运行，将直接安装 Worker"
else
  log "未检测到本地程序，临时启动 Go 本地程序"
  (
    cd "$ROOT_DIR"
    go run ./cmd/goodhr-local-agent
  ) &
  AGENT_PID="$!"
  wait_agent_ready
fi

log "正在安装本地 Worker 到运行目录"
curl -fsS -X POST "$BASE_URL/api/v1/runtime/install-local-worker" \
  -H 'Content-Type: application/json' \
  -d "{\"source_dir\":\"$WORKER_DIR\"}"
printf '\n'

log "Worker 已安装完成"
if [ -n "$AGENT_PID" ]; then
  log "脚本会自动关闭临时启动的本地程序"
else
  log "本地程序原本就在运行，请手动重启它，让新的 Worker 生效"
fi
log "重新启动命令：cd \"$ROOT_DIR\" && go run ./cmd/goodhr-local-agent"
