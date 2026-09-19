#!/usr/bin/env bash
# 本文件一键构建并部署测试环境，公开域名由环境文件固定。
set -euo pipefail
exec bash "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/deploy-environment.sh" dev "$@"
