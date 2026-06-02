"""
本文件负责候选人详情弹框的截图和拼接。

支持逐段滚动截取超出视口的弹框内容，通过 Pillow 拼接为完整截图。
沿用 goodhrpy 的已验证可用代码，迁入 GoodHR 5 Local Agent 执行层。
"""

from __future__ import annotations

import io
import logging
import math
from typing import Optional

from PIL import Image
from playwright.async_api import Page

logger = logging.getLogger("goodhr5.screenshot")


def compute_strip_diff(strip1: Image.Image, strip2: Image.Image) -> float:
    """
    计算两个图片条带的像素差异值。

    将两个等宽等高的图片条带转为 RGB 像素列表，
    逐像素计算颜色差值之和，用于图像匹配时判断相似度。
    差异值越小表示两张图片越相似。

    Args:
        strip1: 第一个图片条带
        strip2: 第二个图片条带

    Returns:
        float: 像素差异总值，0 表示完全相同
    """
    pixels1 = list(strip1.convert("RGB").getdata())
    pixels2 = list(strip2.convert("RGB").getdata())
    total = 0
    for p1, p2 in zip(pixels1, pixels2):
        total += abs(p1[0] - p2[0]) + abs(p1[1] - p2[1]) + abs(p1[2] - p2[2])
    return total


def images_are_same(img1: Image.Image, img2: Image.Image, threshold: float = 0.98) -> bool:
    """
    判断两张图片是否基本相同。

    Args:
        img1: 第一张图片
        img2: 第二张图片
        threshold: 相似度阈值

    Returns:
        bool: 是否基本相同
    """
    if img1.size != img2.size:
        return False

    try:
        import numpy as np
        arr1 = np.array(img1.convert("RGB"))
        arr2 = np.array(img2.convert("RGB"))
        diff = np.abs(arr1.astype(int) - arr2.astype(int))
        same_ratio = np.sum(diff < 10) / diff.size
        del arr1, arr2, diff
        return same_ratio >= threshold
    except ImportError:
        pixels1 = list(img1.convert("RGB").getdata())
        pixels2 = list(img2.convert("RGB").getdata())
        same_count = sum(
            1 for p1, p2 in zip(pixels1, pixels2)
            if abs(p1[0] - p2[0]) < 10 and abs(p1[1] - p2[1]) < 10 and abs(p1[2] - p2[2]) < 10
        )
        return same_count / len(pixels1) >= threshold


def images_are_scroll_duplicates(img1: Image.Image, img2: Image.Image, threshold: float = 0.94) -> bool:
    """
    判断两张滚动截图是否重复。

    滚动到底后页面可能仍有光标、阴影或悬浮元素轻微变化，整图严格比较容易误判。
    这里只比较中间主体区域，并使用更宽松阈值，用于过滤到底后的重复截图。

    Args:
        img1: 第一张滚动截图。
        img2: 第二张滚动截图。
        threshold: 相似度阈值。

    Returns:
        bool: 是否可视为重复截图。
    """
    if img1.size != img2.size:
        return False
    width, height = img1.size
    if width <= 1 or height <= 1:
        return images_are_same(img1, img2, threshold)
    top = int(height * 0.12)
    bottom = int(height * 0.88)
    if bottom <= top:
        return images_are_same(img1, img2, threshold)
    crop1 = img1.crop((0, top, width, bottom))
    crop2 = img2.crop((0, top, width, bottom))
    try:
        return images_are_same(crop1, crop2, threshold)
    finally:
        crop1.close()
        crop2.close()


def merge_two(top_img: Image.Image, bottom_img: Image.Image, max_overlap: int) -> Image.Image:
    """
    将两张图片按最佳匹配位置纵向合并。

    Args:
        top_img: 顶部图片
        bottom_img: 底部图片
        max_overlap: 最大重叠像素数

    Returns:
        Image.Image: 合并后的图片
    """
    search_range = min(max_overlap + 50, top_img.height - 1, bottom_img.height - 1)
    strip_height = min(30, bottom_img.height - 1)
    bottom_strip = bottom_img.crop((0, 0, bottom_img.width, strip_height))

    best_y = top_img.height - max_overlap
    best_diff = float("inf")

    for y in range(max(top_img.height - search_range, 0), top_img.height - strip_height + 1):
        top_strip = top_img.crop((0, y, top_img.width, y + strip_height))
        diff = compute_strip_diff(top_strip, bottom_strip)
        top_strip.close()
        if diff < best_diff:
            best_diff = diff
            best_y = y

    bottom_strip.close()
    merged = Image.new("RGB", (top_img.width, best_y + bottom_img.height), (255, 255, 255))
    merged.paste(top_img, (0, 0))
    merged.paste(bottom_img, (0, best_y))
    return merged


