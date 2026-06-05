"""本文件负责把后台前端构建产物打包成本地控制台更新包。"""

from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import zipfile
from datetime import datetime, timezone
from pathlib import Path


PROJECT_ROOT = Path(__file__).resolve().parents[1]
DIST_DIR = PROJECT_ROOT / "dist"
OUTPUT_DIR = PROJECT_ROOT / "dist-agent-console"
DEFAULT_BASE_URL = "https://goodhr5.58it.cn/agent-console"


def main() -> None:
    """
    生成本地控制台前端 zip 和 manifest。
    """
    args = parse_args()
    version = args.version or datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")
    package_dir = prepare_package_dir(version)
    archive_path = OUTPUT_DIR / f"console-{version}.zip"
    create_archive(package_dir, archive_path)
    digest = sha256_file(archive_path)
    manifest = {
        "version": version,
        "url": f"{args.base_url.rstrip('/')}/{archive_path.name}",
        "sha256": digest,
    }
    manifest_path = OUTPUT_DIR / "manifest.json"
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"控制台前端包：{archive_path}")
    print(f"manifest：{manifest_path}")
    print(f"sha256：{digest}")


def parse_args() -> argparse.Namespace:
    """
    解析命令行参数。

    Returns:
        argparse.Namespace: 解析后的参数。
    """
    parser = argparse.ArgumentParser(description="打包 GoodHR 本地控制台前端")
    parser.add_argument("--version", default="", help="前端包版本号，默认使用 UTC 时间")
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL, help="manifest 中写入的下载基础地址")
    return parser.parse_args()


def prepare_package_dir(version: str) -> Path:
    """
    准备待压缩的控制台目录。

    Args:
        version: 前端包版本号。

    Returns:
        Path: 待压缩目录。
    """
    admin_index = DIST_DIR / "admin" / "index.html"
    assets_dir = DIST_DIR / "assets"
    if not admin_index.exists():
        raise FileNotFoundError("缺少 dist/admin/index.html，请先执行 npm run build")
    if not assets_dir.exists():
        raise FileNotFoundError("缺少 dist/assets，请先执行 npm run build")

    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    package_dir = OUTPUT_DIR / f"console-{version}"
    if package_dir.exists():
        shutil.rmtree(package_dir)
    package_dir.mkdir(parents=True, exist_ok=True)
    shutil.copy2(admin_index, package_dir / "index.html")
    shutil.copytree(assets_dir, package_dir / "assets")
    return package_dir


def create_archive(source_dir: Path, archive_path: Path) -> None:
    """
    创建 zip 压缩包。

    Args:
        source_dir: 待压缩目录。
        archive_path: 输出 zip 路径。
    """
    if archive_path.exists():
        archive_path.unlink()
    with zipfile.ZipFile(archive_path, "w", zipfile.ZIP_DEFLATED) as zip_file:
        for path in sorted(source_dir.rglob("*")):
            if path.is_file():
                zip_file.write(path, path.relative_to(source_dir))


def sha256_file(path: Path) -> str:
    """
    计算文件 sha256。

    Args:
        path: 文件路径。

    Returns:
        str: sha256 十六进制字符串。
    """
    digest = hashlib.sha256()
    with path.open("rb") as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


if __name__ == "__main__":
    main()
