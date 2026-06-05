"""本文件负责提供本地控制台前端页面。"""

from __future__ import annotations

from pathlib import Path

from fastapi import HTTPException
from fastapi.responses import FileResponse, HTMLResponse

from app.paths import frontend_current_dir, source_frontend_dist_dir


FALLBACK_HTML = """<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>GoodHR 本地控制台</title>
  <style>
    body { margin: 0; font-family: Arial, "Microsoft YaHei", sans-serif; background: #f4f6f5; color: #17201b; }
    main { max-width: 720px; margin: 12vh auto; padding: 28px; }
    h1 { font-size: 28px; margin: 0 0 12px; }
    p { line-height: 1.7; color: #405047; }
    code { background: #e7ece9; padding: 2px 6px; border-radius: 4px; }
  </style>
</head>
<body>
  <main>
    <h1>GoodHR 本地控制台正在初始化</h1>
    <p>本地服务已经启动，但控制台前端包还没有准备好。后续版本会在启动时自动从服务器下载最新控制台。</p>
    <p>当前可以先访问 <code>/health</code> 确认本地程序状态。</p>
  </main>
</body>
</html>
"""


def console_index_response():
    """
    返回本地控制台入口页面。

    Returns:
        FileResponse | HTMLResponse: 已下载前端包时返回 index.html，否则返回内置初始化页面。
    """
    index_path = _console_index_path()
    if index_path.exists():
        return FileResponse(index_path, media_type="text/html")
    return HTMLResponse(FALLBACK_HTML)


def console_asset_response(path: str):
    """
    返回本地控制台静态资源。

    Args:
        path: 前端资源相对路径。

    Returns:
        FileResponse: 静态资源响应。
    """
    safe_path = _safe_frontend_path(path)
    if safe_path.is_dir():
        return console_index_response()
    if not safe_path.exists():
        raise HTTPException(404, "console asset not found")
    return FileResponse(safe_path)


def _safe_frontend_path(path: str) -> Path:
    """
    解析本地控制台静态资源路径，并限制在 frontend/current 内。

    Args:
        path: 请求中的相对路径。

    Returns:
        Path: 安全的本地文件路径。
    """
    root = _console_root_dir().resolve()
    target = (root / str(path or "").lstrip("/")).resolve()
    if target != root and root not in target.parents:
        raise HTTPException(400, "invalid console asset path")
    return target


def _console_index_path() -> Path:
    """
    返回当前本地控制台入口 HTML 路径。

    Returns:
        Path: index.html 路径。
    """
    dev_index = source_frontend_dist_dir() / "admin" / "index.html"
    if dev_index.exists():
        return dev_index
    return frontend_current_dir() / "index.html"


def _console_root_dir() -> Path:
    """
    返回当前本地控制台静态资源根目录。

    Returns:
        Path: 静态资源根目录。
    """
    dev_index = source_frontend_dist_dir() / "admin" / "index.html"
    if dev_index.exists():
        return source_frontend_dist_dir()
    return frontend_current_dir()


def has_source_frontend_build() -> bool:
    """
    判断源码前端是否已经构建。

    Returns:
        bool: 存在 dist/admin/index.html 时返回 true。
    """
    return (source_frontend_dist_dir() / "admin" / "index.html").exists()
