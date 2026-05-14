"""
GoodHR 自动化工具 - 平台解析器基类

定义所有招聘平台解析器的抽象接口和通用方法。
各平台解析器继承此基类，实现平台特定的 DOM 提取和操作逻辑。
"""

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import List, Optional

from playwright.async_api import Page

from utils.logger import get_logger

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
