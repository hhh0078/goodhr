"""
GoodHR 自动化工具 - CloakBrowser 浏览器封装

封装 CloakBrowser 的启动、配置和生命周期管理，
提供统一的隐身浏览器实例创建接口。
"""

from typing import Optional

from cloakbrowser import launch as _cloak_launch
from cloakbrowser import launch_async as _cloak_launch_async
from playwright.async_api import Browser, BrowserContext, Page

from core.settings import BrowserConfig, config
from utils.logger import get_logger

logger = get_logger("browser")


async def create_browser(
    browser_config: Optional[BrowserConfig] = None,
    user_data_dir: Optional[str] = None,
) -> Browser:
    """
    创建 CloakBrowser 隐身浏览器实例

    基于 CloakBrowser 的 launch_async 方法，自动配置隐身参数和仿真人行为。
    支持代理、持久化登录、自定义浏览器参数。

    Args:
        browser_config: 浏览器配置，为 None 则使用全局配置
        user_data_dir: 用户数据目录，设置后可保持登录状态（Cookie 持久化）

    Returns:
        Browser: Playwright Browser 实例
    """
    cfg = browser_config or config.browser
    logger.info(f"正在启动 CloakBrowser (headless={cfg.headless}, humanize={cfg.humanize})")

    kwargs = {
        "headless": cfg.headless,
        "humanize": cfg.humanize,
    }

    if cfg.human_preset and cfg.human_preset != "default":
        kwargs["human_preset"] = cfg.human_preset

    if cfg.proxy:
        kwargs["proxy"] = cfg.proxy
        logger.info(f"已配置代理: {cfg.proxy[:20]}...")

    if user_data_dir:
        browser = await _cloak_launch_async(**kwargs)
        logger.info("CloakBrowser 已启动（标准模式）")
        return browser

    browser = await _cloak_launch_async(**kwargs)
    logger.info("CloakBrowser 已启动")
    return browser


async def create_persistent_browser(
    browser_config: Optional[BrowserConfig] = None,
    user_data_dir: Optional[str] = None,
) -> BrowserContext:
    """
    创建持久化浏览器上下文

    使用 launch_persistent_context 创建，Cookie 和 localStorage
    跨会话保持，适用于需要持续登录的场景。

    Args:
        browser_config: 浏览器配置，为 None 则使用全局配置
        user_data_dir: 用户数据目录，默认为项目 data/profiles/default

    Returns:
        BrowserContext: 持久化的浏览器上下文
    """
    from cloakbrowser import launch_persistent_context as _cloak_persistent

    cfg = browser_config or config.browser

    if not user_data_dir:
        user_data_dir = str(config.data_dir / "profiles" / "boss")

    logger.info(f"正在启动持久化 CloakBrowser (data_dir={user_data_dir})")

    kwargs = {
        "user_data_dir": user_data_dir,
        "headless": cfg.headless,
        "humanize": cfg.humanize,
    }

    if cfg.human_preset and cfg.human_preset != "default":
        kwargs["human_preset"] = cfg.human_preset

    if cfg.proxy:
        kwargs["proxy"] = cfg.proxy

    context = await _cloak_persistent(**kwargs)
    logger.info("持久化 CloakBrowser 已启动")
    return context


class BrowserManager:
    """
    浏览器生命周期管理器

    统一管理浏览器实例的创建、获取和销毁，
    确保同一时间只有一个浏览器实例在运行。
    """

    def __init__(self):
        """初始化浏览器管理器"""
        self._browser: Optional[Browser] = None
        self._context: Optional[BrowserContext] = None
        self._pages: dict[str, Page] = {}

    async def start(self, persistent: bool = False, user_data_dir: Optional[str] = None) -> None:
        """
        启动浏览器

        Args:
            persistent: 是否使用持久化模式
            user_data_dir: 用户数据目录（持久化模式必须指定）
        """
        if self._browser or self._context:
            logger.warning("浏览器已在运行中，请先停止")
            return

        if persistent:
            self._context = await create_persistent_browser(user_data_dir=user_data_dir)
        else:
            self._browser = await create_browser(user_data_dir=user_data_dir)

    async def new_page(self, name: str = "default") -> Page:
        """
        创建新页面并注册

        Args:
            name: 页面名称标识

        Returns:
            Page: Playwright Page 实例

        Raises:
            RuntimeError: 浏览器未启动
        """
        if self._context:
            page = await self._context.new_page()
        elif self._browser:
            page = await self._browser.new_page()
        else:
            raise RuntimeError("浏览器未启动，请先调用 start()")

        self._pages[name] = page
        logger.info(f"已创建页面: {name}")
        return page

    async def get_page(self, name: str = "default") -> Optional[Page]:
        """
        获取已注册的页面

        Args:
            name: 页面名称标识

        Returns:
            Page 或 None
        """
        return self._pages.get(name)

    @property
    def is_running(self) -> bool:
        """浏览器是否正在运行"""
        return self._browser is not None or self._context is not None

    async def stop(self) -> None:
        """停止并关闭浏览器，清理所有资源"""
        for name, page in self._pages.items():
            try:
                await page.close()
            except Exception:
                pass
        self._pages.clear()

        if self._browser:
            try:
                await self._browser.close()
            except Exception:
                pass
            self._browser = None

        if self._context:
            try:
                await self._context.close()
            except Exception:
                pass
            self._context = None

        logger.info("浏览器已关闭")
