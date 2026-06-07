#!/usr/bin/env bash
# 本脚本用于构建 GoodHR 控制台 Wails 桌面壳。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SHELL_DIR="$ROOT_DIR/console-shell"
TARGET_OS="${TARGET_OS:-$(go env GOOS)}"
TARGET_ARCH="${TARGET_ARCH:-$(go env GOARCH)}"
OUTPUT_NAME="goodhr-console"
if [ "$TARGET_OS" = "windows" ]; then
  OUTPUT_NAME="goodhr-console.exe"
fi

mkdir -p "$SHELL_DIR/bin"
cd "$SHELL_DIR"

echo "构建 GoodHR 控制台壳：$TARGET_OS/$TARGET_ARCH"
GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build -o "$SHELL_DIR/bin/$OUTPUT_NAME" .
cp "$SHELL_DIR/bin/$OUTPUT_NAME" "$SHELL_DIR/$OUTPUT_NAME"
echo "输出文件：$SHELL_DIR/bin/$OUTPUT_NAME"
