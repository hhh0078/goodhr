#!/usr/bin/env bash
# 本文件执行环境校验、镜像构建、数据库备份、容器更新及健康检查，不修改宝塔站点。
set -euo pipefail
mode="${1:-}"
case "$mode" in
  dev) environment=test ;;
  build) environment=production ;;
  *) echo "用法：bash scripts/deploy-environment.sh dev|build [--build-only]" >&2; exit 2 ;;
esac
action="${2:-}"
if [[ -n "$action" && "$action" != "--build-only" ]]; then
  echo "只支持可选参数 --build-only" >&2; exit 2
fi
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
# 公开环境文件不包含密码；后加载可防止外部变量意外混用环境域名和端口。
set -a
source "environments/$environment.env"
set +a
export GOODHR_BACKEND_ENV="${GOODHR_BACKEND_ENV:-$root/cloud/backend/.env}"
[[ -s "$GOODHR_BACKEND_ENV" ]] || { echo "缺少后端配置文件：$GOODHR_BACKEND_ENV" >&2; exit 1; }
compose=(docker compose --env-file "environments/$environment.env" -f "docker-compose.$mode.yml")
"${compose[@]}" config --quiet
"${compose[@]}" build
[[ "$action" == "--build-only" ]] && exit 0
# 先备份目标数据库，失败时禁止启动新版本及执行自动迁移。数据库名必须由部署者明确给出。
: "${GOODHR_DATABASE:?请设置 GOODHR_DATABASE 为本环境的数据库名}"
[[ "$GOODHR_DATABASE" =~ ^[A-Za-z0-9_]+$ ]] || { echo "数据库名格式不正确" >&2; exit 1; }
backup="$root/backups/$environment/$(date +%Y%m%d-%H%M%S)"
umask 077
mkdir -p "$backup"
cp "$GOODHR_BACKEND_ENV" "$backup/backend.env"
"${compose[@]}" images --format json > "$backup/images.json"
docker exec "${GOODHR_POSTGRES_CONTAINER:-postgres}" sh -c 'exec pg_dump -U "$POSTGRES_USER" -Fc "$1"' sh "$GOODHR_DATABASE" > "$backup/database.dump"
[[ -s "$backup/database.dump" ]] || { echo "数据库备份为空，部署已停止" >&2; exit 1; }
"${compose[@]}" up -d --no-build
backend_port="${GOODHR_CLOUD_ADDR##*:}"
# 最多等待两分钟；失败保留日志及备份，交给维护者按文档恢复。
for attempt in $(seq 1 24); do
  if curl --fail --silent --max-time 3 "http://127.0.0.1:$backend_port/health" >/dev/null &&
     curl --fail --silent --max-time 5 "http://127.0.0.1:$GOODHR_FRONTEND_PORT/" >/dev/null; then
    echo "部署成功：$GOODHR_PUBLIC_BASE_URL"
    echo "备份目录：$backup"
    exit 0
  fi
  sleep 5
done
echo "容器健康检查未通过，请检查 docker compose 日志。备份：$backup" >&2
exit 1
