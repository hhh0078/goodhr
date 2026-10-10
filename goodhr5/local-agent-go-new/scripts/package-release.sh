#!/bin/zsh
# 文件作用说明：生成包含 Go 主程序、Worker 编译产物和 Worker 生产依赖的 macOS 正式发布包。

set -euo pipefail

script_dir=${0:A:h}
project_dir=${script_dir:h}
version=${1:-6}
build_mode=${2:-}
case "${build_mode}" in
  build) build_environment=production ;;
  dev) build_environment=test ;;
  *) print -u2 "请在版本号后指定 dev（测试）或 build（正式）"; exit 1 ;;
esac
environment_file="${project_dir}/../environments/${build_environment}.env"
[[ -f "${environment_file}" ]] || { print -u2 "缺少环境配置：${environment_file}"; exit 1; }
public_base_url=$(sed -n 's/^GOODHR_PUBLIC_BASE_URL=//p' "${environment_file}" | tr -d '\r')
[[ "${public_base_url}" =~ '^https://[a-z0-9.-]+$' ]] || { print -u2 "环境主域名格式不正确"; exit 1; }
environment_suffix=""
[[ "${build_environment}" != "test" ]] || environment_suffix="-test"
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
package_name="goodhr-local-agent-v${version}${environment_suffix}-darwin-${release_arch}"
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
  -ldflags="-s -w -X goodhr5/local-agent-go-new/internal/version.Value=${version} -X goodhr5/local-agent-go-new/internal/config.DefaultCloudURL=${public_base_url} -X goodhr5/local-agent-go-new/internal/config.DefaultConsoleURL=${public_base_url}" \
  -o "${package_dir}/goodhr-local-agent" \
  ./cmd/goodhr-local-agent
cp README.md "${package_dir}/README.md"
cp -R licenses "${package_dir}/licenses"
printf '{"version":"%s","environment":"%s","public_base_url":"%s"}\n' "${version}" "${build_environment}" "${public_base_url}" > "${package_dir}/build-info.json"

cd "${release_root}"
ditto -c -k --sequesterRsrc --keepParent "${package_name}" "${archive_path}"
print "发布包已生成：${archive_path}"
