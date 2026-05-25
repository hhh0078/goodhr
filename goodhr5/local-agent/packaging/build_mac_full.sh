#!/usr/bin/env bash
# 本脚本用于在 macOS 上一键打包 GoodHR Local Agent.app。

set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> 当前目录：$(pwd)"

if [ ! -d ".venv" ]; then
  echo "==> 创建 Python 虚拟环境 .venv"
  python3 -m venv .venv
fi

PYTHON=".venv/bin/python"

echo "==> 配置 pip 国内镜像"
"$PYTHON" -m pip config set global.index-url https://mirrors.aliyun.com/pypi/simple >/dev/null
"$PYTHON" -m pip config set install.trusted-host mirrors.aliyun.com >/dev/null

echo "==> 升级 pip"
"$PYTHON" -m pip install -U pip

echo "==> 安装运行和打包依赖"
"$PYTHON" -m pip install -e ".[packaging]"

echo "==> 准备 macOS CloakBrowser"
"$PYTHON" packaging/prepare_vendor.py --platform mac

echo "==> 开始 PyInstaller 打包"
"$PYTHON" -m PyInstaller --clean --noconfirm packaging/GoodHRLocalAgent.spec

echo "==> 打包完成"
echo "产物位置：$(pwd)/dist/GoodHRLocalAgent.app"
