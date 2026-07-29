#!/bin/zsh
# 文件作用说明：在 macOS 上编译严格 TypeScript Worker 和 Go 本地程序。

set -euo pipefail

script_dir=${0:A:h}
project_dir=${script_dir:h}
npm_registry=${GOODHR_NPM_REGISTRY:-https://registry.npmmirror.com}
go_proxy=${GOPROXY:-https://goproxy.cn,direct}

cd "${project_dir}/worker"
npm ci --registry="${npm_registry}"
npm run build

cd "${project_dir}"
mkdir -p bin
GOPROXY="${go_proxy}" go build -o bin/goodhr-local-agent ./cmd/goodhr-local-agent
