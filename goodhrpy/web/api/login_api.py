"""
GoodHR 自动化工具 - 平台登录 API

提供平台登录相关的 HTTP 接口，
支持启动登录浏览器、查询登录状态、注销登录会话。
"""

import asyncio
from pathlib import Path
from typing import Optional

from fastapi import APIRouter, Query

from core.settings import config
from utils.logger import get_logger

logger = get_logger("login_api")

router = APIRouter()

_PLATFORMS = {
    "boss": {
        "name": "Boss直聘",
        "domain": "zhipin.com",
        "login_url": "https://www.zhipin.com/web/user/?ka=header-login",
        "home_url": "https://www.zhipin.com",
        "logged_in_check": "document.querySelectorAll('.nav-figure, .user-info, .info-avatar').length > 0",
    },
    "lagou": {
        "name": "拉勾网",
        "domain": "lagou.com",
        "login_url": "https://passport.lagou.com/login/login.html",
        "home_url": "https://www.lagou.com",
        "logged_in_check": "document.querySelectorAll('.lg-user, .user-info').length > 0",
    },
    "liepin": {
        "name": "猎聘网",
        "domain": "liepin.com",
        "login_url": "https://c.liepin.com/?loginType=1",
        "home_url": "https://www.liepin.com",
        "logged_in_check": "document.querySelectorAll('.user-info, .user-avatar').length > 0",
    },
    "zhaopin": {
        "name": "智联招聘",
        "domain": "zhaopin.com",
        "login_url": "https://passport.zhaopin.com/org/login?validateCampus=",
        "home_url": "https://www.zhaopin.com",
        "logged_in_check": "document.querySelectorAll('.user-info, .user-name').length > 0",
    },
}

_login_task: Optional[asyncio.Task] = None
_login_status = {"platform": None, "status": "idle", "message": ""}


@router.get("/platforms")
async def list_platforms():
    """
    列出所有支持的平台及登录状态

    Returns:
        dict: 平台列表，每个平台包含 id、name、logged_in 字段
    """
    results = []
    for pid, pinfo in _PLATFORMS.items():
        profile_dir = config.data_dir / "profiles" / pid
        has_cookies = (profile_dir / "Default" / "Cookies").exists() if profile_dir.exists() else False
        results.append({
            "id": pid,
            "name": pinfo["name"],
            "domain": pinfo["domain"],
            "logged_in": has_cookies,
            "login_url": pinfo["login_url"],
        })
    return results


@router.post("/login")
async def start_login(platform: str = Query(default="boss", description="平台标识")):
    """
    启动登录浏览器

    弹出 CloakBrowser 窗口，打开指定平台的登录页面，
    用户扫码登录后自动保存会话。此接口立即返回，
    登录状态通过 /status 接口轮询。

    Args:
        platform: 平台标识（boss/lagou/liepin/zhaopin）

    Returns:
        dict: 操作结果
    """
    global _login_task, _login_status

    if platform not in _PLATFORMS:
        return {"ok": False, "msg": f"不支持的平台: {platform}"}

    if _login_status["status"] == "logging_in":
        return {"ok": False, "msg": "已有登录任务在进行中，请等待完成或刷新页面"}

    _login_status = {
        "platform": platform,
        "status": "logging_in",
        "message": f"正在启动浏览器，请扫码登录 {_PLATFORMS[platform]['name']}...",
    }

    _login_task = asyncio.create_task(_do_login(platform))
    return {"ok": True, "msg": f"登录浏览器已启动，请在弹出的窗口中扫码登录"}


@router.get("/status")
async def login_status():
    """
    查询当前登录任务状态

    Returns:
        dict: 登录状态信息
    """
    return _login_status


@router.post("/logout")
async def logout(platform: str = Query(default="boss", description="平台标识")):
    """
    注销指定平台的登录会话

    删除保存的 Cookie 和浏览器会话数据。

    Args:
        platform: 平台标识

    Returns:
        dict: 操作结果
    """
    import shutil

    global _login_status

    profile_dir = config.data_dir / "profiles" / platform
    if profile_dir.exists():
        shutil.rmtree(profile_dir, ignore_errors=True)

    _login_status = {"platform": None, "status": "idle", "message": ""}
    return {"ok": True, "msg": f"{_PLATFORMS.get(platform, {}).get('name', platform)} 已注销"}


