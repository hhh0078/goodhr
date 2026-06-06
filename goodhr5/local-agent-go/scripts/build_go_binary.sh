#!/usr/bin/env bash
# 文件作用：编译 GoodHR Go 本地程序可执行文件，供发布包或安装器使用。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist/bin"
TARGET_OS="${TARGET_OS:-$(go env GOOS)}"
TARGET_ARCH="${TARGET_ARCH:-$(go env GOARCH)}"
EXT=""

# log 输出脚本状态。
# 参数为要显示的中文消息。
log() {
  printf '[GoodHR] %s\n' "$*"
}

if [ "$TARGET_OS" = "windows" ]; then
  EXT=".exe"
fi

mkdir -p "$DIST_DIR"
OUTPUT="$DIST_DIR/goodhr-local-agent-${TARGET_OS}-${TARGET_ARCH}${EXT}"

log "开始编译 Go 本地程序：GOOS=$TARGET_OS GOARCH=$TARGET_ARCH"
(
  cd "$ROOT_DIR"
  CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build \
    -trimpath \
    -ldflags="-s -w" \
    -o "$OUTPUT" \
    ./cmd/goodhr-local-agent
)

log "编译完成：$OUTPUT"
