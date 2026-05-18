"""
GoodHR 自动化工具 - 系统配置 API

提供系统配置的读取和更新接口，
包括 AI 配置、浏览器配置、任务配置等。
"""

from typing import Optional

from fastapi import APIRouter
from pydantic import BaseModel, Field

from core.settings import config
from utils.logger import get_logger

logger = get_logger("config_api")
router = APIRouter()


class AIConfigResponse(BaseModel):
    """AI 配置响应模型"""

    model: str
    base_url: str
    has_api_key: bool
    click_prompt: str
    temperature: float


class AIConfigUpdate(BaseModel):
    """AI 配置更新模型"""

    api_key: Optional[str] = Field(default=None, description="API 密钥")
    model: Optional[str] = Field(default=None, description="模型名称")
    click_prompt: Optional[str] = Field(default=None, description="粗筛提示词")
    temperature: Optional[float] = Field(default=None, description="生成温度")


class BrowserConfigResponse(BaseModel):
    """浏览器配置响应模型"""

    headless: bool
    humanize: bool
    human_preset: str
    has_proxy: bool


class TaskConfigResponse(BaseModel):
    """任务配置响应模型"""

    match_limit: int
    scroll_delay_min: int
    scroll_delay_max: int
    list_view_delay_min: float
    list_view_delay_max: float
    detail_view_delay_min: float
    detail_view_delay_max: float
    greet_delay_min: float
    greet_delay_max: float
    rest_after_candidates_min: int
    rest_after_candidates_max: int
    rest_times_min: int
    rest_times_max: int
    rest_duration_min: float
    rest_duration_max: float
    keyword_detail_open_probability: int
    click_frequency: int
    detail_mode: str


class SystemConfigResponse(BaseModel):
    """系统总配置响应模型"""

    ai: AIConfigResponse
    browser: BrowserConfigResponse
    task: TaskConfigResponse


@router.get("", response_model=SystemConfigResponse)
async def get_config():
    """获取系统配置"""
    return SystemConfigResponse(
        ai=AIConfigResponse(
            model=config.ai.model,
            base_url=config.ai.base_url,
            has_api_key=bool(config.ai.api_key),
            click_prompt=config.ai.click_prompt,
            temperature=config.ai.temperature,
        ),
        browser=BrowserConfigResponse(
            headless=config.browser.headless,
            humanize=config.browser.humanize,
            human_preset=config.browser.human_preset,
            has_proxy=bool(config.browser.proxy),
        ),
        task=TaskConfigResponse(
            match_limit=config.task.match_limit,
            scroll_delay_min=config.task.scroll_delay_min,
            scroll_delay_max=config.task.scroll_delay_max,
            list_view_delay_min=config.task.list_view_delay_min,
            list_view_delay_max=config.task.list_view_delay_max,
            detail_view_delay_min=config.task.detail_view_delay_min,
            detail_view_delay_max=config.task.detail_view_delay_max,
            greet_delay_min=config.task.greet_delay_min,
            greet_delay_max=config.task.greet_delay_max,
            rest_after_candidates_min=config.task.rest_after_candidates_min,
            rest_after_candidates_max=config.task.rest_after_candidates_max,
            rest_times_min=config.task.rest_times_min,
            rest_times_max=config.task.rest_times_max,
            rest_duration_min=config.task.rest_duration_min,
            rest_duration_max=config.task.rest_duration_max,
            keyword_detail_open_probability=config.task.keyword_detail_open_probability,
            click_frequency=config.task.click_frequency,
            detail_mode=config.task.detail_mode,
        ),
    )


@router.put("/ai", response_model=AIConfigResponse)
async def update_ai_config(data: AIConfigUpdate):
    """
    更新 AI 配置

    运行时修改 AI 相关配置，修改后立即生效。
    注意：此修改仅在运行时生效，重启后恢复为配置文件和环境变量的值。
    """
    if data.api_key is not None:
        config.ai.api_key = data.api_key
    if data.model is not None:
        config.ai.model = data.model
    if data.click_prompt is not None:
        config.ai.click_prompt = data.click_prompt
    if data.temperature is not None:
        config.ai.temperature = data.temperature

    logger.info("AI 配置已更新")

    return AIConfigResponse(
        model=config.ai.model,
        base_url=config.ai.base_url,
        has_api_key=bool(config.ai.api_key),
        click_prompt=config.ai.click_prompt,
        temperature=config.ai.temperature,
    )