@router.post("/open")
async def open_platform(platform: str = Query(default="boss", description="平台标识")):
    """
    使用已保存的会话打开平台网站

    用持久化浏览器加载已保存的 Cookie，直接进入已登录的平台页面。
    浏览器窗口保持打开，用户可手动操作，关闭窗口即退出。

    Args:
        platform: 平台标识

    Returns:
        dict: 操作结果
    """
    pinfo = _PLATFORMS.get(platform)
    if not pinfo:
        return {"ok": False, "msg": f"不支持的平台: {platform}"}

    profile_dir = config.data_dir / "profiles" / platform
    if not (profile_dir / "Default" / "Cookies").exists():
        return {"ok": False, "msg": f"未登录 {_PLATFORMS[platform]['name']}，请先扫码登录"}

    async def _open_browser():
        """后台启动浏览器，加载已保存会话"""
        try:
            from cloakbrowser import launch_persistent_context_async

            context = await launch_persistent_context_async(
                user_data_dir=str(profile_dir),
                headless=False,
                humanize=True,
                viewport={"width": 1280, "height": 800},
            )

            if len(context.pages) > 0:
                page = context.pages[0]
            else:
                page = await context.new_page()

            await page.goto(pinfo["home_url"], wait_until="domcontentloaded", timeout=30000)
            logger.info(f"已打开 {pinfo['name']}，浏览器保持运行")

            try:
                while True:
                    await asyncio.sleep(5)
            except asyncio.CancelledError:
                pass
            finally:
                await context.close()
                logger.info(f"{pinfo['name']} 浏览器已关闭")

        except Exception as e:
            logger.error(f"打开平台出错: {e}")

    asyncio.create_task(_open_browser())
    return {"ok": True, "msg": f"正在打开 {_PLATFORMS[platform]['name']}，请在弹出的浏览器中查看"}


async def _do_login(platform: str) -> None:
    """
    执行异步登录流程（后台任务）

    启动持久化浏览器 → 打开登录页 → 等待扫码 → 保存会话

    Args:
        platform: 平台标识
    """
    global _login_status

    pinfo = _PLATFORMS[platform]
    profile_dir = str(config.data_dir / "profiles" / platform)
    Path(profile_dir).mkdir(parents=True, exist_ok=True)

    try:
        from cloakbrowser import launch_persistent_context_async

        context = await launch_persistent_context_async(
            user_data_dir=profile_dir,
            headless=False,
            humanize=True,
            viewport={"width": 1280, "height": 800},
        )

        if len(context.pages) > 0:
            page = context.pages[0]
        else:
            page = await context.new_page()

        await page.goto(pinfo["login_url"], wait_until="domcontentloaded", timeout=30000)

        _login_status["message"] = f"请在浏览器窗口中扫码登录 {_PLATFORMS[platform]['name']}"

        for i in range(180):
            try:
                is_logged_in = await page.evaluate(pinfo["logged_in_check"])
                if is_logged_in:
                    _login_status = {
                        "platform": platform,
                        "status": "logged_in",
                        "message": f"{pinfo['name']} 登录成功！会话已保存。",
                    }
                    logger.info(f"{pinfo['name']} 登录成功")

                    await page.goto(pinfo["home_url"], wait_until="domcontentloaded", timeout=15000)
                    await asyncio.sleep(3)
                    await context.close()
                    return
            except Exception:
                pass

            await asyncio.sleep(1)

        _login_status = {
            "platform": platform,
            "status": "timeout",
            "message": "登录等待超时，请重新尝试",
        }
        await context.close()

    except Exception as e:
        _login_status = {
            "platform": platform,
            "status": "error",
            "message": f"登录出错: {str(e)}",
        }
        logger.error(f"登录出错: {e}")