def stitch_screenshots(screenshot_bytes_list: list, overlap_pixels: int, platform_name: str = "") -> Optional[bytes]:
    """
    将多张截图按重叠区域拼接成一张完整图片。

    Args:
        screenshot_bytes_list: 多张截图的 PNG 字节数据列表
        overlap_pixels: 预期重叠像素数
        platform_name: 平台名（用于日志）

    Returns:
        Optional[bytes]: 拼接后的 PNG 字节数据
    """
    try:
        images = [Image.open(io.BytesIO(s)) for s in screenshot_bytes_list]
        if len(images) == 1:
            output = io.BytesIO()
            images[0].save(output, format="PNG")
            result_bytes = output.getvalue()
            for img in images:
                img.close()
            return result_bytes

        result = images[0]
        for i in range(1, len(images)):
            new_result = merge_two(result, images[i], overlap_pixels)
            if i > 1:
                result.close()
            result = new_result
            images[i].close()
        images[0].close()

        output = io.BytesIO()
        result.save(output, format="PNG")
        logger.info("[%s] 截图拼接完成（%d 张，总高度 %dpx）", platform_name, len(images), result.height)
        result_bytes = output.getvalue()
        result.close()
        return result_bytes
    except Exception as e:
        logger.warning("截图拼接失败: %s", e)
        return screenshot_bytes_list[0] if screenshot_bytes_list else None


def remove_duplicate_scroll_screenshots(screenshot_bytes_list: list[bytes], platform_name: str = "") -> list[bytes]:
    """
    删除滚动到底后产生的相邻重复截图。

    Args:
        screenshot_bytes_list: 原始滚动截图列表。
        platform_name: 平台名或日志标签。

    Returns:
        list[bytes]: 去重后的截图列表。
    """
    if len(screenshot_bytes_list) <= 1:
        return screenshot_bytes_list
    filtered: list[bytes] = []
    prev_image: Image.Image | None = None
    for index, screenshot_bytes in enumerate(screenshot_bytes_list):
        current_image = Image.open(io.BytesIO(screenshot_bytes))
        try:
            if prev_image is not None and images_are_scroll_duplicates(prev_image, current_image):
                logger.info("[%s] 删除重复滚动截图 index=%d", platform_name, index)
                continue
            filtered.append(screenshot_bytes)
            if prev_image is not None:
                prev_image.close()
            prev_image = current_image.copy()
        finally:
            current_image.close()
    if prev_image is not None:
        prev_image.close()
    return filtered


async def screenshot_modal(
    page: Page, modal_selectors: list[str], platform_name: str = ""
) -> Optional[bytes]:
    """
    对候选人详情弹框截图，只截弹框区域而非整个页面。

    Args:
        page: Playwright Page 实例
        modal_selectors: 弹框 CSS 选择器列表
        platform_name: 平台名（用于日志）

    Returns:
        Optional[bytes]: PNG 截图字节数据
    """
    if not modal_selectors:
        return await _fallback_screenshot(page, platform_name)

    viewport = page.viewport_size
    vw = viewport["width"] if viewport else 1920
    vh = viewport["height"] if viewport else 1080

    for selector in modal_selectors:
        try:
            locator = page.locator(selector).first
            if not await locator.is_visible(timeout=3000):
                continue

            box = await locator.bounding_box()
            if not box or box["width"] < 50 or box["height"] < 50:
                continue

            if box["width"] >= vw * 0.9 and box["height"] >= vh * 0.9:
                logger.debug("[%s] 选择器 %s 匹配到全屏遮罩，跳过", platform_name, selector)
                continue

            needs_scroll = box["y"] + box["height"] > vh
            if not needs_scroll:
                screenshot_bytes = await locator.screenshot(type="png")
            else:
                screenshot_bytes = await _scroll_and_stitch(page, locator, box, vh, platform_name)

            if screenshot_bytes:
                logger.info("[%s] 弹框截图成功", platform_name)
                return screenshot_bytes
        except Exception as e:
            logger.warning("[%s] 选择器 %s 截图失败: %s", platform_name, selector, e)

    logger.warning("[%s] 弹框截图失败，回退全页截图", platform_name)
    return await _fallback_screenshot(page, platform_name)


