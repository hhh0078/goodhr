"""本文件负责下载和启用本地控制台前端包。"""

from __future__ import annotations

import hashlib
import json
import shutil
import urllib.request
import zipfile
from pathlib import Path

from app.paths import frontend_current_dir, frontend_dir


DEFAULT_CONSOLE_MANIFEST_URL = "https://goodhr5.58it.cn/agent-console/manifest.json"


def console_manifest_path() -> Path:
    """
    返回本地控制台版本记录文件路径。

    Returns:
        Path: manifest 记录文件。
    """
    path = frontend_dir() / "manifest.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    return path


def load_local_console_manifest() -> dict:
    """
    读取本地已启用控制台版本信息。

    Returns:
        dict: 本地 manifest，读取失败时返回空字典。
    """
    path = console_manifest_path()
    if not path.exists():
        return {}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except Exception:
        return {}
    return data if isinstance(data, dict) else {}


def update_console_frontend(manifest_url: str = DEFAULT_CONSOLE_MANIFEST_URL) -> dict:
    """
    检查并更新本地控制台前端包。

    Args:
        manifest_url: 云端前端包 manifest 地址。

    Returns:
        dict: 更新结果。
    """
    remote = _download_json(manifest_url)
    version = str(remote.get("version") or "").strip()
    url = str(remote.get("url") or "").strip()
    sha256 = str(remote.get("sha256") or "").strip().lower()
    if not version or not url or not sha256:
        raise ValueError("控制台前端 manifest 缺少 version、url 或 sha256")

    local = load_local_console_manifest()
    if local.get("version") == version and (frontend_current_dir() / "index.html").exists():
        return {"updated": False, "version": version, "reason": "already_latest"}

    archive = _download_file(url, frontend_dir() / "downloads" / f"console-{version}.zip")
    digest = _sha256_file(archive)
    if digest != sha256:
        raise ValueError(f"控制台前端包校验失败 expected={sha256} actual={digest}")

    release_dir = frontend_dir() / "releases" / version
    if release_dir.exists():
        shutil.rmtree(release_dir)
    release_dir.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(archive) as zip_file:
        _extract_zip_safely(zip_file, release_dir)

    current = frontend_current_dir()
    if current.exists():
        shutil.rmtree(current)
    shutil.copytree(_resolve_release_root(release_dir), current)
    console_manifest_path().write_text(json.dumps(remote, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return {"updated": True, "version": version, "path": str(current)}


def _download_json(url: str) -> dict:
    """
    下载 JSON 文件。

    Args:
        url: JSON 地址。

    Returns:
        dict: JSON 对象。
    """
    with urllib.request.urlopen(url, timeout=15) as response:
        data = json.loads(response.read().decode("utf-8"))
    if not isinstance(data, dict):
        raise ValueError("manifest 不是 JSON 对象")
    return data


def _download_file(url: str, target: Path) -> Path:
    """
    下载文件到本地路径。

    Args:
        url: 下载地址。
        target: 目标路径。

    Returns:
        Path: 下载后的文件路径。
    """
    target.parent.mkdir(parents=True, exist_ok=True)
    temp_target = target.with_suffix(target.suffix + ".tmp")
    with urllib.request.urlopen(url, timeout=60) as response, temp_target.open("wb") as file:
        shutil.copyfileobj(response, file)
    temp_target.replace(target)
    return target


def _sha256_file(path: Path) -> str:
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


def _resolve_release_root(release_dir: Path) -> Path:
    """
    解析前端压缩包真正的根目录。

    Args:
        release_dir: 解压目录。

    Returns:
        Path: 包含 index.html 的目录。
    """
    if (release_dir / "index.html").exists():
        return release_dir
    candidates = [path for path in release_dir.iterdir() if path.is_dir() and (path / "index.html").exists()]
    if len(candidates) == 1:
        return candidates[0]
    raise ValueError("控制台前端包中未找到 index.html")


def _extract_zip_safely(zip_file: zipfile.ZipFile, target_dir: Path) -> None:
    """
    安全解压前端压缩包。

    Args:
        zip_file: 已打开的 zip 文件。
        target_dir: 目标解压目录。
    """
    root = target_dir.resolve()
    for member in zip_file.infolist():
        target = (root / member.filename).resolve()
        if target != root and root not in target.parents:
            raise ValueError("控制台前端包包含非法路径")
    zip_file.extractall(root)
