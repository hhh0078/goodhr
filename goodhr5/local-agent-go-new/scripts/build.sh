#!/bin/zsh
# 文件作用说明：在 macOS 上编译严格 TypeScript Worker 和 Go 本地程序。

set -euo pipefail

script_dir=${0:A:h}
project_dir=${script_dir:h}

cd "${project_dir}/worker"
npm ci
npm run build

cd "${project_dir}"
mkdir -p bin
go build -o bin/goodhr-local-agent ./cmd/goodhr-local-agent
