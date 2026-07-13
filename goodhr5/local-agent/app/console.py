"""本文件负责提供本地控制台前端页面。"""

from __future__ import annotations

import os
import time
import urllib.error
import urllib.request
from pathlib import Path

from fastapi import HTTPException
from fastapi.responses import FileResponse, HTMLResponse, Response

from app.paths import frontend_current_dir, source_frontend_dist_dir


DEV_FRONTEND_URL = "http://127.0.0.1:5173"
_dev_check_expires_at = 0.0
_dev_check_available = False
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


async def console_index_response(request_path: str = "/admin/", query_string: str = ""):
    """
    返回本地控制台入口页面。

    Args:
        request_path: 当前请求路径，开发模式下用于代理 Vite 子路由。
        query_string: 当前请求查询字符串。

    Returns:
        Response: 开发服务、已下载前端包或内置初始化页面响应。
    """
    if await _dev_server_available():
        return await _proxy_dev_response(request_path, query_string)
    index_path = _console_index_path()
    if index_path.exists():
        return FileResponse(index_path, media_type="text/html")
    return HTMLResponse(FALLBACK_HTML)


async def console_asset_response(path: str, query_string: str = ""):
    """
    返回本地控制台静态资源。

    Args:
        path: 前端资源相对路径。
        query_string: 当前请求查询字符串。

    Returns:
        Response: 开发服务代理响应或静态资源响应。
    """
    if await _dev_server_available():
        return await _proxy_dev_response("/" + path.lstrip("/"), query_string)
    safe_path = _safe_frontend_path(path)
    if safe_path.is_dir():
        return await console_index_response()
    if not safe_path.exists():
        raise HTTPException(404, "console asset not found")
    return FileResponse(safe_path)


async def console_dev_proxy_response(path: str, query_string: str = ""):
    """
    代理 Vite 开发服务资源。

    Args:
        path: 前端开发资源路径。
        query_string: 当前请求查询字符串。

    Returns:
        Response: Vite 开发服务响应。
    """
    if not await _dev_server_available():
        raise HTTPException(404, "frontend dev server not found")
    return await _proxy_dev_response("/" + path.lstrip("/"), query_string)


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


async def _dev_server_available() -> bool:
    """
    检查源码前端 Vite 开发服务是否可用。

    Returns:
        bool: 可用时返回 true。
    """
    global _dev_check_available, _dev_check_expires_at
    now = time.monotonic()
    if now < _dev_check_expires_at:
        return _dev_check_available
    try:
        response = await _fetch_dev_url(_frontend_dev_url() + "/admin/", timeout=0.35)
        _dev_check_available = response["status_code"] < 500
    except Exception:
        _dev_check_available = False
    _dev_check_expires_at = now + 1
    return _dev_check_available


async def _proxy_dev_response(path: str, query_string: str = "") -> Response:
    """
    从 Vite 开发服务读取响应并透传给浏览器。

    Args:
        path: 需要代理的前端路径。
        query_string: URL 查询字符串。

    Returns:
        Response: 代理后的 HTTP 响应。
    """
    url = _frontend_dev_url() + path
    if query_string:
        url = f"{url}?{query_string}"
    try:
        response = await _fetch_dev_url(url, timeout=10.0)
    except Exception as exc:
        raise HTTPException(502, f"frontend dev server request failed: {exc}") from exc
    return Response(
        content=response["content"],
        status_code=response["status_code"],
        headers=_proxy_headers(response["headers"]),
        media_type=response["headers"].get("content-type"),
    )


async def _fetch_dev_url(url: str, timeout: float) -> dict:
    """
    在线程中请求前端开发服务，避免阻塞事件循环。

    Args:
        url: 完整请求地址。
        timeout: 请求超时时间，单位秒。

    Returns:
        dict: 包含状态码、响应头和响应内容。
    """
    import asyncio

    return await asyncio.to_thread(_fetch_dev_url_sync, url, timeout)


def _fetch_dev_url_sync(url: str, timeout: float) -> dict:
    """
    同步请求前端开发服务。

    Args:
        url: 完整请求地址。
        timeout: 请求超时时间，单位秒。

    Returns:
        dict: 包含状态码、响应头和响应内容。
    """
    request = urllib.request.Request(url, headers={"Accept": "*/*"})
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return {
                "status_code": response.status,
                "headers": dict(response.headers.items()),
                "content": response.read(),
            }
    except urllib.error.HTTPError as exc:
        return {
            "status_code": exc.code,
            "headers": dict(exc.headers.items()),
            "content": exc.read(),
        }


def _frontend_dev_url() -> str:
    """
    返回前端开发服务地址。

    Returns:
        str: Vite 开发服务基础地址。
    """
    return os.getenv("GOODHR_FRONTEND_DEV_URL", DEV_FRONTEND_URL).rstrip("/")


def _proxy_headers(headers: dict[str, str]) -> dict[str, str]:
    """
    过滤代理响应中不应该透传的头。

    Args:
        headers: Vite 开发服务响应头。

    Returns:
        dict[str, str]: 可透传响应头。
    """
    blocked = {
        "connection",
        "content-encoding",
        "content-length",
        "keep-alive",
        "transfer-encoding",
    }
    return {
        key: value
        for key, value in headers.items()
        if key.lower() not in blocked
    }


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