async def screenshot_locator_full(page: Page, locator, platform_name: str = "") -> Optional[bytes]:
    """
    对指定元素截图；元素超出视口时会通过鼠标滚轮拼接为长图。

    Args:
        page: Playwright Page 实例
        locator: 目标元素定位器
        platform_name: 平台名或日志标签

    Returns:
        Optional[bytes]: PNG 截图字节数据。
    """
    try:
        if not await locator.is_visible(timeout=3000):
            return None
        box = await locator.bounding_box()
        if not box or box["width"] < 20 or box["height"] < 20:
            return None

        viewport = page.viewport_size
        vh = viewport["height"] if viewport else 1080
        needs_scroll = box["y"] < 0 or box["y"] + box["height"] > vh
        if needs_scroll:
            return await _scroll_and_stitch(page, locator, box, vh, platform_name)
        return await locator.screenshot(type="png")
    except Exception as exc:
        logger.warning("[%s] 元素完整截图失败: %s", platform_name, exc)
        return None


async def screenshot_locator_parts(page: Page, locator, platform_name: str = "") -> list[bytes]:
    """
    对指定元素按当前视口分段截图。

    元素超出视口时返回多张小图，供 OCR 分段识别，避免拼成长图后识别变慢。

    Args:
        page: Playwright Page 实例。
        locator: 目标元素定位器。
        platform_name: 平台名或日志标签。

    Returns:
        list[bytes]: 按页面顺序排列的 PNG 截图字节列表。
    """
    try:
        if not await locator.is_visible(timeout=3000):
            return []
        box = await locator.bounding_box()
        if not box or box["width"] < 20 or box["height"] < 20:
            return []

        viewport = page.viewport_size
        vh = viewport["height"] if viewport else 1080
        needs_scroll = box["y"] < 0 or box["y"] + box["height"] > vh
        if needs_scroll:
            return await _scroll_capture_parts(page, locator, box, vh, platform_name)
        screenshot_bytes = await locator.screenshot(type="png")
        logger.info("[%s] 元素无需滚动，返回单张截图 bytes=%d", platform_name, len(screenshot_bytes))
        return [screenshot_bytes]
    except Exception as exc:
        logger.warning("[%s] 元素分段截图失败: %s", platform_name, exc)
        return []


async def _scroll_and_stitch(
    page: Page, locator, box: dict, viewport_height: int, platform_name: str
) -> Optional[bytes]:
    """通过鼠标滚轮滚动逐段截图后拼接成完整弹框。"""
    screenshots, overlap = await _scroll_capture_parts_with_overlap(page, locator, box, viewport_height, platform_name)
    if not screenshots:
        return None
    if len(screenshots) == 1:
        return screenshots[0]
    return stitch_screenshots(screenshots, overlap, platform_name)


async def _scroll_capture_parts(
    page: Page, locator, box: dict, viewport_height: int, platform_name: str
) -> list[bytes]:
    """
    通过鼠标滚轮滚动逐段截图，返回去重后的小图列表。

    Args:
        page: Playwright Page 实例。
        locator: 目标元素定位器。
        box: 目标元素边界。
        viewport_height: 当前视口高度。
        platform_name: 平台名或日志标签。

    Returns:
        list[bytes]: 去重后的分段截图列表。
    """
    screenshots, _overlap = await _scroll_capture_parts_with_overlap(page, locator, box, viewport_height, platform_name)
    return screenshots


