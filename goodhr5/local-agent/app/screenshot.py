"""
本文件负责候选人详情弹框的截图和拼接。

支持逐段滚动截取超出视口的弹框内容，通过 Pillow 拼接为完整截图。
沿用 goodhrpy 的已验证可用代码，迁入 GoodHR 5 Local Agent 执行层。
"""

from __future__ import annotations

import io
import logging
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


async def _scroll_and_stitch(
    page: Page, locator, box: dict, viewport_height: int, platform_name: str
) -> Optional[bytes]:
    """通过鼠标滚轮滚动逐段截图后拼接成完整弹框。"""
    clip_y = max(box["y"], 0)
    clip_height = min(box["y"] + box["height"], viewport_height) - clip_y
    if clip_height <= 0:
        return None

    clip = {"x": box["x"], "y": clip_y, "width": box["width"], "height": clip_height}
    mouse_x = box["x"] + box["width"] / 2
    mouse_y = box["y"] + clip_height / 2
    await page.mouse.move(mouse_x, mouse_y)
    await page.wait_for_timeout(300)

    scroll_delta = int(clip_height * 0.7)
    overlap = clip_height - scroll_delta
    max_scrolls = 10
    screenshots = []
    prev_clip_image = None
    all_opened_images = []

    for i in range(max_scrolls):
        current_screenshot = await page.screenshot(type="png", clip=clip)
        current_image = Image.open(io.BytesIO(current_screenshot))
        all_opened_images.append(current_image)

        if prev_clip_image is not None and images_are_same(prev_clip_image, current_image):
            logger.debug("[%s] 滚动第 %d 次后内容未变化，已到底部", platform_name, i)
            break

        screenshots.append(current_screenshot)
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
        return None
    if len(screenshots) == 1:
        return screenshots[0]
    return stitch_screenshots(screenshots, overlap, platform_name)


async def _fallback_screenshot(page: Page, platform_name: str) -> Optional[bytes]:
    """全页截图兜底方案。"""
    try:
        logger.warning("[%s] 回退到全页截图", platform_name)
        return await page.screenshot(type="png")
    except Exception as e:
        logger.error("全页截图失败: %s", e)
        return None
