"""
GoodHR 自动化工具 - 平台登录 API

提供平台登录相关的 HTTP 接口，
支持启动登录浏览器、查询登录状态、注销登录会话。
"""

import asyncio
import json
import uuid
from pathlib import Path
from typing import Optional

from fastapi import APIRouter, Query
from pydantic import BaseModel

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


class PlatformAccountCreate(BaseModel):
    """平台账号创建请求"""

    platform: str
    name: str = ""


def _accounts_file() -> Path:
    return config.data_dir / "accounts.json"


def _load_accounts() -> list[dict]:
    path = _accounts_file()
    if not path.exists():
        return []
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception:
        return []


def _save_accounts(accounts: list[dict]) -> None:
    path = _accounts_file()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(accounts, ensure_ascii=False, indent=2), encoding="utf-8")


def _profile_dir(platform: str, account_id: str) -> Path:
    return config.data_dir / "profiles" / platform / account_id


def _account_profile_dir(account: dict) -> Path:
    profile_dir = account.get("profile_dir")
    if profile_dir:
        return Path(profile_dir)
    return _profile_dir(account.get("platform", ""), account.get("id", ""))


def _has_login_profile(platform: str, account_id: str) -> bool:
    account = _get_account(account_id)
    profile_dir = _account_profile_dir(account) if account else _profile_dir(platform, account_id)
    return (profile_dir / "Default" / "Cookies").exists() if profile_dir.exists() else False


def _ensure_default_account(platform: str) -> Optional[dict]:
    if platform not in _PLATFORMS:
        return None

    accounts = _load_accounts()
    existing = next((a for a in accounts if a.get("platform") == platform), None)
    if existing:
        return existing

    legacy_profile_dir = config.data_dir / "profiles" / platform
    if (legacy_profile_dir / "Default").exists():
        profile_dir = legacy_profile_dir
    else:
        profile_dir = _profile_dir(platform, f"{platform}-default")
    account = {
        "id": f"{platform}-default",
        "platform": platform,
        "name": f"{_PLATFORMS[platform]['name']} 默认账号",
        "profile_dir": str(profile_dir),
    }
    accounts.append(account)
    _save_accounts(accounts)
    return account


def _get_account(account_id: str) -> Optional[dict]:
    return next((a for a in _load_accounts() if a.get("id") == account_id), None)


def _public_account(account: dict) -> dict:
    platform = account.get("platform", "")
    return {
        "id": account.get("id", ""),
        "platform": platform,
        "platform_name": _PLATFORMS.get(platform, {}).get("name", platform),
        "name": account.get("name", ""),
        "logged_in": _has_login_profile(platform, account.get("id", "")),
        "profile_dir": account.get("profile_dir", ""),
    }


@router.get("/platforms")
async def list_platforms():
    """
    列出所有支持的平台及登录状态

    Returns:
        dict: 平台列表，每个平台包含 id、name、logged_in 字段
    """
    results = []
    for pid, pinfo in _PLATFORMS.items():
        platform_accounts = [a for a in _load_accounts() if a.get("platform") == pid]
        has_cookies = any(_has_login_profile(pid, a.get("id", "")) for a in platform_accounts)
        results.append({
            "id": pid,
            "name": pinfo["name"],
            "domain": pinfo["domain"],
            "logged_in": has_cookies,
            "account_count": len(platform_accounts),
            "login_url": pinfo["login_url"],
        })
    return results


@router.get("/accounts")
async def list_accounts(platform: Optional[str] = Query(default=None, description="平台标识")):
    accounts = _load_accounts()
    if platform:
        accounts = [a for a in accounts if a.get("platform") == platform]
    return [_public_account(a) for a in accounts]


