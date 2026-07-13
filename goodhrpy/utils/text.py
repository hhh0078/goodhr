"""
GoodHR 自动化工具 - 文本处理工具模块

提供文本清洗、关键词匹配等通用文本处理功能。
"""

import re
from typing import List


def clean_text(text: str) -> str:
    """
    清洗文本：去除多余空白、特殊字符

    Args:
        text: 原始文本

    Returns:
        清洗后的文本
    """
    text = re.sub(r"\s+", " ", text).strip()
    text = re.sub(r"[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]", "", text)
    return text


def extract_json_from_text(text: str) -> str:
    """
    从文本中提取第一个 JSON 对象

    AI 返回的内容可能包含在 Markdown 代码块或多余文字中，
    此方法提取第一个 { ... } 结构。

    Args:
        text: 包含 JSON 的原始文本

    Returns:
        提取出的 JSON 字符串，未找到则返回空字符串
    """
    match = re.search(r"\{[\s\S]*\}", text)
    return match.group(0) if match else ""


def match_keywords(text: str, keywords: List[str], is_and_mode: bool = False) -> dict:
    """
    在文本中进行关键词匹配

    Args:
        text: 待匹配文本
        keywords: 关键词列表
        is_and_mode: True 为与模式（全部匹配），False 为或模式（任一匹配）

    Returns:
        {"matched": bool, "reason": str, "matched_keywords": List[str]}
    """
    text_lower = text.lower()
    matched_kw = [kw for kw in keywords if kw and kw.lower() in text_lower]
    not_matched_kw = [kw for kw in keywords if kw and kw.lower() not in text_lower]

    if is_and_mode:
        if not_matched_kw:
            return {"matched": False, "reason": f"与模式缺少关键词\"{not_matched_kw[0]}\"", "matched_keywords": matched_kw}
        return {"matched": True, "reason": "与模式全部匹配", "matched_keywords": matched_kw}
    else:
        if matched_kw:
            return {"matched": True, "reason": f"或模式匹配\"{matched_kw[0]}\"", "matched_keywords": matched_kw}
        return {"matched": False, "reason": "或模式无关键词匹配", "matched_keywords": []}


def exclude_keywords(text: str, exclude_words: List[str]) -> dict:
    """
    检查文本中是否包含排除词

    Args:
        text: 待检查文本
        exclude_words: 排除词列表

    Returns:
        {"excluded": bool, "reason": str, "excluded_word": str}
    """
    text_lower = text.lower()
    for word in exclude_words:
        if word and word.lower() in text_lower:
            return {"excluded": True, "reason": f"包含排除词\"{word}\"", "excluded_word": word}
    return {"excluded": False, "reason": "", "excluded_word": ""}
