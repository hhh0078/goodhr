"""本文件负责读取 Local Agent 本地设置。"""

from __future__ import annotations

import json
import os
from pathlib import Path

from app.paths import data_dir


SETTINGS_FILE_NAME = "settings.json"


def config_dir() -> Path:
    """
    返回 Local Agent 配置目录。

    Returns:
        Path: 与 agent_data 同级的 config 目录。
    """
    path = data_dir().parent / "config"
    path.mkdir(parents=True, exist_ok=True)
    return path


def settings_path() -> Path:
    """
    返回本地设置文件路径。

    Returns:
        Path: settings.json 文件路径。
    """
    return config_dir() / SETTINGS_FILE_NAME


def default_download_dir() -> Path:
    """
    返回系统默认下载目录。

    Returns:
        Path: 当前用户 Downloads 目录；不存在时返回 agent_data/downloads。
    """
    home_downloads = Path.home() / "Downloads"
    if home_downloads.exists():
        return home_downloads
    return data_dir() / "downloads"


def load_settings() -> dict:
    """
    读取本地设置 JSON。

    Returns:
        dict: 设置内容；文件不存在或损坏时返回空字典。
    """
    path = settings_path()
    if not path.exists():
        return {}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except Exception:
        return {}
    return data if isinstance(data, dict) else {}


def browser_download_dir() -> Path:
    """
    返回浏览器下载目录。

    Returns:
        Path: 用户设置目录、环境变量目录或系统默认下载目录。
    """
    configured = os.getenv("GOODHR_AGENT_DOWNLOAD_DIR", "").strip()
    if not configured:
        configured = str(load_settings().get("browser_download_dir") or "").strip()
    path = Path(configured).expanduser() if configured else default_download_dir()
    try:
        path.mkdir(parents=True, exist_ok=True)
        return path.resolve()
    except Exception:
        fallback = data_dir() / "downloads"
        fallback.mkdir(parents=True, exist_ok=True)
        return fallback.resolve()
