"""
GoodHR 自动化工具 - 人类行为模拟模块

提供随机延迟、滚动模式等仿真人操作行为，
配合 CloakBrowser 的 humanize 参数实现更真实的自动化行为。
"""

import asyncio
import random
from typing import Optional

from playwright.async_api import Page

from core.settings import TaskConfig
from utils.logger import get_logger

logger = get_logger("humanize")


async def random_delay(min_seconds: int = 3, max_seconds: int = 8) -> None:
    """
    随机等待一段时间，模拟人工操作间隔

    Args:
        min_seconds: 最小等待秒数
        max_seconds: 最大等待秒数
    """
    delay = random.uniform(min_seconds, max_seconds)
    logger.debug(f"等待 {delay:.1f} 秒...")
    await asyncio.sleep(delay)


async def human_scroll(
    page: Page,
    distance: int = 300,
    steps: Optional[int] = None,
) -> None:
    """
    仿真人滚动页面，分多步完成滚轮操作

    Args:
        page: Playwright Page 实例
        distance: 总滚动像素距离（正数向下，负数向上）
        steps: 滚动分几步完成，None 则自动计算
    """
    if steps is None:
        steps = random.randint(3, 8)

    step_distance = distance / steps
    for i in range(steps):
        jitter = random.uniform(-10, 10)
        await page.mouse.wheel(0, step_distance + jitter)
        await asyncio.sleep(random.uniform(0.05, 0.2))

    logger.debug(f"已完成滚动，总距离: {distance}px，分 {steps} 步")


async def human_type(page: Page, selector: str, text: str, delay: Optional[int] = None) -> None:
    """
    仿真人输入文字，逐字符随机延迟

    Args:
        page: Playwright Page 实例
        selector: 输入框选择器
        text: 要输入的文本
        delay: 每字符基础延迟（毫秒），None 则随机
    """
    if delay is None:
        delay = random.randint(50, 150)

    locator = page.locator(selector)
    await locator.click()
    await asyncio.sleep(random.uniform(0.1, 0.3))

    for char in text:
        await page.keyboard.type(char, delay=random.randint(max(30, delay - 30), delay + 50))
        if random.random() < 0.05:
            await asyncio.sleep(random.uniform(0.3, 0.8))

    logger.debug(f"已输入文本: {text[:20]}...")


async def wait_and_click(
    page: Page,
    selector: str,
    timeout: int = 10000,
    delay_before: float = 0.5,
) -> bool:
    """
    等待元素出现后点击，带前后延迟

    Args:
        page: Playwright Page 实例
        selector: 目标元素选择器
        timeout: 等待超时时间（毫秒）
        delay_before: 点击前延迟（秒）

    Returns:
        bool: 是否成功点击
    """
    try:
        locator = page.locator(selector)
        await locator.wait_for(state="visible", timeout=timeout)
        await asyncio.sleep(delay_before + random.uniform(0, 0.5))
        await locator.click()
        logger.debug(f"已点击: {selector}")
        return True
    except Exception as e:
        logger.warning(f"点击失败: {selector}, 原因: {e}")
        return False


async def scroll_to_load(
    page: Page,
    task_config: Optional[TaskConfig] = None,
    max_scrolls: int = 20,
    stop_condition: Optional[callable] = None,
) -> None:
    """
    滚动加载候选人列表，模拟人工浏览行为

    逐屏向下滚动，每屏之间加入随机延迟，
    可设置停止条件（如检测到已无新候选人加载）。

    Args:
        page: Playwright Page 实例
        task_config: 任务配置（控制滚动延迟范围）
        max_scrolls: 最大滚动次数
        stop_condition: 停止条件回调，返回 True 则停止滚动
    """
    cfg = task_config or TaskConfig()
    scroll_delay_min = cfg.scroll_delay_min
    scroll_delay_max = cfg.scroll_delay_max

    for i in range(max_scrolls):
        distance = random.randint(250, 450)
        await human_scroll(page, distance=distance)

        if stop_condition and await stop_condition():
            logger.info(f"滚动到第 {i + 1} 屏时满足停止条件")
            break

        await random_delay(scroll_delay_min, scroll_delay_max)

    logger.info(f"滚动加载完成，共滚动 {max_scrolls} 屏")
