"""本文件负责管理 Local Agent 的招聘平台 profile 元数据。"""

from __future__ import annotations

import json
import re
from datetime import datetime, timezone
from pathlib import Path

from app.paths import data_dir


PROFILES_FILE = "profiles.json"


def list_profiles(platform_id: str = "") -> list[dict[str, str]]:
    """读取本地 profile 列表，可按平台过滤。"""
    profiles = _read_profiles()
    if not platform_id:
        return profiles
    return [item for item in profiles if item.get("platform_id") == platform_id]


def create_profile(platform_id: str, display_name: str) -> dict[str, str]:
    """创建一个本地 profile 元数据记录。"""
    platform_id = platform_id.strip()
    display_name = display_name.strip()
    if not platform_id:
        raise ValueError("platform_id is required")
    if not display_name:
        raise ValueError("display_name is required")

    profiles = _read_profiles()
    profile_id = _next_profile_id(platform_id, profiles)
    profile = {
        "id": profile_id,
        "platform_id": platform_id,
        "display_name": display_name,
        "created_at": datetime.now(timezone.utc).isoformat(),
    }
    profiles.append(profile)
    _write_profiles(profiles)
    return profile


def delete_profile(profile_id: str) -> bool:
    """删除一个本地 profile 元数据记录。"""
    profile_id = profile_id.strip()
    profiles = _read_profiles()
    kept = [item for item in profiles if item.get("id") != profile_id]
    if len(kept) == len(profiles):
        return False

    _write_profiles(kept)
    return True


def profiles_file_path() -> Path:
    """返回本地 profile 元数据文件路径。"""
    return data_dir() / PROFILES_FILE


def _read_profiles() -> list[dict[str, str]]:
    """读取 profile 元数据文件，不存在时返回空列表。"""
    path = profiles_file_path()
    if not path.exists():
        return []

    with path.open("r", encoding="utf-8") as file:
        data = json.load(file)
    if not isinstance(data, list):
        return []
    return data


def _write_profiles(profiles: list[dict[str, str]]) -> None:
    """写入 profile 元数据文件。"""
    path = profiles_file_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as file:
        json.dump(profiles, file, ensure_ascii=False, indent=2)
        file.write("\n")


def _next_profile_id(platform_id: str, profiles: list[dict[str, str]]) -> str:
    """根据平台和现有 profile 列表生成下一个 profile ID。"""
    safe_platform = re.sub(r"[^a-zA-Z0-9_-]+", "_", platform_id).strip("_") or "platform"
    prefix = f"{safe_platform}_"
    max_index = 0
    for profile in profiles:
        profile_id = profile.get("id", "")
        if not profile_id.startswith(prefix):
            continue
        suffix = profile_id.removeprefix(prefix)
        if suffix.isdigit():
            max_index = max(max_index, int(suffix))
    return f"{prefix}{max_index + 1}"
