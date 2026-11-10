#!/bin/sh
# 文件作用：供宝塔每分钟同步main，按正式入口自动完成候选、隔离验收和激活，失败保留旧版。
set -eu

PROJECT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
LOG_FILE="$PROJECT_DIR/deploy.log"
BRANCH="${DEPLOY_BRANCH:-main}"
REMOTE="${DEPLOY_REMOTE:-origin}"
STATE_DIR=/opt/goodhr-release/auto-deploy
umask 077

# log 记录自动发布进度，不输出环境文件和数据库凭据。
log() {
  printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >> "$LOG_FILE"
}

if [ "${DEPLOY_PRUNE:-0}" != 0 ]; then
  log "已禁止全局prune，请使用scripts/release_retention.py的本项目精确计划"
  exit 1
fi
if [ -n "${DEPLOY_COMPOSE_FILE:-}" ] && [ "$DEPLOY_COMPOSE_FILE" != docker-compose.build.yml ]; then
  log "正式发布统一使用docker-compose.build.yml，不再走旧源码挂载部署"
  exit 1
fi
mkdir -p "$STATE_DIR"
# 文件锁由内核随进程退出释放，不再把历史遗留目录误判成仍在部署。
exec 5>>"$STATE_DIR/job.lock"
if ! flock -n 5; then
  log "自动发布仍在进行，本次不重复启动"
  exit 0
fi
cd "$PROJECT_DIR"
# 与正式构建共用源码锁，禁止在另一轮构建或激活中快进同一工作区。
SOURCE_LOCK="$PROJECT_DIR/.release-source.lock"
[ ! -L "$SOURCE_LOCK" ] || { log "源码锁路径异常，已停止"; exit 1; }
exec 6>>"$SOURCE_LOCK"
flock -n 6 || { log "源码正在使用，本次跳过"; exit 0; }
git rev-parse --is-inside-work-tree >/dev/null
if [ -n "$(git status --porcelain --untracked-files=normal)" ]; then
  log "源码有未提交改动，候选准备已停止"
  exit 1
fi
git checkout "$BRANCH" >> "$LOG_FILE" 2>&1
git fetch "$REMOTE" "$BRANCH" >> "$LOG_FILE" 2>&1
LOCAL_COMMIT="$(git rev-parse HEAD)"
REMOTE_COMMIT="$(git rev-parse "$REMOTE/$BRANCH")"
git merge-base --is-ancestor "$LOCAL_COMMIT" "$REMOTE_COMMIT" ||
  { log "分支不能快进，已停止"; exit 1; }
git merge --ff-only "$REMOTE/$BRANCH" >> "$LOG_FILE" 2>&1
export GOODHR_SOURCE_COMMIT="$REMOTE_COMMIT"
# 即使Git已更新，也比较实际线上版本；上轮失败不会因为HEAD相同而永远跳过。
log "核对线上版本 branch=$BRANCH source=$REMOTE_COMMIT"
flock -u 6
exec 6>&-
if python3 "$PROJECT_DIR/scripts/auto_release.py" --source-root "$PROJECT_DIR" >> "$LOG_FILE" 2>&1; then
  printf 'GoodHR检查已完成，实际发布状态见 %s/status.json\n' "$STATE_DIR"
else
  result=$?
  printf 'GoodHR本轮有未完成项（退出码%s），详情见 %s\n' "$result" "$LOG_FILE" >&2
  exit "$result"
fi
