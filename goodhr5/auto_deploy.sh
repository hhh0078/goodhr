#!/bin/sh
# 本文件用于在 Linux 服务器上自动部署 GoodHR 5：仅在目标分支更新时拉取代码，并按变更范围更新对应服务。

set -eu

PROJECT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
LOG_FILE="$PROJECT_DIR/deploy.log"
LOCK_DIR="$PROJECT_DIR/.deploy.lock"
BRANCH="${DEPLOY_BRANCH:-main}"
REMOTE="${DEPLOY_REMOTE:-origin}"
COMPOSE_FILE="${DEPLOY_COMPOSE_FILE:-docker-compose.server.yml}"
DEPLOY_PRUNE="${DEPLOY_PRUNE:-0}"
DEPLOY_PRUNE_AGE="${DEPLOY_PRUNE_AGE:-168h}"

# log 写入带时间的部署日志，方便定位每个部署阶段的耗时和失败原因。
log() {
  printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >> "$LOG_FILE"
}

# cleanup_lock 释放部署锁，避免脚本异常退出后影响下一次定时任务。
cleanup_lock() {
  rmdir "$LOCK_DIR" 2>/dev/null || true
}

# compose_cmd 自动选择服务器当前可用的 Docker Compose 命令。
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
  log "检测到未提交改动，为避免覆盖本地文件，本次部署终止"
  git status --short --untracked-files=no >> "$LOG_FILE" 2>&1
  exit 1
fi

# 先切到部署分支再读取本地提交，避免服务器停留在其他分支时错误比较提交记录。
git checkout "$BRANCH" >> "$LOG_FILE" 2>&1
git fetch "$REMOTE" "$BRANCH" >> "$LOG_FILE" 2>&1

LOCAL_COMMIT="$(git rev-parse HEAD)"
REMOTE_COMMIT="$(git rev-parse "$REMOTE/$BRANCH")"

if [ "$LOCAL_COMMIT" = "$REMOTE_COMMIT" ]; then
  log "代码无更新，本次跳过"
  exit 0
fi

if ! git merge-base --is-ancestor "$LOCAL_COMMIT" "$REMOTE_COMMIT"; then
  log "远端分支无法快进合并 local=$LOCAL_COMMIT remote=$REMOTE_COMMIT，部署终止"
  exit 1
fi

# 保存更新范围，用于只构建或重启真正发生变化的云端服务。
CHANGED_FILES="$(git diff --name-only "$LOCAL_COMMIT" "$REMOTE_COMMIT")"
PROJECT_PREFIX="$(git rev-parse --show-prefix)"
BACKEND_PREFIX="${PROJECT_PREFIX}cloud/backend/"
FRONTEND_PREFIX="${PROJECT_PREFIX}cloud/frontend-next/"
COMPOSE_PATH="${PROJECT_PREFIX}${COMPOSE_FILE#./}"

BACKEND_CHANGED=0
BACKEND_BUILD_REQUIRED=0
FRONTEND_CHANGED=0
FULL_DEPLOY_REQUIRED=0

# classify_changed_file 将单个变更文件归类到后端重启、后端重建、前端重建或完整部署。
classify_changed_file() {
  changed_file="$1"
  case "$changed_file" in
    "$COMPOSE_PATH")
      FULL_DEPLOY_REQUIRED=1
      ;;
    "${BACKEND_PREFIX}Dockerfile"|"${BACKEND_PREFIX}.dockerignore"|"${BACKEND_PREFIX}go.mod"|"${BACKEND_PREFIX}go.sum")
      BACKEND_CHANGED=1
      BACKEND_BUILD_REQUIRED=1
      ;;
    "${BACKEND_PREFIX}"*)
      BACKEND_CHANGED=1
      ;;
    "${FRONTEND_PREFIX}"*)
      FRONTEND_CHANGED=1
      ;;
  esac
}

while IFS= read -r changed_file; do
  [ -n "$changed_file" ] || continue
  classify_changed_file "$changed_file"
done <<EOF
$CHANGED_FILES
EOF

