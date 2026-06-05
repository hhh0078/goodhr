"""本文件负责管理本地 AI 配置并提供统一 AI 调用入口。"""

from __future__ import annotations

import json
import time
from datetime import datetime, timezone
from typing import Any

import httpx

from app.local_db import connect
from app.vision_ai import clean_ai_text_output, extract_chat_content


DEFAULT_AI_CONFIG_ID = "default"


def get_local_ai_config() -> dict[str, Any]:
    """
    读取本地默认 AI 配置。

    Returns:
        dict[str, Any]: AI 配置，未配置时返回空配置。
    """
    with connect() as conn:
        row = conn.execute("SELECT * FROM local_ai_config WHERE id=?", (DEFAULT_AI_CONFIG_ID,)).fetchone()
    if row is None:
        return _empty_config()
    return _config_row_to_dict(row)


def save_local_ai_config(payload: dict[str, Any]) -> dict[str, Any]:
    """
    保存本地默认 AI 配置。

    Args:
        payload: AI 配置参数。

    Returns:
        dict[str, Any]: 保存后的 AI 配置。
    """
    current = get_local_ai_config()
    now = _now_iso()
    config = {
        "id": DEFAULT_AI_CONFIG_ID,
        "provider": str(payload.get("provider", current.get("provider", "")) or "").strip(),
        "base_url": str(
            payload.get("base_url", payload.get("api_url", current.get("base_url", ""))) or ""
        ).strip(),
        "api_key": str(payload.get("api_key", current.get("api_key", "")) or "").strip(),
        "model": str(payload.get("model", payload.get("model_id", current.get("model", ""))) or "").strip(),
        "temperature": _safe_float(payload.get("temperature", current.get("temperature", 0.2)), 0.2),
        "timeout": max(1, _safe_int(payload.get("timeout", current.get("timeout", 120)), 120)),
        "extra": _safe_dict(payload.get("extra", payload.get("extra_body", current.get("extra", {})))),
        "created_at": current.get("created_at") or now,
        "updated_at": now,
    }
    with connect() as conn:
        conn.execute(
            """
            INSERT INTO local_ai_config (
                id, provider, base_url, api_key, model, temperature, timeout, extra_json, created_at, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(id) DO UPDATE SET
                provider=excluded.provider,
                base_url=excluded.base_url,
                api_key=excluded.api_key,
                model=excluded.model,
                temperature=excluded.temperature,
                timeout=excluded.timeout,
                extra_json=excluded.extra_json,
                updated_at=excluded.updated_at
            """,
            (
                config["id"],
                config["provider"],
                config["base_url"],
                config["api_key"],
                config["model"],
                config["temperature"],
                config["timeout"],
                json.dumps(config["extra"], ensure_ascii=False),
                config["created_at"],
                config["updated_at"],
            ),
        )
    return get_local_ai_config()


async def chat_with_local_ai(payload: dict[str, Any]) -> dict[str, Any]:
    """
    使用本地保存的 AI 配置调用 OpenAI 兼容聊天接口。

    Args:
        payload: 聊天请求参数，包含 messages 以及可选覆盖配置。

    Returns:
        dict[str, Any]: AI 回复、模型、耗时和用量信息。
    """
    saved_config = get_local_ai_config()
    override_config = payload.get("config") if isinstance(payload.get("config"), dict) else {}
    config = _merge_runtime_config(saved_config, override_config)
    messages = payload.get("messages")
    if not isinstance(messages, list) or not messages:
        prompt = str(payload.get("prompt") or "").strip()
        if prompt:
            messages = [{"role": "user", "content": prompt}]
    if not isinstance(messages, list) or not messages:
        raise ValueError("AI 请求内容不能为空")

    api_url = _chat_completions_url(str(config.get("base_url") or ""))
    api_key = str(config.get("api_key") or "").strip()
    model = str(payload.get("model") or config.get("model") or "").strip()
    if not api_url:
        raise ValueError("请先在个人配置里填写本地 AI 接口地址")
    if not api_key:
        raise ValueError("请先在个人配置里填写本地 AI 密钥")
    if not model:
        raise ValueError("请先在个人配置里填写本地 AI 模型名称")

    request_body = _build_chat_body(payload, config, messages, model)
    start = time.perf_counter()
    async with httpx.AsyncClient(timeout=float(config.get("timeout", 120) or 120)) as client:
        response = await client.post(
            api_url,
            headers={
                "Authorization": "Bearer " + api_key,
                "Content-Type": "application/json",
            },
            json=request_body,
        )
    elapsed_ms = int((time.perf_counter() - start) * 1000)
    response_json = _response_json(response)
    if response.status_code >= 400:
        preview = response.text[:500].replace("\n", " ")
        raise RuntimeError(f"AI 服务请求失败，状态码 {response.status_code}，响应 {preview}")
    content = extract_chat_content(response_json)
    if not content:
        content = clean_ai_text_output(str(response_json.get("content") or ""))
    return {
        "content": content,
        "model": response_json.get("model") or model,
        "usage": response_json.get("usage") if isinstance(response_json.get("usage"), dict) else {},
        "elapsed_ms": elapsed_ms,
        "raw": response_json,
    }


