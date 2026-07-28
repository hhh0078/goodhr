#!/bin/zsh
# 文件作用说明：安装锁定的 Worker 依赖并下载 CloakBrowser 增强浏览器二进制。

set -euo pipefail

script_dir=${0:A:h}
project_dir=${script_dir:h}

cd "${project_dir}/worker"
npm ci
npm exec -- cloakbrowser install
npm run build
