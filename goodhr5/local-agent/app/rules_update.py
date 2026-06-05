"""本文件负责检查和更新本地平台规则包。"""

from __future__ import annotations

import hashlib
import json
import urllib.request
from datetime import datetime, timezone
from typing import Any

from app.local_db import connect
from app.paths import data_dir


DEFAULT_RULES_MANIFEST_URL = "https://goodhr5.58it.cn/agent-rules/manifest.json"


def local_rules_dir():
    """
    返回本地规则目录。

    Returns:
        Path: rules 目录。
    """
    path = data_dir() / "rules"
    path.mkdir(parents=True, exist_ok=True)
    return path


def get_rules_status() -> dict[str, Any]:
    """
    读取本地规则包状态。

    Returns:
        dict[str, Any]: 规则状态。
    """
    with connect() as conn:
        rows = conn.execute("SELECT * FROM local_rule_versions ORDER BY platform_id ASC").fetchall()
    return {"rules": [{key: row[key] for key in row.keys()} for row in rows]}


def update_rules(manifest_url: str = DEFAULT_RULES_MANIFEST_URL) -> dict[str, Any]:
    """
    检查并更新平台规则包。

    Args:
        manifest_url: 规则 manifest 地址。

    Returns:
        dict[str, Any]: 更新结果。
    """
    manifest = _fetch_json(manifest_url)
    platforms = manifest.get("platforms") if isinstance(manifest.get("platforms"), dict) else {}
    updated: list[dict[str, Any]] = []
    skipped: list[dict[str, Any]] = []
    for platform_id, item in platforms.items():
        if not isinstance(item, dict):
            continue
        version = str(item.get("version") or "").strip()
        url = str(item.get("url") or "").strip()
        sha256 = str(item.get("sha256") or "").strip().lower()
        if not platform_id or not version or not url:
            continue
        current = _current_rule_version(str(platform_id))
        if current.get("version") == version and current.get("status") == "active":
            skipped.append({"platform_id": platform_id, "version": version, "reason": "already_latest"})
            continue
        data = _download_bytes(url)
        digest = hashlib.sha256(data).hexdigest()
        if sha256 and digest != sha256:
            _save_rule_version(str(platform_id), current.get("version", ""), "hash_failed")
            raise RuntimeError(f"规则包校验失败 platform={platform_id}")
        parsed = json.loads(data.decode("utf-8"))
        if not isinstance(parsed, dict):
            raise RuntimeError(f"规则包格式错误 platform={platform_id}")
        target = local_rules_dir() / f"{platform_id}-{version}.json"
        target.write_text(json.dumps(parsed, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        current_file = local_rules_dir() / f"{platform_id}.json"
        current_file.write_text(json.dumps(parsed, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        _save_rule_version(str(platform_id), version, "active")
        updated.append({"platform_id": platform_id, "version": version, "sha256": digest})
    return {"ok": True, "updated": updated, "skipped": skipped, "manifest_version": manifest.get("version", "")}


def _fetch_json(url: str) -> dict[str, Any]:
    """
    下载 JSON 数据。

    Args:
        url: 下载地址。

    Returns:
        dict[str, Any]: JSON 字典。
    """
    data = _download_bytes(url)
    parsed = json.loads(data.decode("utf-8"))
    if not isinstance(parsed, dict):
        raise RuntimeError("manifest 格式错误")
    return parsed


def _download_bytes(url: str) -> bytes:
    """
    下载字节内容。

    Args:
        url: 下载地址。

    Returns:
        bytes: 下载内容。
    """
    with urllib.request.urlopen(url, timeout=20) as response:
        return response.read()


def _current_rule_version(platform_id: str) -> dict[str, str]:
    """
    读取当前平台规则版本。

    Args:
        platform_id: 平台 ID。

    Returns:
        dict[str, str]: 版本记录。
    """
    with connect() as conn:
        row = conn.execute("SELECT * FROM local_rule_versions WHERE platform_id=?", (platform_id,)).fetchone()
    if row is None:
        return {"platform_id": platform_id, "version": "", "status": ""}
    return {key: str(row[key] or "") for key in row.keys()}


def _save_rule_version(platform_id: str, version: str, status: str) -> None:
    """
    保存平台规则版本。

    Args:
        platform_id: 平台 ID。
        version: 规则版本。
        status: 状态。
    """
    with connect() as conn:
        conn.execute(
            """
            INSERT INTO local_rule_versions(platform_id, version, status, updated_at)
            VALUES (?, ?, ?, ?)
            ON CONFLICT(platform_id) DO UPDATE SET
                version=excluded.version,
                status=excluded.status,
                updated_at=excluded.updated_at
            """,
            (platform_id, version, status, datetime.now(timezone.utc).isoformat()),
        )
