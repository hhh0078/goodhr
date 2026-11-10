#!/bin/zsh
# 文件作用说明：从统一打包工具目录调用现有 macOS 双架构打包脚本，透传版本号、环境和退出码。
set -euo pipefail
exec /bin/zsh "${0:A:h}/../scripts/package-release.sh" "$@"
