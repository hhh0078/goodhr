"""本文件负责管理本地 SQLite 岗位模板和默认提示词。"""

from __future__ import annotations

import json
import uuid
from typing import Any

from app.local_db import connect
from app.local_tasks import now_iso


DEFAULT_REVIEW_PROMPT = """你是一个资深的HR专家。当前候选人分数接近岗位阈值，请做打招呼前二次复核评分。

重要提示：
1. 仅输出 JSON，不能输出其它内容。
2. 返回字段必须是 score 和 reason。
3. score 范围是 0-100，可以是小数。
4. reason 控制在30字以内。
5. 评分更关注风险点与关键硬指标。

岗位要求：
${岗位信息}

候选人信息：
${候选人信息}

请返回JSON：{"score": 72, "reason": "边界候选人可谨慎通过"}"""

OPTIMIZE_REQUIREMENT_PROMPT = """你是一个招聘筛选规则整理助手。请把用户输入的岗位要求整理成适合 AI 筛选候选人简历的规则。

要求：
1. 只保留候选人自身条件，不要保留岗位福利、薪资待遇、工作时间、公司介绍、岗位职责、工作内容。
2. 去掉无法从简历中稳定判断的主观要求，例如：有上进心、责任心强、抗压能力强、沟通能力好、性格开朗、团队意识强、吃苦耐劳等。
3. 优先保留硬性条件，例如：学历、专业、工作年限、行业经验、岗位经验、证书、技能、城市、年龄、到岗状态。
4. 如果原文里有模糊条件，请改写成更清晰的筛选规则。
5. 输出中文，按条目列出，不要解释，不要输出 JSON。

用户输入：
{{input}}"""


def list_local_positions() -> list[dict[str, Any]]:
    """
    读取本地岗位模板列表。

    Returns:
        list[dict[str, Any]]: 岗位模板列表。
    """
    with connect() as conn:
        rows = conn.execute("SELECT * FROM local_positions ORDER BY updated_at DESC").fetchall()
    return [_position_row_to_dict(row) for row in rows]


def save_local_position(payload: dict[str, Any]) -> dict[str, Any]:
    """
    新增或更新本地岗位模板。

    Args:
        payload: 岗位模板参数。

    Returns:
        dict[str, Any]: 保存后的岗位模板。
    """
    now = now_iso()
    position_id = str(payload.get("id") or uuid.uuid4()).strip() or str(uuid.uuid4())
    current = _get_existing_position(position_id)
    created_at = str(current.get("created_at") if current else now)
    position = {
        "id": position_id,
        "platform_id": str(payload.get("platform_id") or "boss").strip().lower() or "boss",
        "name": str(payload.get("name") or "").strip(),
        "keywords": _string_list(payload.get("keywords")),
        "exclude_keywords": _string_list(payload.get("exclude_keywords")),
        "description": str(payload.get("description") or ""),
        "greet_message": str(payload.get("greet_message") or ""),
        "is_and_mode": bool(payload.get("is_and_mode")),
        "common_config": _safe_dict(payload.get("common_config")),
        "ai_config": _safe_dict(payload.get("ai_config")),
        "keyword_config": _safe_dict(payload.get("keyword_config")),
        "created_at": created_at,
        "updated_at": now,
    }
    if not position["name"]:
        raise ValueError("position name is required")
    with connect() as conn:
        conn.execute(
            """
            INSERT INTO local_positions (
                id, platform_id, name, keywords_json, exclude_keywords_json, description,
                greet_message, is_and_mode, common_config_json, ai_config_json,
                keyword_config_json, created_at, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(id) DO UPDATE SET
                platform_id=excluded.platform_id,
                name=excluded.name,
                keywords_json=excluded.keywords_json,
                exclude_keywords_json=excluded.exclude_keywords_json,
                description=excluded.description,
                greet_message=excluded.greet_message,
                is_and_mode=excluded.is_and_mode,
                common_config_json=excluded.common_config_json,
                ai_config_json=excluded.ai_config_json,
                keyword_config_json=excluded.keyword_config_json,
                updated_at=excluded.updated_at
            """,
            (
                position["id"],
                position["platform_id"],
                position["name"],
                json.dumps(position["keywords"], ensure_ascii=False),
                json.dumps(position["exclude_keywords"], ensure_ascii=False),
                position["description"],
                position["greet_message"],
                1 if position["is_and_mode"] else 0,
                json.dumps(position["common_config"], ensure_ascii=False),
                json.dumps(position["ai_config"], ensure_ascii=False),
                json.dumps(position["keyword_config"], ensure_ascii=False),
                position["created_at"],
                position["updated_at"],
            ),
        )
    saved = _get_existing_position(position_id)
    if not saved:
        raise FileNotFoundError("local position not found")
    return saved


