#!/bin/sh
# 本文件用于宝塔定时任务自动部署 GoodHR 5；检测 main 分支有更新时自动拉取、构建、重启并清理 Docker 缓存。

set -eu

PROJECT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
LOG_FILE="$PROJECT_DIR/deploy.log"
LOCK_DIR="$PROJECT_DIR/.deploy.lock"
BRANCH="${DEPLOY_BRANCH:-main}"
REMOTE="${DEPLOY_REMOTE:-origin}"
COMPOSE_FILE="${DEPLOY_COMPOSE_FILE:-docker-compose.server.yml}"

# log 写入带时间的部署日志。
log() {
  printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >> "$LOG_FILE"
}

# cleanup_lock 释放部署锁，避免脚本异常后影响下一次定时任务。
cleanup_lock() {
  rmdir "$LOCK_DIR" 2>/dev/null || true
}

# compose_cmd 自动选择当前服务器可用的 Docker Compose 命令。
compose_cmd() {
  if docker compose version >/dev/null 2>&1; then
    printf '%s\n' "docker compose"
    return 0
  fi
  if command -v docker-compose >/dev/null 2>&1; then
    printf '%s\n' "docker-compose"
    return 0
  fi
  return 1
}

if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  log "上一次部署仍在执行，本次跳过"
  exit 0
fi
trap cleanup_lock EXIT INT TERM

cd "$PROJECT_DIR"
log "开始检查更新 branch=$BRANCH remote=$REMOTE compose=$COMPOSE_FILE"

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  log "当前目录不是 Git 仓库，部署终止"
  exit 1
fi

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  log "检测到未提交改动，避免覆盖本地文件，本次部署终止"
  exit 1
fi

git fetch "$REMOTE" "$BRANCH" >> "$LOG_FILE" 2>&1

LOCAL_COMMIT="$(git rev-parse HEAD)"
REMOTE_COMMIT="$(git rev-parse "$REMOTE/$BRANCH")"

if [ "$LOCAL_COMMIT" = "$REMOTE_COMMIT" ]; then
  log "代码无更新，本次跳过"
  exit 0
fi

log "发现更新 local=$LOCAL_COMMIT remote=$REMOTE_COMMIT"
git checkout "$BRANCH" >> "$LOG_FILE" 2>&1
git pull --ff-only "$REMOTE" "$BRANCH" >> "$LOG_FILE" 2>&1

COMPOSE="$(compose_cmd)" || {
  log "未找到 docker compose 或 docker-compose，部署终止"
  exit 1
}

log "开始 Docker 构建"
$COMPOSE -f "$COMPOSE_FILE" build --no-cache >> "$LOG_FILE" 2>&1

log "开始 Docker 重启"
$COMPOSE -f "$COMPOSE_FILE" up -d --remove-orphans >> "$LOG_FILE" 2>&1

log "开始清理 Docker 缓存"
docker image prune -f >> "$LOG_FILE" 2>&1 || true
docker builder prune -f >> "$LOG_FILE" 2>&1 || true
docker container prune -f >> "$LOG_FILE" 2>&1 || true

log "部署完成 commit=$(git rev-parse --short HEAD)"
