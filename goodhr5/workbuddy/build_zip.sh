#!/bin/bash
# 本脚本把 connector/ 目录打包成可上传 WorkBuddy 开放平台的连接器 zip 包。
# 用法：bash build_zip.sh   （产物输出到 dist/goodhr-connector-<版本>.zip）

set -euo pipefail
cd "$(dirname "$0")"

VERSION=$(python3 -c "import json;print(json.load(open('connector/connector-meta.json'))['version'])")
OUT_DIR="dist"
OUT_NAME="goodhr-connector-${VERSION}.zip"

mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR/$OUT_NAME"

# 注意：zip 内根目录就是 connector 的内容（connector-meta.json 必须在压缩包根部）。
(cd connector && zip -r "../$OUT_DIR/$OUT_NAME" . -x "*.DS_Store")

SIZE=$(du -h "${OUT_DIR}/${OUT_NAME}" | cut -f1)
echo "打包完成：${OUT_DIR}/${OUT_NAME}（${SIZE}，上限 20MB）"