def delete_local_position(position_id: str) -> None:
    """
    删除本地岗位模板。

    Args:
        position_id: 岗位模板 ID。
    """
    with connect() as conn:
        cursor = conn.execute("DELETE FROM local_positions WHERE id=?", (position_id,))
    if cursor.rowcount == 0:
        raise FileNotFoundError("local position not found")


def default_local_prompts() -> dict[str, str]:
    """
    返回本地岗位模板默认提示词。

    Returns:
        dict[str, str]: 默认提示词配置。
    """
    return {
        "filter_prompt": "",
        "open_detail_prompt": "",
        "review_prompt": DEFAULT_REVIEW_PROMPT,
    }


def optimize_requirement_prompt(text: str) -> str:
    """
    生成本地 AI 优化岗位要求的提示词。

    Args:
        text: 原始岗位要求。

    Returns:
        str: 可发送给 AI 的完整提示词。
    """
    return OPTIMIZE_REQUIREMENT_PROMPT.replace("{{input}}", str(text or "").strip())


def _get_existing_position(position_id: str) -> dict[str, Any] | None:
    """
    按 ID 读取本地岗位模板。

    Args:
        position_id: 岗位模板 ID。

    Returns:
        dict[str, Any] | None: 找到时返回岗位模板，否则返回 None。
    """
    with connect() as conn:
        row = conn.execute("SELECT * FROM local_positions WHERE id=?", (position_id,)).fetchone()
    return _position_row_to_dict(row) if row else None


def _position_row_to_dict(row: Any) -> dict[str, Any]:
    """
    将 SQLite 岗位模板行转换为前端结构。

    Args:
        row: SQLite 行对象。

    Returns:
        dict[str, Any]: 岗位模板字典。
    """
    return {
        "id": str(row["id"] or ""),
        "platform_id": str(row["platform_id"] or "boss"),
        "name": str(row["name"] or ""),
        "keywords": _json_list(row["keywords_json"]),
        "exclude_keywords": _json_list(row["exclude_keywords_json"]),
        "description": str(row["description"] or ""),
        "greet_message": str(row["greet_message"] or ""),
        "is_and_mode": bool(row["is_and_mode"]),
        "common_config": _json_dict(row["common_config_json"]),
        "ai_config": _json_dict(row["ai_config_json"]),
        "keyword_config": _json_dict(row["keyword_config_json"]),
        "created_at": str(row["created_at"] or ""),
        "updated_at": str(row["updated_at"] or ""),
    }


def _string_list(value: Any) -> list[str]:
    """
    将输入值转换为字符串列表。

    Args:
        value: 原始值。

    Returns:
        list[str]: 去空后的字符串列表。
    """
    if not isinstance(value, list):
        return []
    return [str(item).strip() for item in value if str(item or "").strip()]


def _safe_dict(value: Any) -> dict[str, Any]:
    """
    确保输入值是字典。

    Args:
        value: 原始值。

    Returns:
        dict[str, Any]: 字典值。
    """
    return value if isinstance(value, dict) else {}


def _json_list(value: Any) -> list[str]:
    """
    解析 JSON 字符串列表。

    Args:
        value: JSON 字符串。

    Returns:
        list[str]: 字符串列表。
    """
    try:
        parsed = json.loads(str(value or "[]"))
    except json.JSONDecodeError:
        return []
    return _string_list(parsed)


def _json_dict(value: Any) -> dict[str, Any]:
    """
    解析 JSON 字符串对象。

    Args:
        value: JSON 字符串。

    Returns:
        dict[str, Any]: 字典对象。
    """
    try:
        parsed = json.loads(str(value or "{}"))
    except json.JSONDecodeError:
        return {}
    return parsed if isinstance(parsed, dict) else {}
