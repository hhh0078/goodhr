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
from dataclasses import dataclass
from typing import Any, Callable, Optional

from playwright.async_api import Locator, Page

logger = logging.getLogger("goodhr5.humanize")

# ---------- 默认参数 ----------

DEFAULT_DELAY_MIN = 3
DEFAULT_DELAY_MAX = 8
DEFAULT_SCROLL_DISTANCE = 300
DEFAULT_MAX_SCROLLS = 20


@dataclass
class ElementLocatorSpec:
    """统一的页面元素定位协议。"""

    target_classes: list[str]
    parent_classes: list[str]


def _normalize_class_name(class_name: str) -> str:
    value = str(class_name).strip()
    if not value:
        return ""
    return value if value.startswith(".") else f".{value}"


def _normalize_class_array(items: Any) -> list[str]:
    if not isinstance(items, list):
        return []
    result: list[str] = []
    for item in items:
        selector = _normalize_class_name(str(item))
        if selector:
            result.append(selector)
    return result


def parse_element_locator_spec(raw: Any, *, default_target_classes: Optional[list[str]] = None) -> ElementLocatorSpec:
    """
    解析统一的元素定位参数。

    支持两种格式：
    1. 新格式：{"parent_classes": [...], "target_classes": [...]}
    2. 兼容旧格式：直接传 class 数组，此时视为 target_classes
    """
    if isinstance(raw, dict):
        target_classes = _normalize_class_array(raw.get("target_classes", []))
        parent_classes = _normalize_class_array(raw.get("parent_classes", []))
    else:
        target_classes = _normalize_class_array(raw)
        parent_classes = []

    if not target_classes and default_target_classes:
        target_classes = [_normalize_class_name(item) for item in default_target_classes if _normalize_class_name(item)]

    return ElementLocatorSpec(target_classes=target_classes, parent_classes=parent_classes)


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


async def move_mouse_to_locator(locator: Locator, label: str = "元素") -> bool:
    """
    将鼠标移动到元素中心。

    Args:
        locator: Playwright 元素定位器
        label: 日志展示用名称

    Returns:
        bool: 是否成功移动
    """
    try:
        box = await locator.bounding_box()
        if not box or box.get("width", 0) <= 0 or box.get("height", 0) <= 0:
            logger.warning("%s 无法获取有效位置", label)
            return False
        x = box["x"] + box["width"] / 2
        y = box["y"] + box["height"] / 2
        page = locator.page
        await page.mouse.move(x, y)
        await asyncio.sleep(random.uniform(0.05, 0.2))
        logger.info("已移动鼠标到%s中心: (%.1f, %.1f)", label, x, y)
        return True
    except Exception as exc:
        logger.warning("移动鼠标到%s失败: %s", label, exc)
        return False


async def find_first_visible_locator(container: Page | Locator, selectors: list[str], label: str) -> tuple[Locator, str]:
    """
    在页面或父元素中，按顺序查找第一个可见元素。

    Args:
        container: Page 或父级 Locator
        selectors: class 选择器数组
        label: 错误提示中的元素类型名称

    Returns:
        tuple[Locator, str]: 命中的定位器和选择器
    """
    for selector in selectors:
        try:
            locator = container.locator(selector).first
            if await locator.is_visible(timeout=1500):
                return locator, selector
        except Exception as exc:
            logger.debug("查找%s失败 selector=%s err=%s", label, selector, exc)
            continue
    raise ValueError(f"找不到{label}: {' / '.join(selectors)}")


async def locate_element_by_spec(container: Page | Locator, spec: ElementLocatorSpec, target_label: str = "目标元素") -> tuple[Locator, str, str]:
    """
    按统一协议先找父级，再找目标元素。

    Returns:
        tuple[Locator, str, str]: 目标定位器、命中的父级选择器、命中的目标选择器
    """
    if not spec.target_classes:
        raise ValueError(f"{target_label}的 target_classes 不能为空")

    parent_locator: Page | Locator = container
    matched_parent = ""
    if spec.parent_classes:
        parent_locator, matched_parent = await find_first_visible_locator(container, spec.parent_classes, "父级元素")

    target_locator, matched_target = await find_first_visible_locator(parent_locator, spec.target_classes, target_label)
    return target_locator, matched_parent, matched_target


async def move_mouse_to_element_spec(page: Page, spec: ElementLocatorSpec, target_label: str = "目标元素") -> tuple[bool, str, str]:
    """
    按统一协议定位元素并移动鼠标到其中心。

    Returns:
        tuple[bool, str, str]: 是否成功、父级选择器、目标选择器
    """
    target_locator, matched_parent, matched_target = await locate_element_by_spec(page, spec, target_label)
    moved = await move_mouse_to_locator(target_locator, matched_target)
    if not moved:
        raise ValueError(f"已找到{target_label}，但无法移动鼠标到元素上: {matched_target}")
    return moved, matched_parent, matched_target


async def find_all_locators_by_spec(container: Page | Locator, spec: ElementLocatorSpec, target_label: str = "目标元素") -> tuple[Locator, str, str]:
    """
    按统一协议定位一组元素，返回匹配集合定位器。

    Returns:
        tuple[Locator, str, str]: 集合定位器、命中的父级选择器、命中的目标选择器
    """
    if not spec.target_classes:
        raise ValueError(f"{target_label}的 target_classes 不能为空")

    parent_locator: Page | Locator = container
    matched_parent = ""
    if spec.parent_classes:
        parent_locator, matched_parent = await find_first_visible_locator(container, spec.parent_classes, "父级元素")

    for selector in spec.target_classes:
        try:
            locators = parent_locator.locator(selector)
            if await locators.count() > 0:
                return locators, matched_parent, selector
        except Exception as exc:
            logger.debug("查找%s集合失败 selector=%s err=%s", target_label, selector, exc)
            continue
    raise ValueError(f"找不到{target_label}: {' / '.join(spec.target_classes)}")


async def scroll_to_load(
    page: Page,
    scroll_delay_min: int = DEFAULT_DELAY_MIN,
    scroll_delay_max: int = DEFAULT_DELAY_MAX,
    max_scrolls: int = DEFAULT_MAX_SCROLLS,
    element_spec: Optional[ElementLocatorSpec] = None,
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
        element_spec: 可选元素定位协议；传入后先定位元素并移动到其上方再滚动
        stop_condition: 停止条件回调，返回 True 则停止滚动
    """
    if element_spec and element_spec.target_classes:
        _moved, matched_parent, matched_target = await move_mouse_to_element_spec(page, element_spec, "滚动目标元素")
        if matched_parent:
            logger.info("滚动前已命中父级元素: %s，目标元素: %s", matched_parent, matched_target)
        else:
            logger.info("滚动前已命中目标元素: %s", matched_target)

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
