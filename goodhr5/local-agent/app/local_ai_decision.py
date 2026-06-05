"""本文件负责本地任务中的 AI 筛选决策。"""

from __future__ import annotations

import json
import re
from typing import Any

from app.local_ai import chat_with_local_ai
from app.vision_ai import clean_ai_text_output


DEFAULT_GREET_THRESHOLD = 70.0
DEFAULT_GREET_PROMPT = """你是一个资深的HR专家。
请根据岗位要求给候选人打“打招呼建议分”。

重要提示：
1. 仅输出 JSON，不能输出其它内容。
2. 返回字段必须是 score 和 reason。
3. score 范围是 0-100，可以是小数。
4. reason 控制在30字以内。
5. 禁止输出 Markdown，禁止输出 Markdown 代码块。

岗位要求：
{job_desc}

候选人信息：
{candidate_text}

请返回JSON：{{"score": 78, "reason": "匹配核心要求"}}"""


async def score_candidate_for_greet(task: dict[str, Any], candidate: dict[str, Any]) -> dict[str, Any]:
    """
    使用本地 AI 配置给候选人计算打招呼评分。

    Args:
        task: 本地任务。
        candidate: 候选人信息。

    Returns:
        dict[str, Any]: 评分结果，包含 score、reason、should_greet、threshold。
    """
    position = task.get("position_snapshot") if isinstance(task.get("position_snapshot"), dict) else {}
    threshold = greet_threshold(position)
    prompt = build_greet_score_prompt(position, candidate)
    result = await chat_with_local_ai(
        {
            "messages": [{"role": "user", "content": prompt}],
            "temperature": _ai_config_number(position, 0.2, "temperature"),
            "config": _runtime_ai_config(position),
        }
    )
    decision = parse_score_decision(str(result.get("content") or ""))
    score = clamp_score(decision.get("score", 0))
    reason = truncate_text(str(decision.get("reason") or "AI未给出原因").strip(), 30)
    return {
        "score": score,
        "reason": reason,
        "should_greet": score >= threshold,
        "threshold": threshold,
        "usage": result.get("usage") if isinstance(result.get("usage"), dict) else {},
        "elapsed_ms": int(result.get("elapsed_ms") or 0),
    }


def build_greet_score_prompt(position: dict[str, Any], candidate: dict[str, Any]) -> str:
    """
    构建打招呼评分提示词。

    Args:
        position: 岗位快照。
        candidate: 候选人信息。

    Returns:
        str: 完整提示词。
    """
    ai_config = position.get("ai_config") if isinstance(position.get("ai_config"), dict) else {}
    custom_prompt = str(
        ai_config.get("greet_prompt")
        or ai_config.get("filter_prompt")
        or ai_config.get("click_prompt")
        or ""
    ).strip()
    job_desc = position_description(position)
    candidate_text = str(candidate.get("filter_text") or candidate.get("raw_text") or "").strip()
    default_prompt = DEFAULT_GREET_PROMPT.format(job_desc=job_desc, candidate_text=candidate_text)
    if not custom_prompt:
        return default_prompt
    return _template_prompt(custom_prompt, job_desc, candidate_text, default_prompt)


def parse_score_decision(content: str) -> dict[str, Any]:
    """
    解析 AI 返回的评分 JSON。

    Args:
        content: AI 原始正文。

    Returns:
        dict[str, Any]: 评分字典。
    """
    cleaned = clean_ai_text_output(content)
    candidates = [cleaned]
    match = re.search(r"\{.*\}", cleaned, re.S)
    if match:
        candidates.append(match.group(0))
    for item in candidates:
        try:
            data = json.loads(item)
        except Exception:
            continue
        if isinstance(data, dict):
            return data
    raise ValueError("AI 返回不是合法 JSON")


