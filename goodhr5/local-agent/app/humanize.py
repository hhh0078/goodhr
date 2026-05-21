"""
本文件负责仿真人操作行为模拟。

提供随机延迟、滚动、打字、点击等辅助函数，
配合 CloakBrowser 的 humanize 参数实现更真实的自动化行为。
沿用 goodhrpy 的已验证可用代码，迁入 GoodHR 5 Local Agent。
"""

from __future__ import annotations

import asyncio
import logging
import random
from typing import Callable, Optional

from playwright.async_api import Page

logger = logging.getLogger("goodhr5.humanize")

# ---------- 默认参数 ----------

DEFAULT_DELAY_MIN = 3
DEFAULT_DELAY_MAX = 8
DEFAULT_SCROLL_DISTANCE = 300
DEFAULT_MAX_SCROLLS = 20


# ---------- 延迟 ----------


async def random_delay(min_seconds: int = DEFAULT_DELAY_MIN, max_seconds: int = DEFAULT_DELAY_MAX) -> None:
    """
    随机等待一段时间，模拟人工操作间隔。

    Args:
        min_seconds: 最小等待秒数
        max_seconds: 最大等待秒数
    """
    delay = random.uniform(min_seconds, max_seconds)
    logger.debug("等待 %.1f 秒...", delay)
    await asyncio.sleep(delay)


# ---------- 滚动 ----------


async def human_scroll(
    page: Page,
    distance: int = DEFAULT_SCROLL_DISTANCE,
    steps: Optional[int] = None,
) -> None:
    """
    仿真人滚动页面，分多步完成滚轮操作。

    Args:
        page: Playwright Page 实例
        distance: 总滚动像素距离（正数向下，负数向上）
        steps: 滚动分几步完成，None 则自动计算
    """
    if steps is None:
        steps = random.randint(3, 8)

    step_distance = distance / steps
    for _i in range(steps):
        jitter = random.uniform(-10, 10)
        await page.mouse.wheel(0, step_distance + jitter)
        await asyncio.sleep(random.uniform(0.05, 0.2))

    logger.debug("已完成滚动，总距离: %dpx，分 %d 步", distance, steps)


async def move_mouse_to_element_classes(page: Page, element_classes: list[str]) -> bool:
    """
    将鼠标移动到第一个可见的 class 元素中心，便于后续滚轮作用于对应容器。

    Args:
        page: Playwright Page 实例
        element_classes: class 名数组，例如 ["candidate-list", "list-wrap"]

    Returns:
        bool: 是否成功移动到某个元素上方
    """
    for item in element_classes:
        class_name = str(item).strip()
        if not class_name:
            continue
        selector = class_name if class_name.startswith(".") else f".{class_name}"
        try:
            locator = page.locator(selector).first
            if not await locator.is_visible(timeout=1500):
                continue
            box = await locator.bounding_box()
            if not box or box.get("width", 0) <= 0 or box.get("height", 0) <= 0:
                continue
            x = box["x"] + box["width"] / 2
            y = box["y"] + box["height"] / 2
            await page.mouse.move(x, y)
            await asyncio.sleep(random.uniform(0.05, 0.2))
            logger.info("已移动鼠标到滚动目标: %s", selector)
            return True
        except Exception as exc:
            logger.debug("定位滚动目标失败 selector=%s err=%s", selector, exc)
            continue
    logger.warning("未找到可用滚动目标，将继续按页面默认位置滚动")
    return False


async def scroll_to_load(
    page: Page,
    scroll_delay_min: int = DEFAULT_DELAY_MIN,
    scroll_delay_max: int = DEFAULT_DELAY_MAX,
    max_scrolls: int = DEFAULT_MAX_SCROLLS,
    element_classes: Optional[list[str]] = None,
    stop_condition: Optional[Callable[..., bool]] = None,
) -> None:
    """
    滚动加载候选人列表，模拟人工浏览行为。

    逐屏向下滚动，每屏之间加入随机延迟，
    可设置停止条件（如检测到已无新候选人加载）。

    Args:
        page: Playwright Page 实例
        scroll_delay_min: 滚动间最小延迟秒数
        scroll_delay_max: 滚动间最大延迟秒数
        max_scrolls: 最大滚动次数
        element_classes: 可选 class 数组；传入后先移动到对应元素上再滚动
        stop_condition: 停止条件回调，返回 True 则停止滚动
    """
    if element_classes:
        await move_mouse_to_element_classes(page, element_classes)

    for i in range(max_scrolls):
        distance = random.randint(250, 450)
        await human_scroll(page, distance=distance)

        if stop_condition:
            try:
                should_stop = await stop_condition()
                if should_stop:
                    logger.info("滚动到第 %d 屏时满足停止条件", i + 1)
                    break
            except Exception as e:
                logger.warning("检查停止条件时出错: %s", e)

        await random_delay(scroll_delay_min, scroll_delay_max)

    logger.info("滚动加载完成，共滚动 %d 屏", max_scrolls)


# ---------- 输入与点击 ----------


async def human_type(page: Page, selector: str, text: str, delay: Optional[int] = None) -> None:
    """
    仿真人输入文字，逐字符随机延迟。

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

    logger.debug("已输入文本: %s...", text[:20])


async def click_box_random_point(
    page: Page,
    box: dict,
    label: str = "元素",
    min_ratio: float = 0.3,
    max_ratio: float = 0.7,
) -> bool:
    """
    在元素盒子内部随机点击一个点。

    Args:
        page: Playwright Page 实例
        box: 元素的 bounding_box
        label: 元素标签
        min_ratio: 坐标范围下限
        max_ratio: 坐标范围上限

    Returns:
        bool: 是否成功点击
    """
    if not box or box.get("width", 0) <= 0 or box.get("height", 0) <= 0:
        return False

    ratio_x = random.uniform(min_ratio, max_ratio)
    ratio_y = random.uniform(min_ratio, max_ratio)
    x = box["x"] + box["width"] * ratio_x
    y = box["y"] + box["height"] * ratio_y

    await page.mouse.move(x, y)
    await page.mouse.click(x, y)
    logger.debug("随机点击%s: point=(%.1f,%.1f)", label, x, y)
    return True


async def navigate_to_page(page: Page, url: str, timeout: int = 30000) -> bool:
    """
    导航到指定页面。

    Args:
        page: Playwright Page 实例
        url: 目标页面 URL
        timeout: 导航超时（毫秒）

    Returns:
        bool: 是否成功导航
    """
    try:
        await page.goto(url, wait_until="domcontentloaded", timeout=timeout)
        await page.wait_for_timeout(2000)
        logger.info("已导航到 %s", url)
        return True
    except Exception as e:
        logger.error("导航失败: %s, 原因: %s", url, e)
        return False


async def wait_for_elements(page: Page, selectors: list[str], timeout: int = 10000) -> bool:
    """
    等待任意选择器对应的元素出现。

    Args:
        page: Playwright Page 实例
        selectors: CSS 选择器列表
        timeout: 超时（毫秒）

    Returns:
        bool: 是否至少有一个匹配
    """
    if not selectors:
        return True
    for selector in selectors:
        try:
            await page.locator(selector).first.wait_for(state="visible", timeout=timeout)
            return True
        except Exception:
            continue
    logger.warning("等待元素超时")
    return False


async def wait_and_click(
    page: Page,
    selector: str,
    timeout: int = 10000,
    delay_before: float = 0.5,
) -> bool:
    """
    等待元素出现后点击，带前后延迟。

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
        logger.debug("已点击: %s", selector)
        return True
    except Exception as e:
        logger.warning("点击失败: %s, 原因: %s", selector, e)
        return False
