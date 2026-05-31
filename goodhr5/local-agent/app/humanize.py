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

from playwright.async_api import Frame, Locator, Page

logger = logging.getLogger("goodhr5.humanize")

# ---------- 默认参数 ----------

DEFAULT_DELAY_MIN = 3
DEFAULT_DELAY_MAX = 8
DEFAULT_SCROLL_DISTANCE = 300
DEFAULT_MAX_SCROLLS = 20
DEFAULT_FIND_ATTEMPTS = 6
DEFAULT_FIND_INTERVAL_MS = 500
DEFAULT_VISIBLE_TIMEOUT_MS = 1500


@dataclass
class ElementLocatorSpec:
    """统一的页面元素定位协议。"""

    target_classes: list[list[str]]
    parent_classes: list[list[str]]
    find_attempts: int
    find_interval_ms: int
    visible_timeout_ms: int


def _normalize_class_name(class_name: str) -> str:
    value = str(class_name).strip()
    if not value:
        return ""
    # 兼容完整 CSS 选择器：
    # - class: .foo
    # - id: #foo
    # - attribute: [class*=foo]
    # - 伪类/组合器等：:scope / > .child / div.foo
    # 仅当传入纯类名时，才自动补 "."。
    if value.startswith((".", "#", "[", ":", ">", "~", "+")):
        return value
    if any(token in value for token in (" ", ">", "~", "+", ":", "[", "]", "(", ")", "=")):
        return value
    return f".{value}"


def _normalize_class_array(items: Any) -> list[str]:
    if not isinstance(items, list):
        return []
    result: list[str] = []
    for item in items:
        selector = _normalize_class_name(str(item))
        if selector:
            result.append(selector)
    return result


def _normalize_class_groups(items: Any) -> list[list[str]]:
    if not isinstance(items, list):
        return []
    if items and all(not isinstance(item, list) for item in items):
        group = _normalize_class_array(items)
        return [group] if group else []
    result: list[list[str]] = []
    for item in items:
        if isinstance(item, list):
            group = _normalize_class_array(item)
        else:
            selector = _normalize_class_name(str(item))
            group = [selector] if selector else []
        if group:
            result.append(group)
    return result


def parse_element_locator_spec(raw: Any, *, default_target_classes: Optional[list[str]] = None) -> ElementLocatorSpec:
    """
    解析统一的元素定位参数。

    支持两种格式：
    1. 新格式：{"parent_classes": [[...]], "target_classes": [[...]]}
    2. 兼容旧格式：直接传 class 数组，此时视为 target_classes
    """
    if isinstance(raw, dict):
        target_classes = _normalize_class_groups(raw.get("target_classes", []))
        parent_classes = _normalize_class_groups(raw.get("parent_classes", []))
        find_attempts = max(1, int(raw.get("find_attempts", DEFAULT_FIND_ATTEMPTS) or DEFAULT_FIND_ATTEMPTS))
        find_interval_ms = max(0, int(raw.get("find_interval_ms", DEFAULT_FIND_INTERVAL_MS) or DEFAULT_FIND_INTERVAL_MS))
        visible_timeout_ms = max(0, int(raw.get("visible_timeout_ms", DEFAULT_VISIBLE_TIMEOUT_MS) or DEFAULT_VISIBLE_TIMEOUT_MS))
    else:
        target_classes = _normalize_class_groups(raw)
        parent_classes = []
        find_attempts = DEFAULT_FIND_ATTEMPTS
        find_interval_ms = DEFAULT_FIND_INTERVAL_MS
        visible_timeout_ms = DEFAULT_VISIBLE_TIMEOUT_MS

    if not target_classes and default_target_classes:
        fallback_group = [_normalize_class_name(item) for item in default_target_classes if _normalize_class_name(item)]
        target_classes = [fallback_group] if fallback_group else []

    return ElementLocatorSpec(
        target_classes=target_classes,
        parent_classes=parent_classes,
        find_attempts=find_attempts,
        find_interval_ms=find_interval_ms,
        visible_timeout_ms=visible_timeout_ms,
    )


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
        page = locator.page
        viewport = page.viewport_size or {"width": 1280, "height": 800}
        vw = float(viewport.get("width") or 1280)
        vh = float(viewport.get("height") or 800)
        visible_left = max(0.0, float(box["x"]))
        visible_top = max(0.0, float(box["y"]))
        visible_right = min(vw, float(box["x"] + box["width"]))
        visible_bottom = min(vh, float(box["y"] + box["height"]))
        if visible_right <= visible_left or visible_bottom <= visible_top:
            logger.warning("%s 当前不在可视区域内，无法移动鼠标", label)
            return False
        x = visible_left + (visible_right - visible_left) / 2
        y = visible_top + (visible_bottom - visible_top) / 2
        await page.mouse.move(x, y)
        await asyncio.sleep(random.uniform(0.05, 0.2))
        logger.info("已移动鼠标到%s中心: (%.1f, %.1f)", label, x, y)
        return True
    except Exception as exc:
        logger.warning("移动鼠标到%s失败: %s", label, exc)
        return False


