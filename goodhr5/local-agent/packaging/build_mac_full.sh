#!/usr/bin/env bash
# 本脚本用于在 macOS 上一键打包 GoodHR Local Agent.app。

set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> 当前目录：$(pwd)"

find_python() {
  for candidate in python3.12 python3.11 python3.10; do
    if command -v "$candidate" >/dev/null 2>&1; then
      echo "$candidate"
      return 0
    fi
  done
  return 1
}

SYSTEM_PYTHON="$(find_python || true)"
if [ -z "$SYSTEM_PYTHON" ]; then
  echo "错误：未找到 Python 3.10+。请先安装 python3.12、python3.11 或 python3.10。"
  echo "可选方式：brew install python@3.12"
  exit 1
fi

echo "==> 使用 Python：$("$SYSTEM_PYTHON" --version)"

if [ -x ".venv/bin/python" ]; then
  VENV_VERSION="$(".venv/bin/python" -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')"
  if [ "$(printf '%s\n' "3.10" "$VENV_VERSION" | sort -V | head -n1)" != "3.10" ]; then
    echo "==> 当前 .venv Python 版本为 $VENV_VERSION，低于 3.10，删除后重建"
    rm -rf .venv
  fi
fi

if [ ! -d ".venv" ]; then
  echo "==> 创建 Python 虚拟环境 .venv"
  "$SYSTEM_PYTHON" -m venv .venv
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
