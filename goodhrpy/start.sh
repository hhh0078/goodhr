#!/bin/bash
# GoodHR 自动化工具启动脚本

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# 激活虚拟环境
source .venv/bin/activate

# 从 .env 文件加载环境变量
if [ -f .env ]; then
    set -a
    source .env
    set +a
fi

# 自动设置 CloakBrowser 二进制路径（如果未配置）
if [ -z "$CLOAKBROWSER_BINARY_PATH" ]; then
    LOCAL_BINARY="./data/browser/Chromium.app/Contents/MacOS/Chromium"
    if [ -f "$LOCAL_BINARY" ]; then
        export CLOAKBROWSER_BINARY_PATH="$LOCAL_BINARY"
    fi
fi

# 启动服务
python main.py "$@"
