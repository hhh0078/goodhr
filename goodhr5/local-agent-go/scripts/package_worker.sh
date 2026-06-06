#!/usr/bin/env bash
# 文件作用：打包 GoodHR Node Browser Worker，并输出 sha256，供上传 OSS manifest 使用。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKER_DIR="$ROOT_DIR/worker-node"
DIST_DIR="$ROOT_DIR/dist/runtime"
VERSION="$(node -p "require('$WORKER_DIR/package.json').version" 2>/dev/null || echo "0.1.0")"
PLATFORM="$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)"
PACKAGE_NAME="goodhr-browser-worker-${PLATFORM}-${VERSION}.zip"
PACKAGE_PATH="$DIST_DIR/$PACKAGE_NAME"

# log 输出脚本状态。
# 参数为要显示的中文消息。
log() {
  printf '[GoodHR] %s\n' "$*"
}

if [ ! -d "$WORKER_DIR/node_modules" ]; then
  log "未找到 worker-node/node_modules，无法打包可直接运行的 Worker。"
  log "请先确认 npm 使用国内镜像后，在 worker-node 目录执行 npm install --omit=dev。"
  exit 1
fi

mkdir -p "$DIST_DIR"
rm -f "$PACKAGE_PATH"

log "开始打包 Node Browser Worker：$PACKAGE_NAME"
(
  cd "$WORKER_DIR"
  zip -qr "$PACKAGE_PATH" package.json src node_modules
)

if command -v shasum >/dev/null 2>&1; then
  SHA256="$(shasum -a 256 "$PACKAGE_PATH" | awk '{print $1}')"
else
  SHA256="$(sha256sum "$PACKAGE_PATH" | awk '{print $1}')"
fi

log "打包完成：$PACKAGE_PATH"
log "sha256：$SHA256"
