#!/bin/zsh
# 文件作用说明：安装锁定的 Worker 依赖并下载 Camoufox 反检测浏览器二进制。

set -euo pipefail

script_dir=${0:A:h}
project_dir=${script_dir:h}
npm_registry=${GOODHR_NPM_REGISTRY:-https://registry.npmmirror.com}

cd "${project_dir}/worker"
npm ci --registry="${npm_registry}"
npx camoufox-js fetch
npm run build
