"""本文件负责调用多模态 AI，从详情页截图中直接提取文字并完成分析。"""

from __future__ import annotations

import base64
import re
import time
from typing import Any

import httpx


def clean_ai_text_output(text: str) -> str:
    """
    清理 AI 输出中的思考标签和单层 Markdown 代码块。

    Args:
        text: 模型原始输出。

    Returns:
        str: 可用于后端解析的正文。
    """
    return strip_markdown_code_fence(strip_think_tags(text)).strip()


def strip_think_tags(text: str) -> str:
    """
    删除模型输出中的 <think> 思考内容。

    Args:
        text: 模型原始输出。

    Returns:
        str: 删除思考标签后的正文。
    """
    return re.sub(r"(?is)<think>.*?</think>", "", str(text or "")).strip()


def strip_markdown_code_fence(text: str) -> str:
    """
    删除模型输出外层的 Markdown 代码块。

    Args:
        text: 模型原始输出。

    Returns:
        str: 去掉外层代码块后的正文。
    """
    cleaned = str(text or "").strip()
    if not cleaned.startswith("```"):
        return cleaned
    lines = cleaned.splitlines()
    if len(lines) < 2:
        return cleaned
    if not lines[0].strip().startswith("```") or lines[-1].strip() != "```":
        return cleaned
    return "\n".join(lines[1:-1]).strip()


def build_minimax_vision_content(prompt: str, image_bytes: bytes, image_format: str = "png") -> list[dict[str, Any]]:
    """
    构建 OpenAI 兼容的 MiniMax 多模态图片消息内容。

    Args:
        prompt: 云端传入的业务提示词。
        image_bytes: 图片字节。
        image_format: 图片格式，如 png、jpeg、webp。

    Returns:
        list[dict[str, Any]]: 多模态 content 数组。
    """
    image_b64 = base64.b64encode(image_bytes).decode("ascii")
    normalized_format = str(image_format or "png").strip().lower()
    if normalized_format == "jpg":
        normalized_format = "jpeg"
    if normalized_format not in {"png", "jpeg", "webp"}:
        normalized_format = "png"
    return [
        {"type": "text", "text": str(prompt or "").strip()},
        {
            "type": "image_url",
            "image_url": {
                "url": f"data:image/{normalized_format};base64,{image_b64}",
            },
        },
    ]


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
                return clean_ai_text_output(content)
            if isinstance(content, list):
                parts = []
                for item in content:
                    if isinstance(item, dict) and isinstance(item.get("text"), str):
                        parts.append(item["text"])
                    elif isinstance(item, str):
                        parts.append(item)
                return clean_ai_text_output("\n".join(parts))
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
        raise ValueError("图片识别 AI 接口地址不能为空")
    if not api_key:
        raise ValueError("图片识别 AI 密钥不能为空")
    if not model_id:
        raise ValueError("图片识别 AI 模型名称不能为空")
    if not prompt:
        raise ValueError("图片识别提示词不能为空")

    content = build_minimax_vision_content(prompt, image_bytes, str(config.get("image_format") or "png"))
    request_body = {
        "model": model_id,
        "messages": [{"role": "user", "content": content}],
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
