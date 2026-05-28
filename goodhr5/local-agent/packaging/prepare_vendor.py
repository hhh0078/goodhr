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
    "mac": [
        "https://goodhr.58it.cn/cloakbrowser_mac.zip",
    ],
    "win": [
        "https://cloakbrowser.dev/chromium-v146.0.7680.177.5/cloakbrowser-windows-x64.zip",
        "https://github.com/CloakHQ/cloakbrowser/releases/download/chromium-v146.0.7680.177.5/cloakbrowser-windows-x64.zip",
        "https://goodhr.58it.cn/cloakbrowser_win.zip",
    ],
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


def download_first_available(urls: list[str], target: Path) -> None:
    """
    从候选地址中下载第一个可用文件。

    Args:
        urls: 候选下载地址列表。
        target: 保存路径。
    """
    errors: list[str] = []
    for url in urls:
        try:
            print(f"下载 CloakBrowser：{url}")
            download_file(url, target)
            return
        except Exception as exc:
            if target.exists():
                target.unlink()
            errors.append(f"{url} -> {exc}")
            print(f"下载失败，尝试下一个地址：{exc}", file=sys.stderr)
    raise RuntimeError("所有 CloakBrowser 下载地址都失败：" + "；".join(errors))


def prepare_cloakbrowser(target_platform: str, force: bool, extract: bool) -> Path:
    """
    下载并解压 CloakBrowser。

    Args:
        target_platform: 目标平台，mac 或 win。
        force: 是否强制重新下载和解压。
        extract: 是否解压到 vendor/cloakbrowser。

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
        download_first_available(URLS[target_platform], archive_path)

    if not extract:
        print(f"CloakBrowser 压缩包已准备：{archive_path}")
        return archive_path

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
    parser.add_argument("--no-extract", action="store_true", help="只准备 zip 包，不解压")
    args = parser.parse_args()

    try:
        vendor_dir = prepare_cloakbrowser(args.platform, args.force, not args.no_extract)
    except Exception as exc:
        print(f"准备失败：{exc}", file=sys.stderr)
        raise
    print(f"准备完成：{vendor_dir}")


if __name__ == "__main__":
    main()
