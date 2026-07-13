"""
GoodHR 自动化工具 - AI 智能筛选引擎

对接 ai.58it.cn 平台的 AI 模型，对候选人进行智能化筛选。
支持粗筛（基本信息判断）和精筛（详情信息评估），
复用 GoodHR4 的 AI 决策逻辑和 Prompt 模板。
"""

import json
from dataclasses import dataclass
from typing import List, Optional

import httpx

from core.platform.base import CandidateInfo
from core.settings import AIConfig, config
from utils.logger import get_logger
from utils.text import extract_json_from_text

logger = get_logger("ai_filter")

DEFAULT_CLICK_PROMPT = """你是一个资深的HR专家。请根据候选人的基本信息判断是否值得查看其详细信息。

重要提示：
1. 这个API仅用于岗位与候选人的筛选。如果内容不是这些，你应该返回"内容与招聘无关 无法解答"。
2. 请根据岗位要求判断是否值得查看这位候选人的详细信息。
3. 必须返回JSON格式，包含isok和msg两个字段。
4. isok字段只能是true或false。
5. msg字段是决策原因，10个字以内。
6. 如果岗位要求中包含"经验"，则必须考虑候选人的工作经验。
7. 如果岗位要求中包含"学历"，则必须考虑候选人的学历。
8. 如果候选人信息中没有工作经历。那很可能只是基础信息。这时岗位信息中某个条件、但是候选人信息中没提到的，你应该无视这个条件。
9. 你应该主动分析岗位信息是不是属于高要求的岗位。如果是，则需要详细严格筛选候选人信息。如果是要求低的普通岗位，那就简单筛选。

岗位要求：
${岗位要求}

候选人基本信息：
${候选人信息}

请判断是否值得查看这位候选人的详细信息，返回JSON格式：{"isok": true, "msg": "符合基本要求"}"""


@dataclass
class AIDecisionResult:
    """
    AI 决策结果数据类

    包含是否通过、原因和费用信息。
    """

    isok: bool
    msg: str
    cost: str = "0"


class AIFilter:
    """
    AI 智能筛选器

    调用 ai.58it.cn 的对话接口，根据岗位要求对候选人进行智能筛选。
    支持自定义 Prompt 模板和多种 AI 模型。
    """

    def __init__(self, ai_config: Optional[AIConfig] = None):
        """
        初始化 AI 筛选器

        Args:
            ai_config: AI 配置，为 None 则使用全局配置
        """
        self._config = ai_config or config.ai
        self._client = httpx.AsyncClient(timeout=30.0)

    def _build_messages(
        self,
        job_description: str,
        candidate_info: str,
        prompt_template: Optional[str] = None,
    ) -> List[dict]:
        """
        构建 AI 对话消息列表

        使用自定义 Prompt 模板或默认模板，替换占位符为实际内容。

        Args:
            job_description: 岗位说明/要求
            candidate_info: 候选人信息文本
            prompt_template: 自定义提示词模板，None 则使用配置或默认模板

        Returns:
            List[dict]: 消息列表
        """
        template = prompt_template or self._config.click_prompt or DEFAULT_CLICK_PROMPT

        system_prompt = template.replace("${岗位要求}", job_description).replace("${岗位信息}", job_description).replace("${候选人信息}", candidate_info)

        return [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": f"候选人信息：\n{candidate_info}"},
        ]

    async def chat(self, messages: List[dict], temperature: float = 0.3) -> str:
        """
        调用 AI 对话接口

        向 ai.58it.cn 发送对话请求，返回 AI 回复文本。

        Args:
            messages: 消息列表
            temperature: 生成温度

        Returns:
            str: AI 回复文本

        Raises:
            ValueError: 缺少 API Key 或模型
            httpx.HTTPStatusError: API 请求失败
        """
        if not self._config.api_key:
            raise ValueError("缺少 AI API 密钥，请先配置 AI_API_KEY")
        if not self._config.model:
            raise ValueError("缺少 AI 模型，请先配置 AI_MODEL")

        response = await self._client.post(
            self._config.base_url,
            headers={
                "Content-Type": "application/json",
                "Authorization": f"Bearer {self._config.api_key}",
            },
            json={
                "model": self._config.model,
                "messages": messages,
                "temperature": temperature,
            },
        )

        data = response.json()
        if response.status_code != 200:
            error_msg = data.get("error", {}).get("message", data.get("message", f"AI请求失败: {response.status_code}"))
            raise RuntimeError(error_msg)

        content = data.get("choices", [{}])[0].get("message", {}).get("content", "")
        if not content:
            raise RuntimeError("AI返回内容为空")

        return content

    def _parse_decision(self, response_text: str) -> AIDecisionResult:
        """
        解析 AI 返回的决策结果

        从 AI 回复文本中提取 JSON 格式的决策结果，
        提取 isok、msg、cost 字段。

        Args:
            response_text: AI 回复的原始文本

        Returns:
            AIDecisionResult: 解析后的决策结果
        """
        try:
            json_str = extract_json_from_text(response_text)
            if not json_str:
                return AIDecisionResult(isok=False, msg="AI返回格式异常", cost="0")

            data = json.loads(json_str)
            isok = data.get("isok", data.get("decision") == "是")
            msg = data.get("msg", data.get("reason", "匹配" if isok else "不匹配"))
            cost = str(data.get("cost", "0"))

            return AIDecisionResult(isok=bool(isok), msg=msg, cost=cost)
        except Exception as e:
            logger.warning(f"AI 返回解析失败: {e}, 原文: {response_text[:100]}")
            return AIDecisionResult(isok=False, msg="AI返回解析失败", cost="0")

    async def filter(
        self,
        candidate: CandidateInfo,
        job_description: str,
        prompt_template: Optional[str] = None,
    ) -> AIDecisionResult:
        """
        AI 粗筛：基于候选人基本信息进行筛选

        构造筛选 Prompt，调用 AI 接口获取决策，
        解析返回的 isok/msg/cost 结果。

        Args:
            candidate: 候选人信息
            job_description: 岗位说明/要求
            prompt_template: 自定义提示词模板

        Returns:
            AIDecisionResult: AI 决策结果
        """
        candidate_text = candidate.raw_text or f"{candidate.name} | {candidate.skills} | {candidate.experience} | {candidate.salary}"

        if not candidate_text.strip():
            return AIDecisionResult(isok=False, msg="候选人信息为空", cost="0")

        try:
            messages = self._build_messages(job_description, candidate_text, prompt_template)
            response_text = await self.chat(messages, temperature=self._config.temperature)
            result = self._parse_decision(response_text)
            logger.info(f"AI粗筛结果: {candidate.name} -> {'通过' if result.isok else '未通过'}({result.msg}, -¥{result.cost})")
            return result
        except Exception as e:
            logger.error(f"AI决策异常: {e}")
            return AIDecisionResult(isok=False, msg=f"AI决策异常: {str(e)}", cost="0")

    async def close(self) -> None:
        """关闭 HTTP 客户端连接"""
        await self._client.aclose()