async def scroll_locator_into_view(locator: Locator, label: str = "元素") -> bool:
    """
    将元素滚动到当前可视区域内。

    Args:
        locator: Playwright 元素定位器
        label: 日志展示用名称

    Returns:
        bool: 是否成功滚动到可视区域
    """
    try:
        await locator.scroll_into_view_if_needed(timeout=3000)
        await asyncio.sleep(random.uniform(0.1, 0.25))
        in_viewport = await is_locator_in_viewport(locator)
        if in_viewport:
            logger.info("%s 已滚动到可视区域内", label)
        else:
            logger.warning("%s 已尝试滚动到可视区域，但当前仍不在视口内", label)
        return in_viewport
    except Exception as exc:
        logger.warning("滚动%s到视口失败: %s", label, exc)
        return False


async def is_locator_in_viewport(locator: Locator, visible_ratio: float = 0.35) -> bool:
    """
    判断元素是否位于当前可视区域内。

    Args:
        locator: Playwright 元素定位器
        visible_ratio: 最小可见面积比例，默认 35%

    Returns:
        bool: 元素是否有足够区域位于当前视口内
    """
    try:
        if not await locator.is_visible(timeout=500):
            return False
        box = await locator.bounding_box()
        if not box or box.get("width", 0) <= 0 or box.get("height", 0) <= 0:
            return False
        page = locator.page
        viewport = page.viewport_size or {"width": 1280, "height": 800}
        vw = float(viewport.get("width") or 1280)
        vh = float(viewport.get("height") or 800)
        visible_width = min(float(box["x"] + box["width"]), vw) - max(float(box["x"]), 0.0)
        visible_height = min(float(box["y"] + box["height"]), vh) - max(float(box["y"]), 0.0)
        if visible_width <= 0 or visible_height <= 0:
            return False
        required_width = float(box["width"]) * visible_ratio
        required_height = min(float(box["height"]) * visible_ratio, 120.0)
        return visible_width >= required_width and visible_height >= required_height
    except Exception:
        return False


def _iter_search_containers(container: Page | Frame | Locator) -> list[Page | Frame | Locator]:
    """
    返回用于元素查找的容器列表。

    - Page：先查主文档，再遍历当前页面所有 iframe
    - Frame：只查当前 frame
    - Locator：只查当前定位器范围
    """
    if isinstance(container, Page):
        frames = list(container.frames)
        ordered: list[Page | Frame | Locator] = [container]
        for frame in frames:
            if frame == container.main_frame:
                continue
            ordered.append(frame)
        return ordered
    return [container]


async def find_first_visible_locator(
    container: Page | Frame | Locator,
    selectors: list[str],
    label: str,
    find_attempts: int = DEFAULT_FIND_ATTEMPTS,
    find_interval_ms: int = DEFAULT_FIND_INTERVAL_MS,
) -> tuple[Locator, str]:
    """
    在页面或父元素中，按顺序查找第一个可见元素。

    Args:
        container: Page 或父级 Locator
        selectors: class 选择器数组
        label: 错误提示中的元素类型名称

    Returns:
        tuple[Locator, str]: 命中的定位器和选择器
    """
    for attempt in range(1, max(1, find_attempts) + 1):
        for search_container in _iter_search_containers(container):
            for selector in selectors:
                try:
                    locator = search_container.locator(selector).first
                    if await locator.is_visible(timeout=1500):
                        if attempt > 1:
                            logger.info("第 %d 次查找命中%s: %s", attempt, label, selector)
                        return locator, selector
                except Exception as exc:
                    logger.debug("查找%s失败 attempt=%d selector=%s err=%s", label, attempt, selector, exc)
                    continue
        if attempt < max(1, find_attempts):
            await asyncio.sleep(max(0, find_interval_ms) / 1000)
    raise ValueError(f"找不到{label}: {' / '.join(selectors)}")


