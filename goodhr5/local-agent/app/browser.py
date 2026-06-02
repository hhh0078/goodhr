"""
本文件负责封装 CloakBrowser 浏览器的启动、配置和生命周期管理。

提供统一的隐身浏览器实例创建接口，以及浏览器生命周期管理器 BrowserManager。
沿用 goodhrpy 的已验证可用代码，迁入 GoodHR 5 Local Agent。
"""

from __future__ import annotations

import asyncio
import logging
import os
import platform
import subprocess
import time
from pathlib import Path
from typing import Optional

from cloakbrowser import launch_async as _cloak_launch_async
from cloakbrowser import launch_persistent_context_async as _cloak_persistent_async
from playwright.async_api import Browser, BrowserContext, Page
from playwright._impl._errors import TargetClosedError

logger = logging.getLogger("goodhr5.browser")

# ---------- 浏览器默认配置 ----------

DEFAULT_VIEWPORT_WIDTH = 1280
DEFAULT_VIEWPORT_HEIGHT = 800


# ---------- profile 锁文件清理 ----------


def _cleanup_profile_lock(user_data_dir: str) -> None:
    """
    清理浏览器 profile 目录中的锁文件。

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
                logger.info("已清理锁文件: %s", lock_path)
            except OSError as e:
                logger.warning("清理锁文件失败 %s: %s", lock_path, e)


def _kill_orphan_chromium(user_data_dir: str) -> None:
    """
    终止占用指定 profile 目录的残留 Chromium 及 Playwright 进程。

    当浏览器异常退出后，Chromium 子进程和 Playwright driver 进程可能仍在运行，
    导致新实例无法绑定同一 profile 目录，且持续输出日志。
    使用 pgrep 查找所有匹配进程并逐个终止。

    Args:
        user_data_dir: 浏览器用户数据目录路径
    """
    if platform.system().lower() == "windows":
        logger.info("Windows 环境跳过 pgrep 残留进程清理: %s", user_data_dir)
        return
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
                logger.info("已终止残留浏览器进程: PID %d", pid)
            except (ProcessLookupError, PermissionError):
                pass

        time.sleep(0.5)

        for pid in pids_to_kill:
            try:
                os.kill(pid, 0)
                logger.warning("进程 %d 仍存活，尝试通过 subprocess 终止", pid)
                subprocess.run(["kill", "-9", str(pid)], capture_output=True, timeout=3)
            except (ProcessLookupError, OSError):
                pass

    except subprocess.TimeoutExpired:
        logger.warning("查找残留进程超时")
    except Exception as e:
        logger.warning("检查残留进程时出错: %s", e)


def _kill_all_cloakbrowser_chromium(browser_dir: str) -> None:
    """
    终止所有 CloakBrowser 启动的 Chromium 进程。

    CloakBrowser 的 Chromium 安装在其数据目录下，
    通过匹配该路径下的 Chromium 进程来清理所有残留实例。
    这比 _kill_orphan_chromium 更彻底，能清理命令行参数中
    不包含 profile 目录路径的子进程（如 GPU 进程、渲染进程等）。

    Args:
        browser_dir: CloakBrowser 的 Chromium 安装目录路径
    """
    if platform.system().lower() == "windows":
        logger.info("Windows 环境跳过 pgrep CloakBrowser 残留进程清理: %s", browser_dir)
        return
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

        logger.warning("发现 %d 个残留 Chromium 进程，正在清理: %s", len(pids_to_kill), pids_to_kill)
        for pid in pids_to_kill:
            try:
                os.kill(pid, 9)
            except (ProcessLookupError, PermissionError):
                pass

        time.sleep(0.5)

        still_alive = []
        for pid in pids_to_kill:
            try:
                os.kill(pid, 0)
                still_alive.append(pid)
            except (ProcessLookupError, OSError):
                pass

        if still_alive:
            logger.warning("以下进程仍存活: %s", still_alive)
    except subprocess.TimeoutExpired:
        logger.warning("查找 Chromium 进程超时")
    except Exception as e:
        logger.warning("清理 Chromium 进程时出错: %s", e)


# ---------- 浏览器创建 ----------


async def create_browser(
    headless: bool = False,
    humanize: bool = True,
    human_preset: str = "default",
    proxy: str = "",
    viewport_width: int = DEFAULT_VIEWPORT_WIDTH,
    viewport_height: int = DEFAULT_VIEWPORT_HEIGHT,
    user_data_dir: Optional[str] = None,
) -> Browser:
    """
    创建 CloakBrowser 隐身浏览器实例。

    基于 CloakBrowser 的 launch_async 方法，自动配置隐身参数和仿真人行为。
    支持代理、持久化登录、自定义浏览器参数。

    Args:
        headless: 是否无头模式运行
        humanize: 是否启用仿真人行为
        human_preset: 仿真人行为预设（default/careful）
        proxy: 代理地址（HTTP/SOCKS5）
        viewport_width: 浏览器视口宽度（像素）
        viewport_height: 浏览器视口高度（像素）
        user_data_dir: 用户数据目录，设置后可保持登录状态（Cookie 持久化）

    Returns:
        Browser: Playwright Browser 实例
    """
    logger.info("正在启动 CloakBrowser (headless=%s, humanize=%s)", headless, humanize)

    kwargs: dict = {
        "headless": headless,
        "humanize": humanize,
        "viewport": {"width": viewport_width, "height": viewport_height},
    }

    if human_preset and human_preset != "default":
        kwargs["human_preset"] = human_preset

    if proxy:
        kwargs["proxy"] = proxy
        logger.info("已配置代理: %s...", proxy[:20])

    if user_data_dir:
        browser = await _cloak_launch_async(**kwargs)
        logger.info("CloakBrowser 已启动（标准模式）")
        return browser

    browser = await _cloak_launch_async(**kwargs)
    logger.info("CloakBrowser 已启动")
    return browser


async def create_persistent_browser(
    user_data_dir: str,
    headless: bool = False,
    humanize: bool = True,
    human_preset: str = "default",
    proxy: str = "",
    viewport_width: int = DEFAULT_VIEWPORT_WIDTH,
    viewport_height: int = DEFAULT_VIEWPORT_HEIGHT,
) -> BrowserContext:
    """
    创建持久化浏览器上下文。

    使用 launch_persistent_context 创建，Cookie 和 localStorage
    跨会话保持，适用于需要持续登录的场景。

    Args:
        user_data_dir: 用户数据目录
        headless: 是否无头模式运行
        humanize: 是否启用仿真人行为
        human_preset: 仿真人行为预设
        proxy: 代理地址
        viewport_width: 视口宽度
        viewport_height: 视口高度

    Returns:
        BrowserContext: 持久化的浏览器上下文
    """
    logger.info("正在启动持久化 CloakBrowser (data_dir=%s)", user_data_dir)

    _cleanup_profile_lock(user_data_dir)
    _kill_orphan_chromium(user_data_dir)

    kwargs: dict = {
        "user_data_dir": user_data_dir,
        "headless": headless,
        "humanize": humanize,
        "viewport": {"width": viewport_width, "height": viewport_height},
    }

    if human_preset and human_preset != "default":
        kwargs["human_preset"] = human_preset

    if proxy:
        kwargs["proxy"] = proxy

    context = await _cloak_persistent_async(**kwargs)
    logger.info("持久化 CloakBrowser 已启动")
    return context


# ---------- 浏览器生命周期管理器 ----------


class BrowserManager:
    """
    浏览器生命周期管理器。

    统一管理浏览器实例的创建、获取和销毁，
    确保同一时间只有一个浏览器实例在运行。
    用于任务中创建和复用浏览器页面。
    """

    def __init__(self, browser_data_dir: str = ""):
        """
        初始化浏览器管理器。

        Args:
            browser_data_dir: CloakBrowser Chromium 安装目录，用于清理残留进程
        """
        self._browser: Optional[Browser] = None
        self._context: Optional[BrowserContext] = None
        self._pages: dict[str, Page] = {}
        self._last_user_data_dir: Optional[str] = None
        self._browser_data_dir = browser_data_dir
        self._closed_callbacks: list = []
        self._closed_notified = False
        self._last_exported_cookies: list[dict] = []
        self._state_lock = asyncio.Lock()

    async def start(
        self,
        persistent: bool = False,
        user_data_dir: Optional[str] = None,
        headless: bool = False,
        humanize: bool = True,
        human_preset: str = "default",
        proxy: str = "",
    ) -> str:
        """
        启动浏览器。

        如果已有实例在运行，会先关闭旧实例再创建新实例。

        Args:
            persistent: 是否使用持久化模式
            user_data_dir: 用户数据目录（持久化模式必须指定）
            headless: 是否无头模式
            humanize: 是否仿真人行为
            human_preset: 仿真人预设
            proxy: 代理地址
        """
        async with self._state_lock:
            if (self._browser or self._context) and user_data_dir and self._last_user_data_dir == user_data_dir:
                logger.info("浏览器已使用相同账号目录运行，复用现有实例: %s", user_data_dir)
                return "already_running"
            if self._browser or self._context:
                logger.warning("浏览器已在运行中，先关闭旧实例")
                await self._stop_unlocked()

            self._last_user_data_dir = user_data_dir
            self._closed_notified = False

            if persistent:
                if not user_data_dir:
                    raise ValueError("持久化模式必须指定 user_data_dir")
                self._context = await create_persistent_browser(
                    user_data_dir=user_data_dir,
                    headless=headless,
                    humanize=humanize,
                    human_preset=human_preset,
                    proxy=proxy,
                )
            else:
                self._browser = await create_browser(
                    headless=headless,
                    humanize=humanize,
                    human_preset=human_preset,
                    proxy=proxy,
                    user_data_dir=user_data_dir,
                )
                try:
                    self._browser.on("disconnected", lambda *_: self._notify_closed("disconnected"))
                except Exception:
                    pass
            if self._context:
                try:
                    self._context.on("close", lambda *_: self._notify_closed("context_closed"))
                except Exception:
                    pass
            return "started"

    def add_closed_callback(self, callback) -> None:
        """注册浏览器关闭回调。"""
        self._closed_callbacks.append(callback)

    def _notify_closed(self, reason: str) -> None:
        """触发浏览器关闭回调（仅触发一次）。"""
        if self._closed_notified:
            return
        self._closed_notified = True
        for callback in list(self._closed_callbacks):
            try:
                result = callback(reason)
                if asyncio.iscoroutine(result):
                    asyncio.create_task(result)
            except Exception:
                pass

    async def new_page(self, name: str = "default") -> Page:
        """
        创建新页面并注册。

        Args:
            name: 页面名称标识

        Returns:
            Page: Playwright Page 实例

        Raises:
            RuntimeError: 浏览器未启动
        """
        async with self._state_lock:
            page = await self._new_page_unlocked(name)

        return page

    async def _new_page_unlocked(self, name: str = "default") -> Page:
        """
        在已持有状态锁时创建新页面。

        Args:
            name: 页面名称标识

        Returns:
            Page: Playwright Page 实例
        """
        try:
            if self._context:
                page = await self._context.new_page()
            elif self._browser:
                page = await self._browser.new_page()
            else:
                raise RuntimeError("浏览器未启动，请先调用 start()")
        except TargetClosedError as exc:
            self._context = None
            self._browser = None
            self._pages.clear()
            self._notify_closed("target_closed")
            logger.exception("浏览器已关闭，无法创建新页面")
            raise RuntimeError("浏览器启动后已关闭，请检查 CloakBrowser 文件权限或重新启动本地执行器") from exc

        self._pages[name] = page
        logger.info("已创建页面: %s", name)
        return page

    async def get_page(self, name: str = "default") -> Optional[Page]:
        """
        获取已注册的页面。

        Args:
            name: 页面名称标识

        Returns:
            Page 或 None
        """
        page = self._pages.get(name)
        if page is not None and page.is_closed():
            self._pages.pop(name, None)
            return None
        return page

    async def ensure_page(self, name: str = "default") -> Optional[Page]:
        """
        获取页面；浏览器已启动但页面缺失时自动创建。

        Args:
            name: 页面名称标识

        Returns:
            Page 或 None；浏览器未启动时返回 None
        """
        async with self._state_lock:
            page = self._pages.get(name)
            if page is not None and not page.is_closed():
                return page
            self._pages.pop(name, None)
            if self._browser is None and self._context is None:
                return None
            return await self._new_page_unlocked(name)

    async def list_pages(self) -> dict:
        """
        列出当前浏览器中所有未关闭页面。

        Returns:
            dict: 页面列表，包含页面编号、URL、标题和是否默认页面。
        """
        async with self._state_lock:
            pages = self._all_open_pages_unlocked()
            default_page = self._pages.get("default")
            items: list[dict[str, object]] = []
            for index, page in enumerate(pages):
                try:
                    title = await page.title()
                except Exception:
                    title = ""
                items.append(
                    {
                        "page_id": f"page_{index}",
                        "url": page.url or "",
                        "title": title,
                        "is_default": page is default_page,
                    }
                )
            return {"ok": True, "pages": items, "count": len(items)}

    async def use_page(self, page_id: str) -> dict:
        """
        将指定页面设置为默认操作页面。

        Args:
            page_id: 页面编号，格式为 page_0。

        Returns:
            dict: 被设置为默认页面的信息。
        """
        raw_page_id = (page_id or "").strip()
        if not raw_page_id.startswith("page_"):
            raise ValueError("page_id is required")
        try:
            page_index = int(raw_page_id.replace("page_", "", 1))
        except ValueError as exc:
            raise ValueError("page_id is invalid") from exc

        async with self._state_lock:
            pages = self._all_open_pages_unlocked()
            if page_index < 0 or page_index >= len(pages):
                raise ValueError("page_id not found")
            page = pages[page_index]
            self._pages["default"] = page
            try:
                title = await page.title()
            except Exception:
                title = ""
            logger.info("已切换默认页面 page_id=%s url=%s", raw_page_id, page.url)
            return {"ok": True, "page_id": raw_page_id, "url": page.url or "", "title": title}

    async def close_pages_by_url_contains(self, url_contains: str) -> dict:
        """
        关闭 URL 包含指定文本的所有页面。

        Args:
            url_contains: URL 中需要包含的文本。

        Returns:
            dict: 关闭数量和关闭页面 URL 列表。
        """
        keyword = (url_contains or "").strip()
        if not keyword:
            raise ValueError("url_contains is required")

        async with self._state_lock:
            pages = self._all_open_pages_unlocked()
            closed_urls: list[str] = []
            for page in pages:
                if page.is_closed():
                    continue
                url = page.url or ""
                if keyword not in url:
                    continue
                try:
                    await page.close()
                    closed_urls.append(url)
                except Exception as exc:
                    logger.warning("关闭页面失败 url=%s err=%s", url, exc)

            self._pages = {
                name: page
                for name, page in self._pages.items()
                if page is not None and not page.is_closed()
            }
            logger.info("按 URL 关键字关闭页面 keyword=%s closed=%d", keyword, len(closed_urls))
            return {"ok": True, "url_contains": keyword, "closed_count": len(closed_urls), "closed_urls": closed_urls}

    def _all_open_pages_unlocked(self) -> list[Page]:
        """
        读取当前浏览器里的全部未关闭页面。

        Returns:
            list[Page]: 去重后的页面列表。
        """
        pages: list[Page] = []
        seen: set[int] = set()

        def add(page: Page | None) -> None:
            if page is None or page.is_closed():
                return
            marker = id(page)
            if marker in seen:
                return
            seen.add(marker)
            pages.append(page)

        for page in self._pages.values():
            add(page)
        if self._context is not None:
            for page in self._context.pages:
                add(page)
        if self._browser is not None:
            for context in self._browser.contexts:
                for page in context.pages:
                    add(page)
        return pages

    @property
    def is_running(self) -> bool:
        """浏览器是否正在运行。"""
        return self._browser is not None or self._context is not None

    async def stop(self) -> None:
        """
        停止并关闭浏览器，清理所有资源。

        按顺序执行：关闭页面 → 关闭上下文/浏览器 → 清理残留进程 → 清理状态。
        每一步都独立 try-except，确保某步失败不影响后续清理。
        """
        async with self._state_lock:
            await self._stop_unlocked()

    async def _stop_unlocked(self) -> None:
        """
        在已持有状态锁时关闭浏览器并清理状态。

        该方法只由 start/stop 内部调用，避免启动和关闭并发时状态被交叉修改。
        """
        if self._context or self._pages:
            try:
                await self.export_cookies()
                if self._last_exported_cookies:
                    logger.info("浏览器关闭前已缓存 cookies: %d 条", len(self._last_exported_cookies))
            except Exception as exc:
                logger.warning("浏览器关闭前缓存 cookies 失败: %s", exc)

        for name, page in list(self._pages.items()):
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
        elif self._browser_data_dir:
            _kill_all_cloakbrowser_chromium(self._browser_data_dir)

        if user_data_dir:
            _cleanup_profile_lock(user_data_dir)

        self._last_user_data_dir = None

        self._notify_closed("stopped")
        logger.info("浏览器已关闭")

    async def add_cookies(self, cookies: list[dict]) -> None:
        """向当前浏览器上下文注入 cookies。"""
        if not cookies:
            return
        context = self._context
        if context is None and self._pages:
            page = next(iter(self._pages.values()))
            context = page.context if page else None
        if context is None:
            raise RuntimeError("浏览器未启动，无法注入 cookies")
        await context.add_cookies(cookies)
        self._last_exported_cookies = list(cookies)

    async def export_cookies(self) -> list[dict]:
        """导出当前浏览器上下文 cookies。"""
        context = self._context
        if context is None and self._pages:
            page = next(iter(self._pages.values()))
            context = page.context if page else None
        if context is None:
            if self._last_exported_cookies:
                logger.info("浏览器上下文不存在，使用最近缓存 cookies: %d 条", len(self._last_exported_cookies))
            return list(self._last_exported_cookies)
        try:
            cookies = await context.cookies()
        except Exception as exc:
            if self._last_exported_cookies:
                logger.warning("导出 cookies 失败，使用最近缓存 cookies: %s", exc)
                return list(self._last_exported_cookies)
            raise
        self._last_exported_cookies = list(cookies)
        return cookies
