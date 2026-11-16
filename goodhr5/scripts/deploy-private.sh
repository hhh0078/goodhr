#!/bin/zsh
# 文件作用说明：在 macOS 或 Linux 上校验私有配置并构建启动内网系统，首次启动自动准备组件。
set -euo pipefail
project_dir="${0:A:h:h}"
environment_file="${GOODHR_ENV_FILE:-${project_dir}/environments/private.env}"
[[ -f "${environment_file}" ]] || { print -u2 "请先复制 environments/private.env.example 为 private.env 并填写地址和密码"; exit 1; }
cd "${project_dir}"
export GOODHR_PRIVATE_BACKEND_DOCKERFILE_SHA256=$(shasum -a 256 deployment/private/Dockerfile | cut -d ' ' -f 1)
export GOODHR_PRIVATE_FRONTEND_DOCKERFILE_SHA256=$(shasum -a 256 cloud/frontend-next/Dockerfile | cut -d ' ' -f 1)
docker compose --env-file "${environment_file}" -f docker-compose.private.yml config --quiet
docker compose --env-file "${environment_file}" -f docker-compose.private.yml build release-guard
docker compose --env-file "${environment_file}" -f docker-compose.private.yml run --rm --no-deps release-guard dockerfiles --backend deployment/private/Dockerfile
docker compose --env-file "${environment_file}" -f docker-compose.private.yml run --rm extension-builder
docker compose --env-file "${environment_file}" -f docker-compose.private.yml build backend frontend
docker compose --env-file "${environment_file}" -f docker-compose.private.yml run --rm --no-deps release-guard image --kind backend --image goodhr-private-backend:local --dockerfile deployment/private/Dockerfile
docker compose --env-file "${environment_file}" -f docker-compose.private.yml run --rm --no-deps release-guard image --kind frontend --image goodhr-private-frontend:local --dockerfile cloud/frontend-next/Dockerfile
docker compose --env-file "${environment_file}" -f docker-compose.private.yml up -d --no-build --wait --wait-timeout 3000
docker compose --env-file "${environment_file}" -f docker-compose.private.yml cp gateway:/data/caddy/pki/authorities/local/root.crt private-data/GoodHR-private-root.crt
print "内网系统已启动。首次组件准备完成后，客户端可从内网下载。"
print "请将 private-data/GoodHR-private-root.crt 分发到使用电脑，并按私有部署文档信任证书后访问。"
