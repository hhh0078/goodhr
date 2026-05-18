"""本文件负责管理 Local Agent 的本地任务目录和候选人 JSON。"""

from __future__ import annotations

import json
import re
from datetime import datetime, timezone
from pathlib import Path

from app.paths import data_dir


def init_task(task_id: str, cloud_user_id: str, platform_id: str, platform_account_id: str) -> dict:
    """初始化本地任务目录和 candidates.json。"""
    task_id = _clean_id(task_id, "task")
    task_path = task_dir(task_id)
    task_path.mkdir(parents=True, exist_ok=True)
    (task_path / "screenshots").mkdir(exist_ok=True)
    (task_path / "ocr").mkdir(exist_ok=True)
    (task_path / "logs.jsonl").touch(exist_ok=True)

    data = {
        "task_id": task_id,
        "cloud_user_id": cloud_user_id,
        "platform_id": platform_id,
        "platform_account_id": platform_account_id,
        "created_at": datetime.now(timezone.utc).isoformat(),
        "items": [],
    }
    _write_json(candidates_path(task_id), data)
    return data


def load_candidates(task_id: str) -> dict:
    """读取指定任务的 candidates.json。"""
    path = candidates_path(task_id)
    if not path.exists():
        raise FileNotFoundError("task candidates not found")
    return _read_json(path)


def save_candidate(task_id: str, candidate: dict) -> dict:
    """新增或更新一个候选人记录。"""
    data = load_candidates(task_id)
    candidate_id = str(candidate.get("id", "")).strip()
    if not candidate_id:
        candidate_id = "candidate_" + datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S%f")
        candidate["id"] = candidate_id

    candidate["updated_at"] = datetime.now(timezone.utc).isoformat()
    items = data.setdefault("items", [])
    for index, item in enumerate(items):
        if item.get("id") == candidate_id:
            items[index] = {**item, **candidate}
            _write_json(candidates_path(task_id), data)
            return items[index]

    candidate.setdefault("created_at", candidate["updated_at"])
    items.append(candidate)
    _write_json(candidates_path(task_id), data)
    return candidate


def delete_candidate(task_id: str, candidate_id: str) -> bool:
    """删除一个候选人记录。"""
    data = load_candidates(task_id)
    items = data.get("items", [])
    kept = [item for item in items if item.get("id") != candidate_id]
    if len(kept) == len(items):
        return False

    data["items"] = kept
    _write_json(candidates_path(task_id), data)
    return True


def list_screenshots(task_id: str) -> list[dict[str, str]]:
    """列出任务截图文件。"""
    directory = screenshots_dir(task_id)
    if not directory.exists():
        raise FileNotFoundError("task screenshots not found")

    items = []
    for path in sorted(directory.iterdir()):
        if not path.is_file():
            continue
        items.append({
            "filename": path.name,
            "path": "screenshots/" + path.name,
        })
    return items


def screenshot_path(task_id: str, filename: str) -> Path:
    """返回截图文件路径，并限制文件只能位于 screenshots 目录内。"""
    safe_name = Path(filename).name
    return screenshots_dir(task_id) / safe_name


def delete_screenshot(task_id: str, filename: str) -> bool:
    """删除任务截图文件。"""
    path = screenshot_path(task_id, filename)
    if not path.exists():
        return False
    path.unlink()
    return True


def save_ocr_text(task_id: str, candidate_id: str, text: str) -> dict[str, str]:
    """保存候选人的 OCR 文本到本地任务目录。"""
    task_id = _clean_id(task_id, "task")
    candidate_id = _clean_id(candidate_id, "candidate")
    path = ocr_dir(task_id) / f"{candidate_id}.txt"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")
    return {
        "candidate_id": candidate_id,
        "path": "ocr/" + path.name,
    }


def task_dir(task_id: str) -> Path:
    """返回本地任务目录路径。"""
    return data_dir() / "tasks" / _clean_id(task_id, "task")


def candidates_path(task_id: str) -> Path:
    """返回本地任务 candidates.json 路径。"""
    return task_dir(task_id) / "candidates.json"


def screenshots_dir(task_id: str) -> Path:
    """返回本地任务截图目录路径。"""
    return task_dir(task_id) / "screenshots"


def ocr_dir(task_id: str) -> Path:
    """返回本地任务 OCR 目录路径。"""
    return task_dir(task_id) / "ocr"


def _clean_id(value: str, fallback: str) -> str:
    """清理外部传入 ID，避免写入 agent_data 之外的路径。"""
    cleaned = re.sub(r"[^a-zA-Z0-9_-]+", "_", str(value)).strip("_")
    return cleaned or fallback


def _read_json(path: Path) -> dict:
    """读取 JSON 文件。"""
    with path.open("r", encoding="utf-8") as file:
        return json.load(file)


def _write_json(path: Path, data: dict) -> None:
    """写入 JSON 文件。"""
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as file:
        json.dump(data, file, ensure_ascii=False, indent=2)
        file.write("\n")
