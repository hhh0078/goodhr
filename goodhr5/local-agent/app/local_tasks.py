"""本文件负责管理本地 SQLite 任务、日志和候选人数据。"""

from __future__ import annotations

import json
import uuid
from datetime import datetime, timezone

from app.local_db import connect


def now_iso() -> str:
    """
    返回当前 UTC 时间字符串。

    Returns:
        str: ISO 格式时间。
    """
    return datetime.now(timezone.utc).isoformat()


def create_local_task(payload: dict) -> dict:
    """
    创建本地任务。

    Args:
        payload: 任务参数。

    Returns:
        dict: 新建任务。
    """
    raw_match_limit = payload.get("match_limit") or 0
    try:
        match_limit = int(raw_match_limit)
    except (TypeError, ValueError):
        match_limit = 0
    task_id = str(payload.get("id") or uuid.uuid4())
    created_at = now_iso()
    task = {
        "id": task_id,
        "name": str(payload.get("name") or ""),
        "platform_id": str(payload.get("platform_id") or "boss"),
        "platform_account_id": str(payload.get("platform_account_id") or ""),
        "position_id": str(payload.get("position_id") or ""),
        "mode": str(payload.get("mode") or "ai"),
        "match_limit": max(0, match_limit),
        "status": "pending",
        "enable_sound": 1 if payload.get("enable_sound") else 0,
        "position_snapshot": payload.get("position_snapshot") or {},
        "created_at": created_at,
        "updated_at": created_at,
    }
    with connect() as conn:
        conn.execute(
            """
            INSERT INTO local_tasks (
                id, name, platform_id, platform_account_id, position_id, mode, match_limit,
                status, enable_sound, position_snapshot, created_at, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                task["id"],
                task["name"],
                task["platform_id"],
                task["platform_account_id"],
                task["position_id"],
                task["mode"],
                task["match_limit"],
                task["status"],
                task["enable_sound"],
                json.dumps(task["position_snapshot"], ensure_ascii=False),
                task["created_at"],
                task["updated_at"],
            ),
        )
    return task


def list_local_tasks() -> list[dict]:
    """
    读取本地任务列表。

    Returns:
        list[dict]: 任务列表。
    """
    with connect() as conn:
        rows = conn.execute(
            """
            SELECT * FROM local_tasks
            ORDER BY created_at DESC
            """
        ).fetchall()
    return [_task_row_to_dict(row) for row in rows]


def update_local_task(task_id: str, payload: dict) -> dict:
    """
    更新本地任务基础信息。

    Args:
        task_id: 任务 ID。
        payload: 更新参数。

    Returns:
        dict: 更新后的任务。
    """
    raw_match_limit = payload.get("match_limit") or 0
    try:
        match_limit = int(raw_match_limit)
    except (TypeError, ValueError):
        match_limit = 0
    updated_at = now_iso()
    with connect() as conn:
        _ensure_local_task_exists(conn, task_id)
        conn.execute(
            """
            UPDATE local_tasks
            SET name=?, platform_id=?, platform_account_id=?, position_id=?, mode=?,
                match_limit=?, enable_sound=?, updated_at=?
            WHERE id=?
            """,
            (
                str(payload.get("name") or ""),
                str(payload.get("platform_id") or "boss"),
                str(payload.get("platform_account_id") or ""),
                str(payload.get("position_id") or ""),
                str(payload.get("mode") or "ai"),
                max(0, match_limit),
                1 if payload.get("enable_sound") else 0,
                updated_at,
                task_id,
            ),
        )
        row = conn.execute("SELECT * FROM local_tasks WHERE id=?", (task_id,)).fetchone()
    return _task_row_to_dict(row)


def delete_local_task(task_id: str) -> None:
    """
    删除本地任务及关联数据。

    Args:
        task_id: 任务 ID。
    """
    with connect() as conn:
        _ensure_local_task_exists(conn, task_id)
        conn.execute("DELETE FROM local_tasks WHERE id=?", (task_id,))


def update_local_task_status(task_id: str, status: str) -> dict:
    """
    更新本地任务状态。

    Args:
        task_id: 任务 ID。
        status: 新状态。

    Returns:
        dict: 更新后的任务。
    """
    updated_at = now_iso()
    with connect() as conn:
        conn.execute(
            "UPDATE local_tasks SET status=?, updated_at=? WHERE id=?",
            (status, updated_at, task_id),
        )
        row = conn.execute("SELECT * FROM local_tasks WHERE id=?", (task_id,)).fetchone()
    if row is None:
        raise FileNotFoundError("local task not found")
    return _task_row_to_dict(row)


def clear_local_task_logs(task_id: str) -> str:
    """
    清空本地任务日志。

    Args:
        task_id: 任务 ID。

    Returns:
        str: 清空时间。
    """
    cleared_at = now_iso()
    with connect() as conn:
        _ensure_local_task_exists(conn, task_id)
        conn.execute("DELETE FROM local_task_logs WHERE task_id=?", (task_id,))
    return cleared_at


def add_local_task_log(task_id: str, level: str, message: str) -> dict:
    """
    写入本地任务日志。

    Args:
        task_id: 任务 ID。
        level: 日志级别。
        message: 日志内容。

    Returns:
        dict: 日志记录。
    """
    created_at = now_iso()
    with connect() as conn:
        _ensure_local_task_exists(conn, task_id)
        cursor = conn.execute(
            "INSERT INTO local_task_logs(task_id, level, message, created_at) VALUES(?, ?, ?, ?)",
            (task_id, level or "info", message or "", created_at),
        )
    return {"id": cursor.lastrowid, "task_id": task_id, "level": level, "message": message, "created_at": created_at}


def list_local_task_logs(task_id: str, limit: int = 100) -> list[dict]:
    """
    读取本地任务日志。

    Args:
        task_id: 任务 ID。
        limit: 返回数量。

    Returns:
        list[dict]: 日志列表。
    """
    safe_limit = max(1, min(int(limit or 100), 500))
    with connect() as conn:
        rows = conn.execute(
            """
            SELECT id, task_id, level, message, created_at
            FROM local_task_logs
            WHERE task_id=?
            ORDER BY id DESC
            LIMIT ?
            """,
            (task_id, safe_limit),
        ).fetchall()
    return [dict(row) for row in reversed(rows)]


def save_local_candidate(task_id: str, candidate: dict) -> dict:
    """
    保存本地候选人。

    Args:
        task_id: 任务 ID。
        candidate: 候选人数据。

    Returns:
        dict: 候选人数据。
    """
    candidate_id = str(candidate.get("id") or uuid.uuid4())
    candidate_name = str(candidate.get("name") or candidate.get("candidate_name") or "")
    status = str(candidate.get("status") or "")
    now = now_iso()
    payload = dict(candidate)
    payload["id"] = candidate_id
    with connect() as conn:
        _ensure_local_task_exists(conn, task_id)
        existing = conn.execute(
            "SELECT created_at FROM local_candidates WHERE task_id=? AND id=?",
            (task_id, candidate_id),
        ).fetchone()
        created_at = str(existing["created_at"]) if existing else now
        conn.execute(
            """
            INSERT INTO local_candidates(id, task_id, candidate_name, status, payload, created_at, updated_at)
            VALUES(?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(task_id, id) DO UPDATE SET
                candidate_name=excluded.candidate_name,
                status=excluded.status,
                payload=excluded.payload,
                updated_at=excluded.updated_at
            """,
            (
                candidate_id,
                task_id,
                candidate_name,
                status,
                json.dumps(payload, ensure_ascii=False),
                created_at,
                now,
            ),
        )
    return payload


def list_local_candidates(task_id: str) -> list[dict]:
    """
    读取本地候选人列表。

    Args:
        task_id: 任务 ID。

    Returns:
        list[dict]: 候选人列表。
    """
    with connect() as conn:
        rows = conn.execute(
            "SELECT payload FROM local_candidates WHERE task_id=? ORDER BY updated_at DESC",
            (task_id,),
        ).fetchall()
    result = []
    for row in rows:
        try:
            result.append(json.loads(row["payload"]))
        except Exception:
            continue
    return result


def delete_local_candidate(task_id: str, candidate_id: str) -> None:
    """
    删除本地候选人。

    Args:
        task_id: 任务 ID。
        candidate_id: 候选人 ID。
    """
    with connect() as conn:
        _ensure_local_task_exists(conn, task_id)
        conn.execute(
            "DELETE FROM local_candidates WHERE task_id=? AND id=?",
            (task_id, candidate_id),
        )


def _ensure_local_task_exists(conn, task_id: str) -> None:
    """
    确认本地任务存在。

    Args:
        conn: SQLite 连接。
        task_id: 任务 ID。

    Raises:
        FileNotFoundError: 任务不存在时抛出。
    """
    row = conn.execute("SELECT 1 FROM local_tasks WHERE id=?", (task_id,)).fetchone()
    if row is None:
        raise FileNotFoundError("local task not found")


def _task_row_to_dict(row) -> dict:
    """
    将 SQLite 任务行转换为字典。

    Args:
        row: SQLite Row。

    Returns:
        dict: 任务字典。
    """
    item = dict(row)
    try:
        item["position_snapshot"] = json.loads(item.get("position_snapshot") or "{}")
    except Exception:
        item["position_snapshot"] = {}
    return item
