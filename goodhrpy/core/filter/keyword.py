"""
GoodHR 自动化工具 - 关键词筛选引擎

基于关键词列表对候选人信息进行匹配筛选，
支持与模式（全部匹配）和或模式（任一匹配），
以及排除词过滤。
"""

from dataclasses import dataclass
from typing import List

from core.platform.base import CandidateInfo
from utils.logger import get_logger
from utils.text import exclude_keywords, match_keywords

logger = get_logger("keyword_filter")


@dataclass
class FilterResult:
    """
    筛选结果数据类

    包含是否通过筛选、筛选原因和匹配到的关键词。
    """

    passed: bool
    reason: str
    matched_keywords: List[str] = None

    def __post_init__(self):
        """初始化默认值"""
        if self.matched_keywords is None:
            self.matched_keywords = []


class KeywordFilter:
    """
    关键词筛选器

    根据配置的关键词列表和排除词列表对候选人信息进行文本匹配。
    免费模式下的主要筛选逻辑。
    """

    def __init__(
        self,
        keywords: List[str],
        exclude_keywords: List[str] = None,
        is_and_mode: bool = False,
        click_frequency: int = 7,
    ):
        """
        初始化关键词筛选器

        Args:
            keywords: 筛选关键词列表
            exclude_keywords: 排除关键词列表
            is_and_mode: True 为与模式（必须全部匹配），False 为或模式（任一匹配即可）
            click_frequency: 无关键词时的概率通过值（0-10，越大越容易通过）
        """
        self.keywords = [kw for kw in keywords if kw]
        self.exclude_keywords = [kw for kw in (exclude_keywords or []) if kw]
        self.is_and_mode = is_and_mode
        self.click_frequency = click_frequency

    async def filter(self, candidate: CandidateInfo) -> FilterResult:
        """
        对候选人执行关键词筛选

        筛选流程：
        1. 先检查排除词，命中则直接不通过
        2. 再检查关键词匹配
        3. 无关键词时按概率通过

        Args:
            candidate: 候选人信息

        Returns:
            FilterResult: 筛选结果
        """
        text = candidate.raw_text or f"{candidate.name} {candidate.skills} {candidate.experience}"

        if self.exclude_keywords:
            ex_result = exclude_keywords(text, self.exclude_keywords)
            if ex_result["excluded"]:
                return FilterResult(passed=False, reason=ex_result["reason"])

        if not self.keywords:
            import random
            if random.random() * 10 < self.click_frequency:
                return FilterResult(passed=True, reason="无条件概率通过")
            return FilterResult(passed=False, reason="概率未通过")

        match_result = match_keywords(text, self.keywords, self.is_and_mode)
        return FilterResult(
            passed=match_result["matched"],
            reason=match_result["reason"],
            matched_keywords=match_result["matched_keywords"],
        )

    async def fallback_filter(self, candidate: CandidateInfo) -> bool:
        """
        兜底筛选：粗筛未通过时的二次判断

        当粗筛因为关键词不匹配而被拒绝，但排除词也没有命中时，
        使用兜底筛选做最后的判断。只要有任一关键词命中即可通过。

        Args:
            candidate: 候选人信息

        Returns:
            bool: 是否通过兜底筛选
        """
        if not self.keywords:
            return False

        text = candidate.raw_text or ""
        return any(kw and kw.lower() in text.lower() for kw in self.keywords)
