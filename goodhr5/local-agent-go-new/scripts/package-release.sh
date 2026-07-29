#!/bin/zsh
# 文件作用说明：生成包含 Go 主程序、Worker 编译产物和 Worker 生产依赖的 macOS 正式发布包。

set -euo pipefail

script_dir=${0:A:h}
project_dir=${script_dir:h}
version=${1:-6}
npm_registry=${GOODHR_NPM_REGISTRY:-https://registry.npmmirror.com}
go_proxy=${GOPROXY:-https://goproxy.cn,direct}

if [[ ! "${version}" =~ '^[0-9A-Za-z._-]+$' ]]; then
  print -u2 "版本号只能包含数字、字母、点、下划线和短横线"
  exit 1
fi

case "$(uname -m)" in
  arm64) release_arch="arm64" ;;
  x86_64) release_arch="x64" ;;
  *)
    print -u2 "当前 macOS 架构暂不支持打包：$(uname -m)"
    exit 1
    ;;
esac

release_root="${project_dir}/release"
package_name="goodhr-local-agent-v${version}-darwin-${release_arch}"
package_dir="${release_root}/${package_name}"
archive_path="${release_root}/${package_name}.zip"

if [[ "${package_dir}" != "${release_root}/goodhr-local-agent-"* ]]; then
  print -u2 "发布目录校验失败，已经停止打包"
  exit 1
fi

rm -rf "${package_dir}"
rm -f "${archive_path}"
mkdir -p "${package_dir}/worker"

cd "${project_dir}/worker"
npm ci --registry="${npm_registry}"
npm run build
cp -R dist "${package_dir}/worker/dist"
cp package.json package-lock.json "${package_dir}/worker/"

cd "${package_dir}/worker"
npm ci --omit=dev --registry="${npm_registry}"

cd "${project_dir}"
GOPROXY="${go_proxy}" go build \
  -trimpath \
  -ldflags="-s -w -X goodhr5/local-agent-go-new/internal/version.Value=${version} -X goodhr5/local-agent-go-new/internal/config.DefaultCloudURL=https://goodhr5.58it.cn -X goodhr5/local-agent-go-new/internal/config.DefaultConsoleURL=https://goodhr5.58it.cn" \
  -o "${package_dir}/goodhr-local-agent" \
  ./cmd/goodhr-local-agent
cp README.md "${package_dir}/README.md"

cd "${release_root}"
ditto -c -k --sequesterRsrc --keepParent "${package_name}" "${archive_path}"
print "发布包已生成：${archive_path}"
