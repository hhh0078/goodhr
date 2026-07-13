from __future__ import annotations

import os
from pathlib import Path


APP_ROOT = Path(__file__).resolve().parents[1]


def install_dir() -> Path:
    """
    返回 GoodHR 本地安装/运行根目录。

    Returns:
        Path: 用户选择的安装目录或源码运行目录。
    """
    configured = os.getenv("GOODHR_INSTALL_DIR")
    if configured:
        return Path(configured).expanduser().resolve()
    data_configured = os.getenv("GOODHR_AGENT_DATA_DIR")
    if data_configured:
        return Path(data_configured).expanduser().resolve().parent
    return APP_ROOT


def data_dir() -> Path:
    """
    返回 GoodHR 本地数据目录。

    Returns:
        Path: 安装目录下 data 目录；兼容旧环境变量 GOODHR_AGENT_DATA_DIR。
    """
    configured = os.getenv("GOODHR_AGENT_DATA_DIR")
    if configured:
        return Path(configured).expanduser().resolve()
    return install_dir() / "data"


def config_dir() -> Path:
    """
    返回 GoodHR 本地配置目录。

    Returns:
        Path: 安装目录下 config 目录。
    """
    path = install_dir() / "config"
    path.mkdir(parents=True, exist_ok=True)
    return path


def frontend_dir() -> Path:
    """
    返回 GoodHR 本地控制台前端目录。

    Returns:
        Path: 安装目录下 frontend 目录。
    """
    path = install_dir() / "frontend"
    path.mkdir(parents=True, exist_ok=True)
    return path


def frontend_current_dir() -> Path:
    """
    返回当前启用的本地控制台前端目录。

    Returns:
        Path: frontend/current 目录。
    """
    return frontend_dir() / "current"


def source_frontend_dist_dir() -> Path:
    """
    返回源码开发环境中的云端前端构建目录。

    Returns:
        Path: goodhr5/cloud/frontend/dist 目录。
    """
    return APP_ROOT.parent / "cloud" / "frontend" / "dist"
