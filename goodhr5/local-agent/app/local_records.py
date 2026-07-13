"""本文件负责管理 Local Agent 本地设置、下载记录和截图记录。"""

from __future__ import annotations

import json
import uuid
from datetime import datetime, timezone
from typing import Any

from app.local_db import connect


def get_local_settings() -> dict[str, Any]:
    """
    读取全部本地设置。

    Returns:
        dict[str, Any]: 设置字典。
    """
    with connect() as conn:
        rows = conn.execute("SELECT key, value FROM local_settings ORDER BY key ASC").fetchall()
    result: dict[str, Any] = {}
    for row in rows:
        result[str(row["key"])] = _decode_value(row["value"])
    return result


def save_local_settings(payload: dict[str, Any]) -> dict[str, Any]:
    """
    保存本地设置。

    Args:
        payload: 设置字典。

    Returns:
        dict[str, Any]: 保存后的全部设置。
    """
    now = _now_iso()
    with connect() as conn:
        for key, value in (payload or {}).items():
            clean_key = str(key or "").strip()
            if not clean_key:
                continue
            conn.execute(
                """
                INSERT INTO local_settings(key, value, updated_at)
                VALUES (?, ?, ?)
                ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at
                """,
                (clean_key, json.dumps(value, ensure_ascii=False), now),
            )
    return get_local_settings()


def list_local_downloads(task_id: str = "") -> list[dict[str, Any]]:
    """
    读取本地下载记录。

    Args:
        task_id: 可选任务 ID。

    Returns:
        list[dict[str, Any]]: 下载记录列表。
    """
    query = "SELECT * FROM local_downloads"
    args: tuple[Any, ...] = ()
    if task_id:
        query += " WHERE task_id=?"
        args = (task_id,)
    query += " ORDER BY created_at DESC"
    with connect() as conn:
        rows = conn.execute(query, args).fetchall()
    return [_row_to_dict(row) for row in rows]


def save_local_download(payload: dict[str, Any]) -> dict[str, Any]:
    """
    保存本地下载记录。

    Args:
        payload: 下载记录参数。

    Returns:
        dict[str, Any]: 保存后的下载记录。
    """
    now = _now_iso()
    item_id = str(payload.get("id") or uuid.uuid4())
    item = {
        "id": item_id,
        "task_id": str(payload.get("task_id") or ""),
        "url": str(payload.get("url") or ""),
        "file_path": str(payload.get("file_path") or ""),
        "file_name": str(payload.get("file_name") or ""),
        "mime_type": str(payload.get("mime_type") or ""),
        "size": _safe_int(payload.get("size"), 0),
        "status": str(payload.get("status") or "saved"),
        "created_at": str(payload.get("created_at") or now),
        "updated_at": now,
    }
    with connect() as conn:
        conn.execute(
            """
            INSERT INTO local_downloads (
                id, task_id, url, file_path, file_name, mime_type, size, status, created_at, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(id) DO UPDATE SET
                task_id=excluded.task_id,
                url=excluded.url,
                file_path=excluded.file_path,
                file_name=excluded.file_name,
                mime_type=excluded.mime_type,
                size=excluded.size,
                status=excluded.status,
                updated_at=excluded.updated_at
            """,
            (
                item["id"],
                item["task_id"],
                item["url"],
                item["file_path"],
                item["file_name"],
                item["mime_type"],
                item["size"],
                item["status"],
                item["created_at"],
                item["updated_at"],
            ),
        )
    return item


def list_local_screenshots(task_id: str = "") -> list[dict[str, Any]]:
    """
    读取本地截图记录。

    Args:
        task_id: 可选任务 ID。

    Returns:
        list[dict[str, Any]]: 截图记录列表。
    """
    query = "SELECT * FROM local_screenshots"
    args: tuple[Any, ...] = ()
    if task_id:
        query += " WHERE task_id=?"
        args = (task_id,)
    query += " ORDER BY created_at DESC"
    with connect() as conn:
        rows = conn.execute(query, args).fetchall()
    return [_row_to_dict(row) for row in rows]


def save_local_screenshot(payload: dict[str, Any]) -> dict[str, Any]:
    """
    保存本地截图记录。

    Args:
        payload: 截图记录参数。

    Returns:
        dict[str, Any]: 保存后的截图记录。
    """
    item = {
        "id": str(payload.get("id") or uuid.uuid4()),
        "task_id": str(payload.get("task_id") or ""),
        "file_path": str(payload.get("file_path") or ""),
        "label": str(payload.get("label") or ""),
        "width": _safe_int(payload.get("width"), 0),
        "height": _safe_int(payload.get("height"), 0),
        "created_at": str(payload.get("created_at") or _now_iso()),
    }
    with connect() as conn:
        conn.execute(
            """
            INSERT OR REPLACE INTO local_screenshots (
                id, task_id, file_path, label, width, height, created_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                item["id"],
                item["task_id"],
                item["file_path"],
                item["label"],
                item["width"],
                item["height"],
                item["created_at"],
            ),
        )
    return item


def _row_to_dict(row) -> dict[str, Any]:
    """
    将 SQLite 行转换为字典。

    Args:
        row: SQLite 查询行。

    Returns:
        dict[str, Any]: 字典。
    """
    return {key: row[key] for key in row.keys()}


def _decode_value(value: str) -> Any:
    """
    解析设置值。

    Args:
        value: JSON 字符串。

    Returns:
        Any: 解析后的值。
    """
    try:
        return json.loads(str(value or "null"))
    except Exception:
        return value


def _safe_int(value: Any, default: int) -> int:
    """
    将任意值安全转换为整数。

    Args:
        value: 原始值。
        default: 默认值。

    Returns:
        int: 整数。
    """
    try:
        return int(value)
    except (TypeError, ValueError):
        return default


def _now_iso() -> str:
    """
    返回当前 UTC 时间字符串。

    Returns:
        str: ISO 格式时间。
    """
    return datetime.now(timezone.utc).isoformat()