async def _find_first_visible_locator_once(
    container: Page | Frame | Locator,
    selectors: list[str],
    label: str,
    visible_timeout_ms: int = DEFAULT_VISIBLE_TIMEOUT_MS,
) -> tuple[Page | Frame | Locator, Locator, str] | None:
    """
    单轮查找第一个可见元素。

    Args:
        container: 页面、Frame 或父级元素
        selectors: 需要依次尝试的选择器
        label: 日志中的元素名称
        visible_timeout_ms: 判断可见时最多等待的毫秒数

    Returns:
        命中的搜索容器、定位器和选择器；未命中返回 None。
    """
    # 当容器本身是 Locator 时，先尝试“匹配自身”，避免在容器内查找后代导致漏匹配。
    if isinstance(container, Locator):
        for selector in selectors:
            try:
                is_self_match = await container.evaluate(
                    "(el, selector) => !!(el && el.matches && el.matches(selector))",
                    selector,
                )
                if is_self_match and await container.is_visible(timeout=visible_timeout_ms):
                    return container, container, selector
            except Exception as exc:
                logger.debug("匹配%s自身失败 selector=%s err=%s", label, selector, exc)
                continue

    for search_container in _iter_search_containers(container):
        for selector in selectors:
            try:
                locator = search_container.locator(selector).first
                if await locator.is_visible(timeout=visible_timeout_ms):
                    return search_container, locator, selector
            except Exception as exc:
                logger.debug("查找%s失败 selector=%s err=%s", label, selector, exc)
                continue
    return None


async def locate_element_by_spec(container: Page | Frame | Locator, spec: ElementLocatorSpec, target_label: str = "目标元素") -> tuple[Locator, str, str]:
    """
    按统一协议先找父级，再找目标元素。

    Returns:
        tuple[Locator, str, str]: 目标定位器、命中的父级选择器、命中的目标选择器
    """
    if not spec.target_classes:
        raise ValueError(f"{target_label}的 target_classes 不能为空")

    parent_groups = spec.parent_classes or [[]]
    matched_error = ""
    for attempt in range(1, max(1, spec.find_attempts) + 1):
        for parent_group in parent_groups:
            parent_locator: Page | Frame | Locator = container
            matched_parent = ""
            if parent_group:
                found_parent = await _find_first_visible_locator_once(container, parent_group, "父级元素", spec.visible_timeout_ms)
                if not found_parent:
                    continue
                _parent_container, parent_locator, matched_parent = found_parent
            for target_group in spec.target_classes:
                found_target = await _find_first_visible_locator_once(parent_locator, target_group, target_label, spec.visible_timeout_ms)
                if found_target:
                    _target_container, target_locator, matched_target = found_target
                    if attempt > 1:
                        logger.info("第 %d 次交叉查找命中%s: parent=%s target=%s", attempt, target_label, matched_parent or "-", matched_target)
                    return target_locator, matched_parent, matched_target
                matched_error = " / ".join(target_group)
        if attempt < max(1, spec.find_attempts):
            await asyncio.sleep(max(0, spec.find_interval_ms) / 1000)
    raise ValueError(f"找不到{target_label}: {matched_error or '未提供有效选择器'}")


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


async def find_all_locators_by_spec(container: Page | Frame | Locator, spec: ElementLocatorSpec, target_label: str = "目标元素") -> tuple[Locator, str, str]:
    """
    按统一协议定位一组元素，返回匹配集合定位器。

    Returns:
        tuple[Locator, str, str]: 集合定位器、命中的父级选择器、命中的目标选择器
    """
    if not spec.target_classes:
        raise ValueError(f"{target_label}的 target_classes 不能为空")

    parent_groups = spec.parent_classes or [[]]
    matched_error = ""
    for attempt in range(1, max(1, spec.find_attempts) + 1):
        for parent_group in parent_groups:
            parent_locator: Page | Frame | Locator = container
            matched_parent = ""
            if parent_group:
                found_parent = await _find_first_visible_locator_once(container, parent_group, "父级元素", spec.visible_timeout_ms)
                if not found_parent:
                    continue
                _parent_container, parent_locator, matched_parent = found_parent

            for target_group in spec.target_classes:
                for search_container in _iter_search_containers(parent_locator):
                    for selector in target_group:
                        try:
                            locators = search_container.locator(selector)
                            if await locators.count() > 0:
                                if attempt > 1:
                                    logger.info("第 %d 次交叉查找命中%s集合: parent=%s target=%s", attempt, target_label, matched_parent or "-", selector)
                                return locators, matched_parent, selector
                        except Exception as exc:
                            logger.debug("查找%s集合失败 attempt=%d selector=%s err=%s", target_label, attempt, selector, exc)
                            continue
                matched_error = " / ".join(target_group)
        if attempt < max(1, spec.find_attempts):
            await asyncio.sleep(max(0, spec.find_interval_ms) / 1000)
    raise ValueError(f"找不到{target_label}: {matched_error or '未提供有效选择器'}")


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

    delay_ms = random.randint(100, 300)
    await page.mouse.move(x, y)
    await page.mouse.click(x, y, delay=delay_ms)
    logger.debug("随机点击%s: point=(%.1f,%.1f), delay=%dms", label, x, y, delay_ms)
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
