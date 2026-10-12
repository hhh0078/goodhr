#!/bin/zsh
# 文件作用说明：在 macOS 上生成同时支持 Apple 和 Intel 芯片的 GoodHR 应用、ZIP 和未签名 PKG 安装器。

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
if [[ ! "${version}" =~ '^[0-9]+([.][0-9]+)*$' ]]; then
  print -u2 "macOS 安装包版本号只能包含数字和点"
  exit 1
fi
environment_file="${project_dir}/../environments/${build_environment}.env"
[[ -f "${environment_file}" ]] || { print -u2 "缺少环境配置：${environment_file}"; exit 1; }
public_base_url=$(sed -n 's/^GOODHR_PUBLIC_BASE_URL=//p' "${environment_file}" | tr -d '\r')
[[ "${public_base_url}" =~ '^https://[a-z0-9.-]+$' ]] || { print -u2 "环境主域名格式不正确"; exit 1; }
environment_suffix=""
[[ "${build_environment}" != "test" ]] || environment_suffix="-test"
npm_registry=${GOODHR_NPM_REGISTRY:-https://registry.npmmirror.com}
go_proxy=${GOPROXY:-https://goproxy.cn,direct}

[[ "$(uname -s)" == "Darwin" ]] || { print -u2 "macOS 安装包必须在 Mac 上构建"; exit 1; }
for command_name in node npm go lipo pkgbuild ditto iconutil sips; do
  command -v "${command_name}" >/dev/null || { print -u2 "缺少打包命令：${command_name}"; exit 1; }
done

release_root="${project_dir}/release"
mkdir -p "${release_root}"
stage_dir=$(mktemp -d "${release_root}/.macos-package.XXXXXX")
trap 'rm -rf -- "${stage_dir}"' EXIT
app_name="GoodHR Local Agent.app"
app_path="${stage_dir}/payload/Applications/${app_name}"
macos_dir="${app_path}/Contents/MacOS"
worker_dir="${macos_dir}/worker"
mkdir -p "${worker_dir}" "${app_path}/Contents/Resources"

cd "${project_dir}/worker"
npm ci --ignore-scripts --registry="${npm_registry}"
npm run build
cp -R dist "${worker_dir}/dist"
cp package.json package-lock.json "${worker_dir}/"

cd "${worker_dir}"
npm ci --omit=dev --ignore-scripts --os=darwin --cpu=arm64 --registry="${npm_registry}"
[[ -f node_modules/impit-darwin-arm64/package.json ]] || { print -u2 "Worker 缺少 Apple 芯片原生依赖"; exit 1; }

# Intel 原生依赖必须从同一锁文件单独安装，不能使用打包机 arm64 的 node_modules 代替。
intel_worker="${stage_dir}/worker-intel"
mkdir -p "${intel_worker}"
cp package.json package-lock.json "${intel_worker}/"
cd "${intel_worker}"
npm ci --omit=dev --ignore-scripts --os=darwin --cpu=x64 --registry="${npm_registry}"
[[ -f node_modules/impit-darwin-x64/package.json ]] || { print -u2 "Worker 缺少 Intel 原生依赖"; exit 1; }
cp -R node_modules/impit-darwin-x64 "${worker_dir}/node_modules/"
node --check "${worker_dir}/dist/main.js"

cd "${project_dir}"
build_flags="-s -w -X goodhr5/local-agent-go-new/internal/version.Value=${version} -X goodhr5/local-agent-go-new/internal/config.DefaultCloudURL=${public_base_url} -X goodhr5/local-agent-go-new/internal/config.DefaultConsoleURL=${public_base_url}"
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 GOPROXY="${go_proxy}" go build -trimpath -ldflags="${build_flags}" -o "${stage_dir}/agent-arm64" ./cmd/goodhr-local-agent
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 GOPROXY="${go_proxy}" go build -trimpath -ldflags="${build_flags}" -o "${stage_dir}/agent-x64" ./cmd/goodhr-local-agent
lipo -create -output "${macos_dir}/goodhr-local-agent" "${stage_dir}/agent-arm64" "${stage_dir}/agent-x64"
lipo "${macos_dir}/goodhr-local-agent" -verify_arch arm64 x86_64
chmod 755 "${macos_dir}/goodhr-local-agent"

cp packaging/macos/Info.plist "${app_path}/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${version}" "${app_path}/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion ${version}" "${app_path}/Contents/Info.plist"
cp README.md "${app_path}/Contents/Resources/README.md"
cp -R licenses "${macos_dir}/licenses"
printf '{"version":"%s","environment":"%s","public_base_url":"%s"}\n' "${version}" "${build_environment}" "${public_base_url}" > "${macos_dir}/build-info.json"

iconset="${stage_dir}/GoodHR.iconset"
mkdir -p "${iconset}"
for size in 16 32 128 256 512; do
  sips -s format png -z "${size}" "${size}" assets/goodhr-logo-transparent-512.png --out "${iconset}/icon_${size}x${size}.png" >/dev/null
  doubled=$((size * 2))
  sips -s format png -z "${doubled}" "${doubled}" assets/goodhr-logo-transparent-512.png --out "${iconset}/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "${iconset}" -o "${app_path}/Contents/Resources/GoodHR.icns"
plutil -lint "${app_path}/Contents/Info.plist"

package_name="GoodHR-Local-Agent-Setup-v${version}${environment_suffix}-mac-universal.pkg"
archive_name="goodhr-local-agent-v${version}${environment_suffix}-darwin-universal.zip"
pkgbuild --root "${stage_dir}/payload" --install-location / \
  --identifier cn.58it.goodhr.localagent --version "${version}" \
  --scripts "${project_dir}/packaging/macos" "${stage_dir}/${package_name}"
ditto -c -k --sequesterRsrc --keepParent "${app_path}" "${stage_dir}/${archive_name}"

rm -rf -- "${release_root}/${app_name}"
ditto "${app_path}" "${release_root}/${app_name}"
mv -f "${stage_dir}/${package_name}" "${release_root}/${package_name}"
mv -f "${stage_dir}/${archive_name}" "${release_root}/${archive_name}"
print "macOS 通用安装包：${release_root}/${package_name}"
print "macOS 应用：${release_root}/${app_name}"
print "SHA256：$(shasum -a 256 "${release_root}/${package_name}" | awk '{print $1}')"