log "发现更新 local=$LOCAL_COMMIT remote=$REMOTE_COMMIT backend=$BACKEND_CHANGED backend_build=$BACKEND_BUILD_REQUIRED frontend=$FRONTEND_CHANGED full=$FULL_DEPLOY_REQUIRED"

# fetch 已经取得远端提交，此处直接快进合并，避免 git pull 再次访问远端。
git merge --ff-only "$REMOTE/$BRANCH" >> "$LOG_FILE" 2>&1

COMPOSE="$(compose_cmd)" || {
  log "未找到 docker compose 或 docker-compose，部署终止"
  exit 1
}

# 任一核心容器不存在时执行完整部署，保证首次安装或容器被删除后能够自动恢复。
BACKEND_CONTAINER="$($COMPOSE -f "$COMPOSE_FILE" ps -q backend 2>/dev/null || true)"
FRONTEND_CONTAINER="$($COMPOSE -f "$COMPOSE_FILE" ps -q frontend 2>/dev/null || true)"
if [ -z "$BACKEND_CONTAINER" ] || [ -z "$FRONTEND_CONTAINER" ]; then
  FULL_DEPLOY_REQUIRED=1
  log "检测到核心容器不存在，改为完整部署"
fi

if [ "$FULL_DEPLOY_REQUIRED" = "1" ]; then
  log "开始完整 Docker 构建，启用分层缓存"
  $COMPOSE -f "$COMPOSE_FILE" build backend frontend >> "$LOG_FILE" 2>&1
  log "开始更新全部 Docker 服务"
  $COMPOSE -f "$COMPOSE_FILE" up -d --remove-orphans >> "$LOG_FILE" 2>&1
else
  BUILD_SERVICES=""
  if [ "$BACKEND_BUILD_REQUIRED" = "1" ]; then
    BUILD_SERVICES="$BUILD_SERVICES backend"
  fi
  if [ "$FRONTEND_CHANGED" = "1" ]; then
    BUILD_SERVICES="$BUILD_SERVICES frontend"
  fi

  if [ -n "$BUILD_SERVICES" ]; then
    log "开始构建变更服务 services=$BUILD_SERVICES，启用分层缓存"
    $COMPOSE -f "$COMPOSE_FILE" build $BUILD_SERVICES >> "$LOG_FILE" 2>&1
    log "开始更新已构建服务 services=$BUILD_SERVICES"
    $COMPOSE -f "$COMPOSE_FILE" up -d --no-deps $BUILD_SERVICES >> "$LOG_FILE" 2>&1
  fi

  if [ "$BACKEND_CHANGED" = "1" ] && [ "$BACKEND_BUILD_REQUIRED" = "0" ]; then
    # 生产后端挂载宿主机源码，普通 Go 源码变化只需重启进程，无需重新构建镜像。
    log "后端源码发生变化，仅重启 backend 服务"
    $COMPOSE -f "$COMPOSE_FILE" restart backend >> "$LOG_FILE" 2>&1
  fi

  if [ "$BACKEND_CHANGED" = "0" ] && [ "$FRONTEND_CHANGED" = "0" ]; then
    log "本次更新不涉及云端前后端，跳过 Docker 构建与重启"
  fi
fi

# 默认保留镜像与 BuildKit 缓存；仅显式设置 DEPLOY_PRUNE=1 时清理超过指定时长的缓存。
if [ "$DEPLOY_PRUNE" = "1" ]; then
  log "开始清理超过 $DEPLOY_PRUNE_AGE 的 Docker 缓存"
  docker image prune -f --filter "until=$DEPLOY_PRUNE_AGE" >> "$LOG_FILE" 2>&1 || true
  docker builder prune -f --filter "until=$DEPLOY_PRUNE_AGE" >> "$LOG_FILE" 2>&1 || true
  docker container prune -f --filter "until=$DEPLOY_PRUNE_AGE" >> "$LOG_FILE" 2>&1 || true
fi

log "部署完成 commit=$(git rev-parse --short HEAD)"
