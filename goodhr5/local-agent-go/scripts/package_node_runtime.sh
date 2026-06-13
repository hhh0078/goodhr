#!/usr/bin/env bash
# 文件作用：从本机 Node 安装目录打包 GoodHR Node runtime，并输出 sha256，供上传 OSS manifest 使用。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist/runtime"
NODE_BIN="${NODE_BIN:-$(command -v node || true)}"

# log 输出脚本状态。
# 参数为要显示的中文消息。
log() {
  printf '[GoodHR] %s\n' "$*"
}

if [ -z "$NODE_BIN" ] || [ ! -x "$NODE_BIN" ]; then
  log "未找到可执行 node，请先安装 Node runtime。"
  exit 1
fi

NODE_VERSION="$("$NODE_BIN" -p "process.versions.node")"
PLATFORM="$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)"
PACKAGE_NAME="goodhr-node-runtime-${PLATFORM}-v${NODE_VERSION}.tar.gz"
PACKAGE_PATH="$DIST_DIR/$PACKAGE_NAME"
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

mkdir -p "$DIST_DIR" "$TMP_DIR/node/bin"
cp "$NODE_BIN" "$TMP_DIR/node/bin/node"

if [ "$(uname -s)" = "Darwin" ]; then
  chmod +x "$TMP_DIR/node/bin/node"
fi

log "开始打包 Node runtime：$PACKAGE_NAME"
tar -C "$TMP_DIR" -czf "$PACKAGE_PATH" node

if command -v shasum >/dev/null 2>&1; then
  SHA256="$(shasum -a 256 "$PACKAGE_PATH" | awk '{print $1}')"
else
  SHA256="$(sha256sum "$PACKAGE_PATH" | awk '{print $1}')"
fi

log "打包完成：$PACKAGE_PATH"
log "sha256：$SHA256"
