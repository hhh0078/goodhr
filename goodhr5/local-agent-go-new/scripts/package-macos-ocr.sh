#!/bin/zsh
# 文件作用说明：把 macOS Vision OCR 编译成 Apple Silicon 与 Intel 通用二进制并打成独立组件 ZIP。

set -euo pipefail

script_dir=${0:A:h}
project_dir=${script_dir:h}
version=${1:-1.0.0}
if [[ ! "${version}" =~ '^[0-9A-Za-z._-]+$' ]]; then
  print -u2 "OCR 版本号只能包含数字、字母、点、下划线和短横线"
  exit 1
fi
if [[ "$(uname -s)" != "Darwin" ]]; then
  print -u2 "Vision OCR 必须在 macOS 上编译"
  exit 1
fi

source_file="${project_dir}/packaging/mac-ocr/RapidOCR-json.swift"
release_dir="${project_dir}/release"
archive="${release_dir}/goodhr-ocr-vision-v${version}-darwin-universal.zip"
sdk=$(xcrun --sdk macosx --show-sdk-path)
swiftc=$(xcrun --find swiftc)
temp_root=${TMPDIR:-/tmp}
build_dir=$(mktemp -d "${temp_root%/}/goodhr-ocr.XXXXXX")
trap 'rm -rf "${build_dir}"' EXIT

"${swiftc}" -O -parse-as-library -target arm64-apple-macos13.0 -sdk "${sdk}" "${source_file}" -o "${build_dir}/ocr-arm64"
"${swiftc}" -O -parse-as-library -target x86_64-apple-macos13.0 -sdk "${sdk}" "${source_file}" -o "${build_dir}/ocr-x64"
mkdir -p "${build_dir}/package" "${release_dir}"
lipo -create "${build_dir}/ocr-arm64" "${build_dir}/ocr-x64" -output "${build_dir}/package/RapidOCR-json"
chmod 755 "${build_dir}/package/RapidOCR-json"
lipo "${build_dir}/package/RapidOCR-json" -verify_arch arm64 x86_64
rm -f "${archive}"
cd "${build_dir}/package"
zip -X -q "${archive}" RapidOCR-json
print "OCR 组件已生成：${archive}"
shasum -a 256 "${archive}"