async def _scroll_capture_parts_with_overlap(
    page: Page, locator, box: dict, viewport_height: int, platform_name: str
) -> tuple[list[bytes], int]:
    """
    通过鼠标滚轮滚动逐段截图，并返回拼接需要的重叠高度。

    Args:
        page: Playwright Page 实例。
        locator: 目标元素定位器。
        box: 目标元素边界。
        viewport_height: 当前视口高度。
        platform_name: 平台名或日志标签。

    Returns:
        tuple[list[bytes], int]: 分段截图列表和重叠高度。
    """
    clip_x = max(int(round(float(box["x"]))), 0)
    clip_y = max(int(round(float(box["y"]))), 0)
    clip_width = max(int(round(float(box["width"]))), 1)
    clip_bottom = min(int(round(float(box["y"]) + float(box["height"]))), int(viewport_height))
    clip_height = max(clip_bottom - clip_y, 0)
    if clip_height <= 0:
        return [], 0

    clip = {"x": clip_x, "y": clip_y, "width": clip_width, "height": clip_height}
    mouse_x = clip_x + clip_width / 2
    mouse_y = box["y"] + clip_height / 2
    await page.mouse.move(mouse_x, mouse_y)
    await page.wait_for_timeout(300)

    scroll_delta = max(int(clip_height * 0.7), 1)
    overlap = max(int(clip_height - scroll_delta), 0)
    max_scrolls = 10
    estimated_screenshots = max(1, math.ceil(max(float(box["height"]) - clip_height, 0) / scroll_delta) + 1)
    logger.info(
        "[%s] 滚动截图参数 selector_box=(x=%s,y=%s,w=%s,h=%s) viewport_height=%s clip=%s scroll_delta=%d overlap=%d estimated_screenshots=%d max_scrolls=%d",
        platform_name,
        box.get("x"),
        box.get("y"),
        box.get("width"),
        box.get("height"),
        viewport_height,
        clip,
        scroll_delta,
        overlap,
        estimated_screenshots,
        max_scrolls,
    )
    screenshots = []
    prev_clip_image = None
    all_opened_images = []

    for i in range(max_scrolls):
        current_screenshot = await page.screenshot(type="png", clip=clip)
        current_image = Image.open(io.BytesIO(current_screenshot))
        all_opened_images.append(current_image)
        logger.info(
            "[%s] 滚动截图 step=%d/%d bytes=%d image=%dx%d kept=%d estimated=%d",
            platform_name,
            i + 1,
            max_scrolls,
            len(current_screenshot),
            current_image.width,
            current_image.height,
            len(screenshots),
            estimated_screenshots,
        )

        if prev_clip_image is not None and images_are_scroll_duplicates(prev_clip_image, current_image):
            logger.info(
                "[%s] 滚动截图 step=%d 判定主体内容重复，已到底部，丢弃当前截图 bytes=%d",
                platform_name,
                i + 1,
                len(current_screenshot),
            )
            break

        screenshots.append(current_screenshot)
        logger.info("[%s] 滚动截图 step=%d 已保留，准备滚动 delta=%d", platform_name, i + 1, scroll_delta)
        if prev_clip_image is not None:
            prev_clip_image.close()
        prev_clip_image = current_image
        await page.mouse.wheel(0, scroll_delta)
        await page.wait_for_timeout(500)

    for img in all_opened_images:
        try:
            img.close()
        except Exception:
            pass

    if not screenshots:
        return [], overlap
    before_dedupe_count = len(screenshots)
    screenshots = remove_duplicate_scroll_screenshots(screenshots, platform_name)
    logger.info(
        "[%s] 滚动截图完成 raw_count=%d kept_count=%d removed_count=%d estimated=%d",
        platform_name,
        before_dedupe_count,
        len(screenshots),
        before_dedupe_count - len(screenshots),
        estimated_screenshots,
    )
    if len(screenshots) == 1:
        return screenshots, overlap
    return screenshots, overlap


async def _fallback_screenshot(page: Page, platform_name: str) -> Optional[bytes]:
    """全页截图兜底方案。"""
    try:
        logger.warning("[%s] 回退到全页截图", platform_name)
        return await page.screenshot(type="png")
    except Exception as e:
        logger.error("全页截图失败: %s", e)
        return None