@router.post("/accounts")
async def create_account(data: PlatformAccountCreate):
    if data.platform not in _PLATFORMS:
        return {"ok": False, "msg": f"不支持的平台: {data.platform}"}

    account_id = f"{data.platform}-{uuid.uuid4().hex[:8]}"
    account = {
        "id": account_id,
        "platform": data.platform,
        "name": data.name.strip() or f"{_PLATFORMS[data.platform]['name']} 账号",
        "profile_dir": str(_profile_dir(data.platform, account_id)),
    }
    accounts = _load_accounts()
    accounts.append(account)
    _save_accounts(accounts)
    return {"ok": True, "account": _public_account(account)}


@router.post("/login")
async def start_login(
    platform: str = Query(default="boss", description="平台标识"),
    account_id: Optional[str] = Query(default=None, description="账号ID"),
    account_name: Optional[str] = Query(default=None, description="新账号名称"),
):
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

    account = _get_account(account_id) if account_id else None
    if not account:
        if account_name:
            created = await create_account(PlatformAccountCreate(platform=platform, name=account_name))
            account = created.get("account")
        else:
            account = _ensure_default_account(platform)
    if not account:
        return {"ok": False, "msg": "账号创建失败"}
    account_id = account["id"]

    if _login_status["status"] == "logging_in":
        return {"ok": False, "msg": "已有登录任务在进行中，请等待完成或刷新页面"}

    _login_status = {
        "platform": platform,
        "account_id": account_id,
        "status": "logging_in",
        "message": f"正在启动浏览器，请扫码登录 {account['name']}...",
    }

    _login_task = asyncio.create_task(_do_login(platform, account_id))
    return {"ok": True, "msg": "登录浏览器已启动，请在弹出的窗口中扫码登录"}


@router.get("/status")
async def login_status():
    """
    查询当前登录任务状态

    Returns:
        dict: 登录状态信息
    """
    return _login_status


@router.post("/logout")
async def logout(
    platform: str = Query(default="boss", description="平台标识"),
    account_id: Optional[str] = Query(default=None, description="账号ID"),
):
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

    account = _get_account(account_id) if account_id else _ensure_default_account(platform)
    profile_dir = _account_profile_dir(account) if account else config.data_dir / "profiles" / platform
    if profile_dir.exists():
        shutil.rmtree(profile_dir, ignore_errors=True)

    _login_status = {"platform": None, "status": "idle", "message": ""}
    return {"ok": True, "msg": f"{account.get('name', platform) if account else platform} 已注销"}


@router.post("/open")
async def open_platform(
    platform: str = Query(default="boss", description="平台标识"),
    account_id: Optional[str] = Query(default=None, description="账号ID"),
):
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

    account = _get_account(account_id) if account_id else _ensure_default_account(platform)
    if not account:
        return {"ok": False, "msg": "账号不存在"}

    profile_dir = _account_profile_dir(account)
    if not (profile_dir / "Default" / "Cookies").exists():
        return {"ok": False, "msg": f"未登录 {account['name']}，请先扫码登录"}

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
    return {"ok": True, "msg": f"正在打开 {account['name']}，请在弹出的浏览器中查看"}


async def _do_login(platform: str, account_id: str) -> None:
    """
    执行异步登录流程（后台任务）

    启动持久化浏览器 → 打开登录页 → 等待扫码 → 保存会话

    Args:
        platform: 平台标识
    """
    global _login_status

    pinfo = _PLATFORMS[platform]
    account = _get_account(account_id)
    if not account:
        _login_status = {"platform": platform, "status": "error", "message": "账号不存在"}
        return

    profile_dir = str(_account_profile_dir(account))
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

        _login_status["message"] = f"请在浏览器窗口中扫码登录 {account['name']}"

        for i in range(180):
            try:
                is_logged_in = await page.evaluate(pinfo["logged_in_check"])
                if is_logged_in:
                    _login_status = {
                        "platform": platform,
                        "account_id": account_id,
                        "status": "logged_in",
                        "message": f"{account['name']} 登录成功！会话已保存。",
                    }
                    logger.info(f"{account['name']} 登录成功")

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
            "account_id": account_id,
            "status": "error",
            "message": f"登录出错: {str(e)}",
        }
        logger.error(f"登录出错: {e}")
