#!/bin/bash
# 文件作用说明：展开真实 macOS 安装包，确认 GoodHR 安装到 /Applications 且不会追随旧应用位置。
set -euo pipefail

package_path=${1:?请传入待检查的 macOS PKG 安装包}
verify_dir=$(mktemp -d "${TMPDIR:-/tmp}/goodhr-pkg-verify.XXXXXX")
trap 'rm -rf -- "$verify_dir"' EXIT
/usr/sbin/pkgutil --expand "$package_path" "$verify_dir/expanded"
package_info="$verify_dir/expanded/PackageInfo"
# 顶层 relocatable=false 不能禁止应用重定位，必须检查 relocate 下没有 bundle。
valid=$(/usr/bin/xmllint --xpath 'boolean(/pkg-info[@identifier="cn.58it.goodhr.localagent"][@install-location="/"]/bundle[@id="cn.58it.goodhr.localagent"][@path="./Applications/GoodHR Local Agent.app"]) and count(/pkg-info/relocate/bundle)=0' "$package_info")
if [[ "$valid" != true ]]; then
  echo 'GoodHR 安装包位置检查未通过：必须固定安装到 /Applications，且禁止应用重定位。' >&2
  exit 1
fi
echo 'GoodHR 安装包位置检查通过：/Applications，禁止应用重定位。'