class TaskConfigUpdate(BaseModel):
    """任务配置更新模型"""

    match_limit: Optional[int] = Field(default=None, description="匹配上限")
    scroll_delay_min: Optional[int] = Field(default=None, description="滚动最小延迟")
    scroll_delay_max: Optional[int] = Field(default=None, description="滚动最大延迟")
    list_view_delay_min: Optional[float] = Field(default=None, description="候选人列表查看最小延迟")
    list_view_delay_max: Optional[float] = Field(default=None, description="候选人列表查看最大延迟")
    detail_view_delay_min: Optional[float] = Field(default=None, description="详情弹框打开后最小延迟")
    detail_view_delay_max: Optional[float] = Field(default=None, description="详情弹框打开后最大延迟")
    greet_delay_min: Optional[float] = Field(default=None, description="打招呼前最小延迟")
    greet_delay_max: Optional[float] = Field(default=None, description="打招呼前最大延迟")
    rest_after_candidates_min: Optional[int] = Field(default=None, description="处理多少个候选人后休息：最小值")
    rest_after_candidates_max: Optional[int] = Field(default=None, description="处理多少个候选人后休息：最大值")
    rest_times_min: Optional[int] = Field(default=None, description="单次任务随机休息次数：最小值")
    rest_times_max: Optional[int] = Field(default=None, description="单次任务随机休息次数：最大值")
    rest_duration_min: Optional[float] = Field(default=None, description="每次随机休息最短分钟数")
    rest_duration_max: Optional[float] = Field(default=None, description="每次随机休息最长分钟数")
    keyword_detail_open_probability: Optional[int] = Field(default=None, description="关键词模式打开详情概率")
    click_frequency: Optional[int] = Field(default=None, description="点击概率")
    detail_mode: Optional[str] = Field(default=None, description="详情获取模式: dom/ocr")


@router.put("/task", response_model=TaskConfigResponse)
async def update_task_config(data: TaskConfigUpdate):
    """
    更新任务配置

    运行时修改任务相关配置，修改后立即生效。
    detail_mode 可选值：dom（DOM选择器读取）、ocr（截图OCR识别）。
    """
    if data.match_limit is not None:
        config.task.match_limit = data.match_limit
    if data.scroll_delay_min is not None:
        config.task.scroll_delay_min = data.scroll_delay_min
    if data.scroll_delay_max is not None:
        config.task.scroll_delay_max = data.scroll_delay_max
    if data.list_view_delay_min is not None:
        config.task.list_view_delay_min = data.list_view_delay_min
    if data.list_view_delay_max is not None:
        config.task.list_view_delay_max = data.list_view_delay_max
    if data.detail_view_delay_min is not None:
        config.task.detail_view_delay_min = data.detail_view_delay_min
    if data.detail_view_delay_max is not None:
        config.task.detail_view_delay_max = data.detail_view_delay_max
    if data.greet_delay_min is not None:
        config.task.greet_delay_min = data.greet_delay_min
    if data.greet_delay_max is not None:
        config.task.greet_delay_max = data.greet_delay_max
    if data.rest_after_candidates_min is not None:
        config.task.rest_after_candidates_min = max(data.rest_after_candidates_min, 0)
    if data.rest_after_candidates_max is not None:
        config.task.rest_after_candidates_max = max(data.rest_after_candidates_max, 0)
    if data.rest_times_min is not None:
        config.task.rest_times_min = max(data.rest_times_min, 0)
    if data.rest_times_max is not None:
        config.task.rest_times_max = max(data.rest_times_max, 0)
    if data.rest_duration_min is not None:
        config.task.rest_duration_min = max(data.rest_duration_min, 0)
    if data.rest_duration_max is not None:
        config.task.rest_duration_max = max(data.rest_duration_max, 0)
    if data.keyword_detail_open_probability is not None:
        config.task.keyword_detail_open_probability = min(max(data.keyword_detail_open_probability, 0), 100)
    if data.click_frequency is not None:
        config.task.click_frequency = data.click_frequency
    if data.detail_mode is not None:
        if data.detail_mode not in ("dom", "ocr"):
            from fastapi import HTTPException
            raise HTTPException(status_code=400, detail="detail_mode 只支持 dom 或 ocr")
        config.task.detail_mode = data.detail_mode

    logger.info(f"任务配置已更新，detail_mode={config.task.detail_mode}")

    return TaskConfigResponse(
        match_limit=config.task.match_limit,
        scroll_delay_min=config.task.scroll_delay_min,
        scroll_delay_max=config.task.scroll_delay_max,
        list_view_delay_min=config.task.list_view_delay_min,
        list_view_delay_max=config.task.list_view_delay_max,
        detail_view_delay_min=config.task.detail_view_delay_min,
        detail_view_delay_max=config.task.detail_view_delay_max,
        greet_delay_min=config.task.greet_delay_min,
        greet_delay_max=config.task.greet_delay_max,
        rest_after_candidates_min=config.task.rest_after_candidates_min,
        rest_after_candidates_max=config.task.rest_after_candidates_max,
        rest_times_min=config.task.rest_times_min,
        rest_times_max=config.task.rest_times_max,
        rest_duration_min=config.task.rest_duration_min,
        rest_duration_max=config.task.rest_duration_max,
        keyword_detail_open_probability=config.task.keyword_detail_open_probability,
        click_frequency=config.task.click_frequency,
        detail_mode=config.task.detail_mode,
    )
