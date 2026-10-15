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
if [[ -n "$action" && "$action" != "--build-only" && "$action" != "--activate-only" ]]; then
  echo "只支持 --build-only 或 --activate-only" >&2; exit 2
fi
[[ "$mode" == build || "$action" != "--activate-only" ]] || { echo "隔离激活仅用于正式环境" >&2; exit 2; }
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
# 公开环境文件不包含密码；后加载可防止外部变量意外混用环境域名和端口。
set -a
source "environments/$environment.env"
set +a
export GOODHR_BACKEND_ENV="${GOODHR_BACKEND_ENV:-$root/cloud/backend/.env}"
[[ -s "$GOODHR_BACKEND_ENV" ]] || { echo "缺少后端配置文件：$GOODHR_BACKEND_ENV" >&2; exit 1; }
umask 077
if [[ "$mode" == build ]]; then
  source "$root/scripts/production-release.sh"
  production_prepare
fi
# 备份库名必须和服务实际连接的数据库一致，避免备份错库后仍执行迁移。
if [[ "$action" != "--build-only" ]]; then
  : "${GOODHR_DATABASE:?请设置 GOODHR_DATABASE 为本环境的数据库名}"
  python3 - "$GOODHR_BACKEND_ENV" "$GOODHR_DATABASE" <<'PY'
import sys, re, shlex
from pathlib import Path
from urllib.parse import urlsplit, unquote
values = {}
for line in Path(sys.argv[1]).read_text(encoding='utf-8-sig').splitlines():
    if '=' in line and not line.lstrip().startswith('#'):
        key, value = line.split('=', 1)
        values[key.strip()] = value.strip().strip('"').strip("'")
dsn = values.get('GOODHR_PG_DSN', '')
if dsn.startswith(('postgres://', 'postgresql://')):
    database = unquote(urlsplit(dsn).path.lstrip('/'))
else:
    fields = dict(item.split('=', 1) for item in shlex.split(dsn) if '=' in item)
    database = fields.get('dbname', '')
if not re.fullmatch(r'[A-Za-z0-9_]+', sys.argv[2]) or database != sys.argv[2]:
    raise SystemExit('备份库名与后端配置不一致，部署已停止；请检查 GOODHR_DATABASE 和 GOODHR_PG_DSN')
PY
fi
compose=(docker compose --env-file "environments/$environment.env" -f "docker-compose.$mode.yml")
"${compose[@]}" config --quiet
if [[ "$action" != "--activate-only" ]]; then
  if [[ "$mode" == build ]]; then
    production_require_new_images
  fi
  "${compose[@]}" build
fi
if [[ "$mode" == build ]]; then
  production_check_images
fi
if [[ "$action" == "--build-only" ]]; then
  echo "候选镜像已构建，尚未切换线上服务。"
  exit 0
fi
if [[ "$mode" == build ]]; then
  production_require_evidence
fi
# 先备份目标数据库，失败时禁止启动新版本及执行自动迁移。数据库名必须由部署者明确给出。
: "${GOODHR_DATABASE:?请设置 GOODHR_DATABASE 为本环境的数据库名}"
[[ "$GOODHR_DATABASE" =~ ^[A-Za-z0-9_]+$ ]] || { echo "数据库名格式不正确" >&2; exit 1; }
backup="$root/backups/$environment/$(date +%Y%m%d-%H%M%S)"
if [[ "$mode" == build ]]; then
  backup="$production_release_dir"
fi
umask 077
mkdir -p "$backup"
[[ ! -e "$backup/database.dump" ]] || { echo "本次备份已存在，请核对发布状态，避免覆盖" >&2; exit 1; }
cp "$GOODHR_BACKEND_ENV" "$backup/backend.env"
"${compose[@]}" images --format json > "$backup/images.json"
docker exec "${GOODHR_POSTGRES_CONTAINER:-postgres}" sh -c 'exec pg_dump -U "$POSTGRES_USER" -Fc "$1"' sh "$GOODHR_DATABASE" > "$backup/database.dump"
[[ -s "$backup/database.dump" ]] || { echo "数据库备份为空，部署已停止" >&2; exit 1; }
docker exec -i "${GOODHR_POSTGRES_CONTAINER:-postgres}" pg_restore --list < "$backup/database.dump" > "$backup/database-toc.txt"
if [[ "$mode" == build ]]; then
  "${compose[@]}" up -d --no-build --pull never --no-deps backend frontend
else
  "${compose[@]}" up -d --no-build
fi
backend_port="${GOODHR_CLOUD_ADDR##*:}"
# 最多等待两分钟；失败保留日志及备份，交给维护者按文档恢复。
for attempt in $(seq 1 24); do
  if curl --fail --silent --max-time 3 "http://127.0.0.1:$backend_port/health" >/dev/null &&
     curl --fail --silent --max-time 5 "http://127.0.0.1:$GOODHR_FRONTEND_PORT/" >/dev/null; then
    if [[ "$mode" == build ]]; then
      if production_finalize; then
        echo "发布材料及旧版本收尾完成。"
      else
        cleanup_status=$?
        echo "线上已经激活且健康检查通过；发布收尾未完成（cleanup_pending），请按保留计划继续处理。" >&2
        exit "$cleanup_status"
      fi
    fi
    echo "部署成功：$GOODHR_PUBLIC_BASE_URL"
    echo "备份目录：$backup"
    exit 0
  fi
  sleep 5
done
echo "容器健康检查未通过，请检查 docker compose 日志。备份：$backup" >&2
exit 1
