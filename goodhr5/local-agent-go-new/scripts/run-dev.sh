#!/bin/zsh
# 文件作用说明：在 macOS 开发环境编译 Worker 后启动 Go 本地程序。

set -euo pipefail

script_dir=${0:A:h}
project_dir=${script_dir:h}

cd "${project_dir}/worker"
npm run build

cd "${project_dir}"
go run ./cmd/goodhr-local-agent "$@"
