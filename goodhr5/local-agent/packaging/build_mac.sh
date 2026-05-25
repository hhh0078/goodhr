#!/usr/bin/env bash
# 本脚本用于在 macOS 上打包 GoodHR Local Agent.app。

set -euo pipefail

cd "$(dirname "$0")/.."

python3 packaging/prepare_vendor.py --platform mac
python3 -m PyInstaller --clean --noconfirm --distpath dist --workpath build packaging/GoodHRLocalAgent.spec

echo "打包完成：dist/GoodHRLocalAgent.app"
