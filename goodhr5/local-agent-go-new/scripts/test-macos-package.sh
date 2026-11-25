#!/bin/bash
# 文件作用说明：用真实 pkgbuild 构建合成安装包，验证固定安装位置及拒绝旧式可重定位包，无需管理员权限。
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
project_dir=$(dirname "$script_dir")
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/goodhr-pkg-test.XXXXXX")
trap 'rm -rf -- "$test_dir"' EXIT
app_dir="$test_dir/payload/Applications/GoodHR Local Agent.app"
mkdir -p "$app_dir/Contents/MacOS"
cp "$project_dir/packaging/macos/Info.plist" "$app_dir/Contents/Info.plist"
printf '#!/bin/sh\nexit 0\n' > "$app_dir/Contents/MacOS/goodhr-launcher"
chmod 755 "$app_dir/Contents/MacOS/goodhr-launcher"

/usr/bin/pkgbuild --root "$test_dir/payload" --install-location / \
  --component-plist "$project_dir/packaging/macos/components.plist" \
  --identifier cn.58it.goodhr.localagent --version 0 "$test_dir/fixed.pkg" >/dev/null
bash "$script_dir/verify-macos-package.sh" "$test_dir/fixed.pkg"

cp "$project_dir/packaging/macos/components.plist" "$test_dir/relocatable.plist"
/usr/libexec/PlistBuddy -c 'Set :0:BundleIsRelocatable true' "$test_dir/relocatable.plist"
/usr/bin/pkgbuild --root "$test_dir/payload" --install-location / \
  --component-plist "$test_dir/relocatable.plist" \
  --identifier cn.58it.goodhr.localagent --version 0 "$test_dir/relocatable.pkg" >/dev/null
if bash "$script_dir/verify-macos-package.sh" "$test_dir/relocatable.pkg" > "$test_dir/rejected.log" 2>&1; then
  echo '回归未通过：错误的可重定位包没有被拦住。' >&2
  exit 1
fi
grep -q '禁止应用重定位' "$test_dir/rejected.log"
/bin/sh -n "$project_dir/packaging/macos/postinstall"
echo 'macOS 安装包固定位置回归通过；全程未执行安装。'
