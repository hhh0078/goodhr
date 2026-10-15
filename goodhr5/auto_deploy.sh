#!/bin/sh
# 文件作用：跟踪授权分支并经唯一正式入口准备候选；隔离验证后再显式激活，禁止源码挂载重启和全局Docker清理。
set -eu

PROJECT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
LOG_FILE="$PROJECT_DIR/deploy.log"
LOCK_DIR="$PROJECT_DIR/.deploy.lock"
BRANCH="${DEPLOY_BRANCH:-main}"
REMOTE="${DEPLOY_REMOTE:-origin}"

# log 记录候选准备结果，不输出环境文件和数据库凭据。
log() {
  printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >> "$LOG_FILE"
}

# cleanup_lock 释放当前任务独占的检查锁。
cleanup_lock() {
  rmdir "$LOCK_DIR" 2>/dev/null || true
}

if [ "${DEPLOY_PRUNE:-0}" != 0 ]; then
  log "已禁止全局prune，请使用scripts/release_retention.py的本项目精确计划"
  exit 1
fi
if [ -n "${DEPLOY_COMPOSE_FILE:-}" ] && [ "$DEPLOY_COMPOSE_FILE" != docker-compose.build.yml ]; then
  log "正式发布统一使用docker-compose.build.yml，不再走旧源码挂载部署"
  exit 1
fi
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  log "已有检查在进行，本次跳过"
  exit 0
fi
trap cleanup_lock EXIT INT TERM
cd "$PROJECT_DIR"
# 与正式构建共用源码锁，禁止在另一轮构建或激活中快进同一工作区。
SOURCE_LOCK="$PROJECT_DIR/.release-source.lock"
[ ! -L "$SOURCE_LOCK" ] || { log "源码锁路径异常，已停止"; exit 1; }
exec 11>>"$SOURCE_LOCK"
flock -n 11 || { log "源码正在使用，本次跳过"; exit 0; }
git rev-parse --is-inside-work-tree >/dev/null
if [ -n "$(git status --porcelain --untracked-files=normal)" ]; then
  log "源码有未提交改动，候选准备已停止"
  exit 1
fi
git checkout "$BRANCH" >> "$LOG_FILE" 2>&1
git fetch "$REMOTE" "$BRANCH" >> "$LOG_FILE" 2>&1
LOCAL_COMMIT="$(git rev-parse HEAD)"
REMOTE_COMMIT="$(git rev-parse "$REMOTE/$BRANCH")"
if [ "$LOCAL_COMMIT" = "$REMOTE_COMMIT" ]; then
  log "代码无更新，本次跳过"
  exit 0
fi
git merge-base --is-ancestor "$LOCAL_COMMIT" "$REMOTE_COMMIT" ||
  { log "分支不能快进，已停止"; exit 1; }
CHANGED_FILES="$(git diff --name-only "$LOCAL_COMMIT" "$REMOTE_COMMIT" -- \
  cloud/backend cloud/frontend-next scripts docker-compose.build.yml environments/production.env)"
git merge --ff-only "$REMOTE/$BRANCH" >> "$LOG_FILE" 2>&1
if [ -z "$CHANGED_FILES" ]; then
  log "本次没有云端或发布文件改动，跳过构建"
  exit 0
fi
export GOODHR_SOURCE_COMMIT="$REMOTE_COMMIT"
log "经正式入口准备候选 source=$REMOTE_COMMIT"
# 子入口自行重新持有同一把锁，并重新核对提交；避免父子进程重复持锁。
flock -u 11
exec 11>&-
bash "$PROJECT_DIR/scripts/deploy-build.sh" --build-only >> "$LOG_FILE" 2>&1
log "候选已准备，线上未切换；完成隔离及回退验证后，用同一标签执行deploy-build.sh --activate-only"
