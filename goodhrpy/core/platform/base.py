"""
GoodHR 自动化工具 - 平台解析器基类

定义所有招聘平台解析器的抽象接口和通用方法。
各平台解析器继承此基类，实现平台特定的 DOM 提取和操作逻辑。
"""

import io
import random
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import List, Optional

from PIL import Image
from playwright.async_api import Page

from utils.logger import get_logger


def _compute_strip_diff(strip1: Image.Image, strip2: Image.Image) -> float:
    """
    计算两个图片条带的像素差异值

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

logger = get_logger("platform")


@dataclass
class CandidateInfo:
    """
    候选人信息数据类

    存储从页面提取的单个候选人信息，
    不同平台解析器填充不同的字段。
    """

    name: str = ""
    age: str = ""
    education: str = ""
    experience: str = ""
    skills: str = ""
    salary: str = ""
    raw_text: str = ""
    element_index: int = -1
    platform_user_id: str = ""


@dataclass
class PlatformConfig:
    """
    平台配置数据类

    定义平台页面上的 CSS 选择器映射，
    用于定位候选人卡片、按钮等 DOM 元素。
    """

    id: str = ""
    name: str = ""
    domain: str = ""

    card_container: str = ""
    card_selectors: List[str] = field(default_factory=list)
    name_selector: str = ""
    basic_info_selectors: List[str] = field(default_factory=list)
    education_selectors: List[str] = field(default_factory=list)
    university_selector: str = ""
    description_selector: str = ""

    greet_btn_selectors: List[str] = field(default_factory=list)
    continue_btn_selectors: List[str] = field(default_factory=list)

    detail_open_selectors: List[str] = field(default_factory=list)
    detail_close_selectors: List[str] = field(default_factory=list)
    detail_modal_selectors: List[str] = field(default_factory=list)

    extra_selectors: List[dict] = field(default_factory=list)


class BaseParser(ABC):
    """
    平台解析器抽象基类

    提供候选人提取、打招呼、详情页操作等通用方法框架，
    子类需实现平台特定的选择器和逻辑。
    """

    platform_id: str = ""
    platform_name: str = ""

    def __init__(self, config: PlatformConfig):
        """
        初始化解析器

        Args:
            config: 平台配置（CSS 选择器等）
        """
        self.config = config

    def is_current_platform(self, url: str) -> bool:
        """
        判断当前 URL 是否属于此平台

        Args:
            url: 页面 URL

        Returns:
            bool: 是否匹配此平台
        """
        return self.config.domain in url

    @abstractmethod
    def get_entry_url(self, position_name: str = "") -> str:
        """
        获取此平台的候选人推荐页入口 URL

        Args:
            position_name: 岗位名称（部分平台需要）

        Returns:
            str: 入口页面 URL
        """
        ...

    @abstractmethod
    async def ensure_on_page(self, page: Page, timeout: int = 60) -> bool:
        """
        确保页面在推荐页，每秒判断页面路由

        先导航到推荐页入口 URL，然后每秒检查当前 URL 是否符合推荐页特征。
        如果已登录，页面会自动跳转到推荐页；如果未登录，会停留在登录页。
        超时未跳转则返回 False。

        Args:
            page: Playwright Page 实例
            timeout: 超时秒数，默认 60 秒

        Returns:
            bool: 是否成功进入推荐页
        """
        ...

    @abstractmethod
    async def check_login_status(self, page: Page) -> bool:
        """
        检查是否已登录当前平台

        通过判断页面 URL 或 DOM 元素确认登录状态。

        Args:
            page: Playwright Page 实例

        Returns:
            bool: 是否已登录
        """
        ...

    @abstractmethod
    async def wait_for_login(self, page: Page, timeout: int = 120000) -> bool:
        """
        等待用户手动登录

        打开登录页面后等待用户扫码登录，
        检测到登录成功后返回 True。

        Args:
            page: Playwright Page 实例
            timeout: 等待超时（毫秒），默认 2 分钟

        Returns:
            bool: 是否登录成功
        """
        ...

    @abstractmethod
    async def extract_candidates(self, page: Page) -> List[CandidateInfo]:
        """
        从当前页面提取所有候选人信息

        通过 page.evaluate() 执行 JS 读取 DOM，
        提取候选人卡片中的姓名、学历、经验等信息。

        Args:
            page: Playwright Page 实例

        Returns:
            List[CandidateInfo]: 提取到的候选人信息列表
        """
        ...

    @abstractmethod
    async def click_greet(self, page: Page, candidate_index: int) -> bool:
        """
        点击指定候选人的打招呼按钮

        Args:
            page: Playwright Page 实例
            candidate_index: 候选人在列表中的索引

        Returns:
            bool: 是否成功打招呼
        """
        ...

    async def open_detail(self, page: Page, candidate_index: int) -> Optional[str]:
        """
        打开候选人详情页并提取详细信息

        Args:
            page: Playwright Page 实例
            candidate_index: 候选人索引

        Returns:
            Optional[str]: 详情页额外信息文本，无需详情页则返回 None
        """
        return None

    async def close_detail(self, page: Page) -> bool:
        """
        关闭候选人详情页

        Args:
            page: Playwright Page 实例

        Returns:
            bool: 是否成功关闭
        """
        return True

    async def screenshot_detail(self, page: Page) -> Optional[bytes]:
        """
        对候选人详情弹框截图，只截弹框区域而非整个页面

        当弹框内容超出视口时，通过模拟鼠标滚轮在弹框内部滚动，
        逐段截图后用 Pillow 拼接成完整的弹框截图。
        子类可重写此方法，提供平台特定的弹框定位逻辑。

        Args:
            page: Playwright Page 实例

        Returns:
            Optional[bytes]: PNG 格式的截图字节数据，截图失败返回 None
        """
        modal_selectors = self.config.detail_modal_selectors
        if not modal_selectors:
            return await self._fallback_screenshot(page)

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

                is_full_overlay = box["width"] >= vw * 0.9 and box["height"] >= vh * 0.9
                if is_full_overlay:
                    logger.debug(f"[{self.platform_name}] 选择器 {selector} 匹配到全屏遮罩层，跳过")
                    continue

                needs_scroll = box["y"] + box["height"] > vh
                logger.info(
                    f"[{self.platform_name}] 弹框定位: 选择器={selector},"
                    f" box=({int(box['x'])},{int(box['y'])},{int(box['width'])},{int(box['height'])}),"
                    f" 视口={vw}x{vh}, 需要滚动={needs_scroll}"
                )

                if not needs_scroll:
                    screenshot_bytes = await locator.screenshot(type="png")
                else:
                    screenshot_bytes = await self._scroll_and_stitch(page, locator, box, vh)

                if screenshot_bytes:
                    logger.info(f"[{self.platform_name}] 详情弹框截图成功（选择器: {selector}）")
                    return screenshot_bytes
            except Exception as e:
                logger.warning(f"[{self.platform_name}] 选择器 {selector} 截图失败: {e}")
                continue

        logger.warning(f"[{self.platform_name}] 所有选择器均未匹配到弹框内容区域")
        return await self._fallback_screenshot(page)

    async def _scroll_and_stitch(
        self, page: Page, locator, box: dict, viewport_height: int
    ) -> Optional[bytes]:
        """通过鼠标滚轮滚动逐段截图后拼接成完整弹框截图，box 为页面坐标"""
        clip_y = max(box["y"], 0)
        clip_height = min(box["y"] + box["height"], viewport_height) - clip_y
        if clip_height <= 0:
            return None

        clip = {
            "x": box["x"],
            "y": clip_y,
            "width": box["width"],
            "height": clip_height,
        }

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

            if prev_clip_image is not None:
                try:
                    if self._images_are_same(prev_clip_image, current_image):
                        logger.debug(f"[{self.platform_name}] 滚动第 {i} 次后内容未变化，已到底部")
                        break
                finally:
                    pass

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

        return self._stitch_screenshots(screenshots, overlap)

    @staticmethod
    def _images_are_same(img1: Image.Image, img2: Image.Image, threshold: float = 0.98) -> bool:
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

    def _stitch_screenshots(
        self, screenshot_bytes_list: list, overlap_pixels: int
    ) -> Optional[bytes]:
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
                new_result = self._merge_two(result, images[i], overlap_pixels)
                if i > 1:
                    result.close()
                result = new_result
                images[i].close()
            images[0].close()

            output = io.BytesIO()
            result.save(output, format="PNG")
            logger.info(
                f"[{self.platform_name}] 截图拼接完成"
                f"（{len(images)} 张，总高度 {result.height}px）"
            )
            result_bytes = output.getvalue()
            result.close()
            return result_bytes
        except Exception as e:
            logger.warning(f"截图拼接失败: {e}")
            return screenshot_bytes_list[0] if screenshot_bytes_list else None

    @staticmethod
    def _merge_two(
        top_img: Image.Image, bottom_img: Image.Image, max_overlap: int
    ) -> Image.Image:
        search_range = min(max_overlap + 50, top_img.height - 1, bottom_img.height - 1)
        strip_height = min(30, bottom_img.height - 1)

        bottom_strip = bottom_img.crop((0, 0, bottom_img.width, strip_height))

        best_y = top_img.height - max_overlap
        best_diff = float("inf")

        for y in range(max(top_img.height - search_range, 0), top_img.height - strip_height + 1):
            top_strip = top_img.crop((0, y, top_img.width, y + strip_height))
            diff = _compute_strip_diff(top_strip, bottom_strip)
            top_strip.close()
            if diff < best_diff:
                best_diff = diff
                best_y = y

        bottom_strip.close()

        merged = Image.new("RGB", (top_img.width, best_y + bottom_img.height), (255, 255, 255))
        merged.paste(top_img, (0, 0))
        merged.paste(bottom_img, (0, best_y))
        return merged

    async def _fallback_screenshot(self, page: Page) -> Optional[bytes]:
        """
        截图兜底方案：全页截图

        当所有弹框选择器都无法匹配时，回退到截取整个页面。

        Args:
            page: Playwright Page 实例

        Returns:
            Optional[bytes]: PNG 格式的截图字节数据，截图失败返回 None
        """
        try:
            logger.warning(f"[{self.platform_name}] 回退到全页截图")
            return await page.screenshot(type="png")
        except Exception as e:
            logger.error(f"全页截图失败: {e}")
            return None

    async def navigate_to_recommend(self, page: Page, position_name: str = "") -> bool:
        """
        导航到候选人推荐页面

        Args:
            page: Playwright Page 实例
            position_name: 岗位名称

        Returns:
            bool: 是否成功导航
        """
        url = self.get_entry_url(position_name)
        if not url:
            return False

        try:
            await page.goto(url, wait_until="domcontentloaded", timeout=30000)
            await page.wait_for_timeout(2000)
            logger.info(f"已导航到 {self.platform_name} 推荐页")
            return True
        except Exception as e:
            logger.error(f"导航到推荐页失败: {e}")
            return False

    async def wait_for_cards(self, page: Page, timeout: int = 10000) -> bool:
        """
        等待候选人卡片加载

        Args:
            page: Playwright Page 实例
            timeout: 超时时间（毫秒）

        Returns:
            bool: 是否成功加载
        """
        if not self.config.card_selectors:
            return True

        for selector in self.config.card_selectors:
            try:
                await page.locator(selector).first.wait_for(state="visible", timeout=timeout)
                return True
            except Exception:
                continue

        logger.warning("候选人卡片未加载")
        return False

    async def click_box_random_point(
        self,
        page: Page,
        box: dict,
        label: str = "元素",
        min_ratio: float = 0.3,
        max_ratio: float = 0.7,
    ) -> bool:
        """
        在元素盒子内部随机点击一个点。

        默认在元素宽高 30%-70% 区域内取点，这是默认真人点击位置随机性：
        不总是精确点击中心，也不贴近边缘。
        """
        if not box or box.get("width", 0) <= 0 or box.get("height", 0) <= 0:
            return False

        ratio_x = random.uniform(min_ratio, max_ratio)
        ratio_y = random.uniform(min_ratio, max_ratio)
        x = box["x"] + box["width"] * ratio_x
        y = box["y"] + box["height"] * ratio_y

        await page.mouse.move(x, y)
        await page.mouse.click(x, y)
        logger.debug(
            f"[{self.platform_name}] 随机点击{label}: "
            f"point=({x:.1f},{y:.1f}), ratio=({ratio_x:.2f},{ratio_y:.2f})"
        )
        return True
