"""
GoodHR 自动化工具 - CloakBrowser 浏览器封装

封装 CloakBrowser 的启动、配置和生命周期管理，
提供统一的隐身浏览器实例创建接口。
"""

import os
import subprocess
from pathlib import Path
from typing import Optional

from cloakbrowser import launch_async as _cloak_launch_async
from cloakbrowser import launch_persistent_context_async as _cloak_persistent_async
from playwright.async_api import Browser, BrowserContext, Page

from core.settings import BrowserConfig, config
from utils.logger import get_logger

logger = get_logger("browser")


def _cleanup_profile_lock(user_data_dir: str) -> None:
    """
    清理浏览器 profile 目录中的锁文件

    Chrome/Chromium 启动时会在 profile 目录创建 SingletonLock 等文件，
    如果浏览器异常退出，这些锁文件会残留，导致新实例无法启动。

    Args:
        user_data_dir: 浏览器用户数据目录路径
    """
    lock_files = ["SingletonLock", "SingletonSocket", "SingletonCookie"]
    profile_dir = Path(user_data_dir)
    for lock_name in lock_files:
        lock_path = profile_dir / lock_name
        if lock_path.exists():
            try:
                lock_path.unlink()
                logger.info(f"已清理锁文件: {lock_path}")
            except OSError as e:
                logger.warning(f"清理锁文件失败 {lock_path}: {e}")


def _kill_orphan_chromium(user_data_dir: str) -> None:
    """
    终止占用指定 profile 目录的残留 Chromium 及 Playwright 进程

    当浏览器异常退出后，Chromium 子进程和 Playwright driver 进程可能仍在运行，
    导致新实例无法绑定同一 profile 目录，且持续输出日志。
    使用 pgrep 查找所有匹配进程并逐个终止。

    Args:
        user_data_dir: 浏览器用户数据目录路径
    """
    try:
        result = subprocess.run(
            ["pgrep", "-af", user_data_dir],
            capture_output=True, text=True, timeout=5,
        )
        pids_to_kill = []
        for line in result.stdout.strip().split("\n"):
            if not line:
                continue
            parts = line.split()
            if not parts:
                continue
            if "chrom" in line.lower() or "playwright" in line.lower():
                try:
                    pid = int(parts[0])
                    pids_to_kill.append(pid)
                except ValueError:
                    continue

        if not pids_to_kill:
            return

        for pid in pids_to_kill:
            try:
                os.kill(pid, 9)
                logger.info(f"已终止残留浏览器进程: PID {pid}")
            except (ProcessLookupError, PermissionError):
                pass

        import time
        time.sleep(0.5)

        for pid in pids_to_kill:
            try:
                os.kill(pid, 0)
                logger.warning(f"进程 {pid} 仍存活，尝试通过 subprocess 终止")
                subprocess.run(["kill", "-9", str(pid)], capture_output=True, timeout=3)
            except (ProcessLookupError, OSError):
                pass

    except subprocess.TimeoutExpired:
        logger.warning("查找残留进程超时")
    except Exception as e:
        logger.warning(f"检查残留进程时出错: {e}")


def _kill_all_cloakbrowser_chromium() -> None:
    """
    终止所有 CloakBrowser 启动的 Chromium 进程

    CloakBrowser 的 Chromium 安装在项目 data/browser/ 目录下，
    通过匹配该路径下的 Chromium 进程来清理所有残留实例。
    这比 _kill_orphan_chromium 更彻底，能清理命令行参数中
    不包含 profile 目录路径的子进程（如 GPU 进程、渲染进程等）。
    """
    browser_dir = str(config.data_dir / "browser")
    try:
        result = subprocess.run(
            ["pgrep", "-af", browser_dir],
            capture_output=True, text=True, timeout=5,
        )
        pids_to_kill = []
        for line in result.stdout.strip().split("\n"):
            if not line:
                continue
            parts = line.split()
            if not parts:
                continue
            try:
                pid = int(parts[0])
                pids_to_kill.append(pid)
            except ValueError:
                continue

        if not pids_to_kill:
            logger.info("未发现 CloakBrowser Chromium 残留进程")
            return

        logger.warning(f"发现 {len(pids_to_kill)} 个残留 Chromium 进程，正在清理: {pids_to_kill}")
        for pid in pids_to_kill:
            try:
                os.kill(pid, 9)
            except (ProcessLookupError, PermissionError):
                pass

        import time
        time.sleep(0.5)

        still_alive = []
        for pid in pids_to_kill:
            try:
                os.kill(pid, 0)
                still_alive.append(pid)
            except (ProcessLookupError, OSError):
                pass

        if still_alive:
            logger.warning(f"以下进程仍存活: {still_alive}")
    except subprocess.TimeoutExpired:
        logger.warning("查找 Chromium 进程超时")
    except Exception as e:
        logger.warning(f"清理 Chromium 进程时出错: {e}")


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
        "viewport": {"width": cfg.viewport_width, "height": cfg.viewport_height},
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
    cfg = browser_config or config.browser

    if not user_data_dir:
        user_data_dir = str(config.data_dir / "profiles" / "boss")

    logger.info(f"正在启动持久化 CloakBrowser (data_dir={user_data_dir})")

    _cleanup_profile_lock(user_data_dir)

    _kill_orphan_chromium(user_data_dir)

    kwargs = {
        "user_data_dir": user_data_dir,
        "headless": cfg.headless,
        "humanize": cfg.humanize,
        "viewport": {"width": cfg.viewport_width, "height": cfg.viewport_height},
    }

    if cfg.human_preset and cfg.human_preset != "default":
        kwargs["human_preset"] = cfg.human_preset

    if cfg.proxy:
        kwargs["proxy"] = cfg.proxy

    context = await _cloak_persistent_async(**kwargs)
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
        self._last_user_data_dir: Optional[str] = None

    async def start(self, persistent: bool = False, user_data_dir: Optional[str] = None) -> None:
        """
        启动浏览器

        Args:
            persistent: 是否使用持久化模式
            user_data_dir: 用户数据目录（持久化模式必须指定）
        """
        if self._browser or self._context:
            logger.warning("浏览器已在运行中，先关闭旧实例")
            await self.stop()

        self._last_user_data_dir = user_data_dir

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
        """
        停止并关闭浏览器，清理所有资源

        按顺序执行：关闭页面 → 关闭上下文/浏览器 → 清理残留进程 → 清理状态。
        每一步都独立 try-except，确保某步失败不影响后续清理。
        使用 _kill_all_cloakbrowser_chromium 彻底清理所有 Chromium 子进程。
        """
        for name, page in self._pages.items():
            try:
                await page.close()
            except Exception:
                pass
        self._pages.clear()

        user_data_dir = self._last_user_data_dir

        if self._context:
            try:
                await self._context.close()
            except Exception:
                pass
            self._context = None

        if self._browser:
            try:
                await self._browser.close()
            except Exception:
                pass
            self._browser = None

        if user_data_dir:
            _kill_orphan_chromium(user_data_dir)
        else:
            _kill_all_cloakbrowser_chromium()

        if user_data_dir:
            _cleanup_profile_lock(user_data_dir)

        self._last_user_data_dir = None

        logger.info("浏览器已关闭")
