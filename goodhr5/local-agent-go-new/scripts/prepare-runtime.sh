#!/bin/zsh
# 文件作用说明：通过国内 npm 镜像安装 Worker 依赖，并用个人 Key 下载官方 Stable Chromium。

set -euo pipefail

script_dir=${0:A:h}
project_dir=${script_dir:h}
npm_registry=${GOODHR_NPM_REGISTRY:-https://registry.npmmirror.com}

if [[ -z "${CLOAKBROWSER_LICENSE_KEY:-}" ]]; then
	print -u2 "请先设置 CLOAKBROWSER_LICENSE_KEY，再准备最新版浏览器"
	exit 1
fi

cd "${project_dir}/worker"
npm ci --registry="${npm_registry}"
npm run build
CLOAKBROWSER_RELEASE_CHANNEL=stable \
	CLOAKBROWSER_AUTO_UPDATE=true \
	npm exec -- cloakbrowser install