def greet_threshold(position: dict[str, Any]) -> float:
    """
    读取岗位打招呼阈值。

    Args:
        position: 岗位快照。

    Returns:
        float: 阈值。
    """
    return _ai_config_number(position, DEFAULT_GREET_THRESHOLD, "greet_score_threshold", "greet_threshold")


def position_description(position: dict[str, Any]) -> str:
    """
    读取岗位描述文本。

    Args:
        position: 岗位快照。

    Returns:
        str: 岗位描述。
    """
    ai_config = position.get("ai_config") if isinstance(position.get("ai_config"), dict) else {}
    requirement = str(ai_config.get("position_requirement") or "").strip()
    if requirement:
        return requirement
    parts = [
        str(position.get("name") or "").strip(),
        str(position.get("description") or "").strip(),
        "关键词：" + "、".join(_string_list(position.get("keywords"))),
        "排除词：" + "、".join(_string_list(position.get("exclude_keywords") or position.get("exclude"))),
    ]
    return "\n".join(part for part in parts if part and not part.endswith("："))


def clamp_score(value: Any) -> float:
    """
    将评分限制在 0 到 100。

    Args:
        value: 原始评分。

    Returns:
        float: 规范化评分。
    """
    try:
        score = float(value)
    except (TypeError, ValueError):
        score = 0.0
    return max(0.0, min(100.0, score))


def truncate_text(value: str, max_length: int) -> str:
    """
    截断文本到指定长度。

    Args:
        value: 原始文本。
        max_length: 最大长度。

    Returns:
        str: 截断后的文本。
    """
    text = str(value or "").strip()
    if len(text) <= max_length:
        return text
    return text[:max_length]


def _template_prompt(template: str, job_desc: str, candidate_text: str, fallback: str) -> str:
    """
    替换自定义提示词变量。

    Args:
        template: 自定义模板。
        job_desc: 岗位描述。
        candidate_text: 候选人文本。
        fallback: 默认提示词。

    Returns:
        str: 替换后的提示词。
    """
    prompt = str(template or "").strip()
    replacements = {
        "${岗位信息}": job_desc,
        "${候选人信息}": candidate_text,
        "{{岗位信息}}": job_desc,
        "{{候选人信息}}": candidate_text,
        "{job_desc}": job_desc,
        "{candidate_text}": candidate_text,
        "{default_prompt}": fallback,
    }
    for key, value in replacements.items():
        prompt = prompt.replace(key, value)
    if job_desc not in prompt or candidate_text not in prompt:
        prompt = f"{prompt}\n\n岗位要求：\n{job_desc}\n\n候选人信息：\n{candidate_text}"
    return prompt


def _runtime_ai_config(position: dict[str, Any]) -> dict[str, Any]:
    """
    读取岗位中的单次 AI 覆盖配置。

    Args:
        position: 岗位快照。

    Returns:
        dict[str, Any]: 单次覆盖配置。
    """
    ai_config = position.get("ai_config") if isinstance(position.get("ai_config"), dict) else {}
    config: dict[str, Any] = {}
    for key in ["model", "model_id", "temperature", "timeout"]:
        if ai_config.get(key) not in (None, ""):
            config[key] = ai_config.get(key)
    return config


def _ai_config_number(position: dict[str, Any], default: float, *keys: str) -> float:
    """
    从岗位 AI 配置中读取数字。

    Args:
        position: 岗位快照。
        default: 默认值。
        keys: 字段名列表。

    Returns:
        float: 数字值。
    """
    ai_config = position.get("ai_config") if isinstance(position.get("ai_config"), dict) else {}
    for key in keys:
        try:
            value = float(ai_config.get(key))
        except (TypeError, ValueError):
            continue
        return value
    return default


def _string_list(value: Any) -> list[str]:
    """
    将配置值转换为字符串列表。

    Args:
        value: 原始配置值。

    Returns:
        list[str]: 字符串列表。
    """
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    if isinstance(value, str):
        return [item.strip() for item in value.replace(",", " ").split() if item.strip()]
    return []