def _build_chat_body(
    payload: dict[str, Any],
    config: dict[str, Any],
    messages: list[Any],
    model: str,
) -> dict[str, Any]:
    """
    构建 OpenAI 兼容聊天请求体。

    Args:
        payload: 原始请求参数。
        config: 本地 AI 配置。
        messages: 消息列表。
        model: 模型名称。

    Returns:
        dict[str, Any]: 请求体。
    """
    extra = _safe_dict(config.get("extra"))
    body = {
        "model": model,
        "messages": messages,
        "temperature": _safe_float(payload.get("temperature", config.get("temperature", 0.2)), 0.2),
    }
    max_tokens = payload.get("max_tokens")
    if max_tokens is not None:
        body["max_tokens"] = max(1, _safe_int(max_tokens, 0))
    body.update(extra)
    return body


def _merge_runtime_config(saved_config: dict[str, Any], override: dict[str, Any]) -> dict[str, Any]:
    """
    合并本地保存配置和单次请求覆盖配置。

    Args:
        saved_config: 本地保存的 AI 配置。
        override: 单次请求覆盖配置。

    Returns:
        dict[str, Any]: 合并后的配置。
    """
    merged = dict(saved_config or {})
    keys = ["provider", "base_url", "api_url", "api_key", "model", "model_id", "temperature", "timeout"]
    keys.extend(["extra", "extra_body"])
    for key in keys:
        if key in override and override.get(key) not in (None, ""):
            merged[key] = override.get(key)
    if merged.get("api_url") and not merged.get("base_url"):
        merged["base_url"] = merged.get("api_url")
    if merged.get("model_id") and not merged.get("model"):
        merged["model"] = merged.get("model_id")
    if merged.get("extra_body") and not merged.get("extra"):
        merged["extra"] = merged.get("extra_body")
    return merged


def _chat_completions_url(base_url: str) -> str:
    """
    生成 OpenAI 兼容 chat/completions 地址。

    Args:
        base_url: 用户填写的基础地址或完整接口地址。

    Returns:
        str: 完整聊天接口地址。
    """
    value = str(base_url or "").strip().rstrip("/")
    if not value:
        return ""
    if value.endswith("/chat/completions"):
        return value
    if value.endswith("/v1"):
        return value + "/chat/completions"
    return value + "/v1/chat/completions"


def _response_json(response: httpx.Response) -> dict[str, Any]:
    """
    安全解析 HTTP JSON 响应。

    Args:
        response: HTTP 响应对象。

    Returns:
        dict[str, Any]: JSON 字典。
    """
    try:
        data = response.json()
    except Exception:
        data = {}
    return data if isinstance(data, dict) else {}


def _config_row_to_dict(row) -> dict[str, Any]:
    """
    将 SQLite 配置行转换为字典。

    Args:
        row: SQLite 查询行。

    Returns:
        dict[str, Any]: AI 配置。
    """
    return {
        "id": str(row["id"] or DEFAULT_AI_CONFIG_ID),
        "provider": str(row["provider"] or ""),
        "base_url": str(row["base_url"] or ""),
        "api_key": str(row["api_key"] or ""),
        "model": str(row["model"] or ""),
        "temperature": _safe_float(row["temperature"], 0.2),
        "timeout": _safe_int(row["timeout"], 120),
        "extra": _safe_json_dict(row["extra_json"]),
        "created_at": str(row["created_at"] or ""),
        "updated_at": str(row["updated_at"] or ""),
    }


def _empty_config() -> dict[str, Any]:
    """
    返回空 AI 配置。

    Returns:
        dict[str, Any]: 空配置。
    """
    return {
        "id": DEFAULT_AI_CONFIG_ID,
        "provider": "",
        "base_url": "",
        "api_key": "",
        "model": "",
        "temperature": 0.2,
        "timeout": 120,
        "extra": {},
        "created_at": "",
        "updated_at": "",
    }


def _safe_json_dict(value: Any) -> dict[str, Any]:
    """
    将 JSON 字符串安全转换为字典。

    Args:
        value: JSON 字符串或字典。

    Returns:
        dict[str, Any]: 字典。
    """
    if isinstance(value, dict):
        return value
    try:
        data = json.loads(str(value or "{}"))
    except Exception:
        data = {}
    return data if isinstance(data, dict) else {}


def _safe_dict(value: Any) -> dict[str, Any]:
    """
    将任意值安全转换为字典。

    Args:
        value: 原始值。

    Returns:
        dict[str, Any]: 字典。
    """
    if isinstance(value, dict):
        return value
    return _safe_json_dict(value)


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


def _safe_float(value: Any, default: float) -> float:
    """
    将任意值安全转换为浮点数。

    Args:
        value: 原始值。
        default: 默认值。

    Returns:
        float: 浮点数。
    """
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def _now_iso() -> str:
    """
    返回当前 UTC 时间字符串。

    Returns:
        str: ISO 格式时间。
    """
    return datetime.now(timezone.utc).isoformat()
