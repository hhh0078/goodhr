#!/usr/bin/env bash
# 本文件一键构建并部署正式环境，公开域名由环境文件固定。
set -euo pipefail
if [[ "${1:-}" == --runtime-repair ]]; then
  shift
  # 编译产物迁移也从正式入口进入，实际切换必须提供已审查计划及固定SHA。
  exec python3 "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release_cutover.py" activate "$@"
fi
exec bash "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/deploy-environment.sh" build "$@"
