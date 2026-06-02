"""本文件负责调用多模态 AI，从详情页截图中直接提取文字并完成分析。"""

from __future__ import annotations

import base64
import re
import time
from typing import Any

import httpx


def strip_think_tags(text: str) -> str:
    """
    删除模型输出中的 <think> 思考内容。

    Args:
        text: 模型原始输出。

    Returns:
        str: 删除思考标签后的正文。
    """
    return re.sub(r"(?is)<think>.*?</think>", "", str(text or "")).strip()


def build_minimax_vision_prompt(prompt: str, image_bytes: bytes) -> str:
    """
    构建 MiniMax 多模态图片理解提示词。

    Args:
        prompt: 云端传入的业务提示词。
        image_bytes: PNG 图片字节。

    Returns:
        str: 包含图片 base64 的完整提示词。
    """
    image_b64 = base64.b64encode(image_bytes).decode("ascii")
    return (
        str(prompt or "").strip()
        + "\n\n请阅读下面这张图片并按要求输出。\n"
        + f"[Image base64:{image_b64}]"
    )


def extract_chat_content(response_json: dict[str, Any]) -> str:
    """
    从 OpenAI 兼容响应中提取模型正文。

    Args:
        response_json: AI 接口返回 JSON。

    Returns:
        str: 模型回复正文。
    """
    choices = response_json.get("choices")
    if isinstance(choices, list) and choices:
        message = choices[0].get("message") if isinstance(choices[0], dict) else None
        if isinstance(message, dict):
            content = message.get("content")
            if isinstance(content, str):
                return strip_think_tags(content)
            if isinstance(content, list):
                parts = []
                for item in content:
                    if isinstance(item, dict) and isinstance(item.get("text"), str):
                        parts.append(item["text"])
                    elif isinstance(item, str):
                        parts.append(item)
                return strip_think_tags("\n".join(parts))
    return ""


async def analyze_image_with_ai(config: dict[str, Any], image_bytes: bytes) -> tuple[str, dict[str, Any]]:
    """
    调用多模态 AI 分析图片。

    Args:
        config: 包含 api_url、api_key、model_id、prompt 的配置。
        image_bytes: PNG 图片字节。

    Returns:
        tuple[str, dict[str, Any]]: AI 正文和调试信息。
    """
    api_url = str(config.get("api_url") or "").strip()
    api_key = str(config.get("api_key") or "").strip()
    model_id = str(config.get("model_id") or config.get("model") or "").strip()
    prompt = str(config.get("prompt") or "").strip()
    if not api_url:
        raise ValueError("ai_vision.api_url is required")
    if not api_key:
        raise ValueError("ai_vision.api_key is required")
    if not model_id:
        raise ValueError("ai_vision.model_id is required")
    if not prompt:
        raise ValueError("ai_vision.prompt is required")

    full_prompt = build_minimax_vision_prompt(prompt, image_bytes)
    request_body = {
        "model": model_id,
        "messages": [{"role": "user", "content": full_prompt}],
        "temperature": float(config.get("temperature", 0.1) or 0.1),
        "reasoning_split": True,
    }
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
    if response.status_code >= 400:
        preview = response.text[:500].replace("\n", " ")
        raise RuntimeError(f"AI 图片识别请求失败 status={response.status_code} body={preview}")

    response_json = response.json()
    content = extract_chat_content(response_json)
    usage = response_json.get("usage") if isinstance(response_json, dict) else None
    meta = {
        "elapsed_ms": elapsed_ms,
        "model": model_id,
        "content_length": len(content),
        "usage": usage if isinstance(usage, dict) else {},
    }
    return content, meta
