"""打包前准备内置浏览器资源。

本脚本只在开发/打包机器上执行，用于下载当前平台需要的 CloakBrowser
压缩包并解压到 local-agent/vendor/cloakbrowser。运行后的浏览器文件不提交
到 Git，只作为 PyInstaller 打包资源使用。
"""

from __future__ import annotations

import argparse
import platform
import shutil
import sys
import urllib.request
import zipfile
from pathlib import Path


URLS = {
    "mac": "https://goodhr.58it.cn/cloakbrowser_mac.zip",
    "win": "https://goodhr.58it.cn/cloakbrowser_win.zip",
}


def project_root() -> Path:
    """
    获取 local-agent 根目录。

    Returns:
        Path: local-agent 根目录。
    """
    return Path(__file__).resolve().parents[1]


def current_target() -> str:
    """
    根据当前系统判断打包目标。

    Returns:
        str: mac 或 win。
    """
    system = platform.system().lower()
    if system == "darwin":
        return "mac"
    if system == "windows":
        return "win"
    raise RuntimeError(f"暂不支持当前系统：{platform.system()}")


def download_file(url: str, target: Path) -> None:
    """
    下载文件到指定路径。

    Args:
        url: 下载地址。
        target: 保存路径。
    """
    target.parent.mkdir(parents=True, exist_ok=True)
    with urllib.request.urlopen(url, timeout=120) as response:
        with target.open("wb") as file:
            shutil.copyfileobj(response, file)


def prepare_cloakbrowser(target_platform: str, force: bool) -> Path:
    """
    下载并解压 CloakBrowser。

    Args:
        target_platform: 目标平台，mac 或 win。
        force: 是否强制重新下载和解压。

    Returns:
        Path: 解压后的浏览器目录。
    """
    if target_platform not in URLS:
        raise RuntimeError("target_platform must be mac or win")

    root = project_root()
    vendor_dir = root / "vendor" / "cloakbrowser"
    archive_path = root / "vendor" / "downloads" / f"cloakbrowser_{target_platform}.zip"

    if force and vendor_dir.exists():
        shutil.rmtree(vendor_dir)
    if force and archive_path.exists():
        archive_path.unlink()

    if not archive_path.exists():
        print(f"下载 CloakBrowser：{URLS[target_platform]}")
        download_file(URLS[target_platform], archive_path)

    if not vendor_dir.exists():
        vendor_dir.mkdir(parents=True, exist_ok=True)
        print(f"解压 CloakBrowser 到：{vendor_dir}")
        with zipfile.ZipFile(archive_path) as zip_file:
            zip_file.extractall(vendor_dir)
    else:
        print(f"CloakBrowser 已存在：{vendor_dir}")

    return vendor_dir


def main() -> None:
    """执行命令行入口。"""
    parser = argparse.ArgumentParser(description="准备 GoodHR Local Agent 打包资源")
    parser.add_argument("--platform", choices=["mac", "win"], default=current_target())
    parser.add_argument("--force", action="store_true", help="强制重新下载和解压")
    args = parser.parse_args()

    try:
        vendor_dir = prepare_cloakbrowser(args.platform, args.force)
    except Exception as exc:
        print(f"准备失败：{exc}", file=sys.stderr)
        raise
    print(f"准备完成：{vendor_dir}")


if __name__ == "__main__":
    main()
