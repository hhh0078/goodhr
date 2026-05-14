"""
GoodHR 自动化工具 - Boss直聘登录脚本

一键启动 CloakBrowser 隐身浏览器，打开 Boss直聘登录页面，
等待用户扫码登录，登录成功后自动保存会话，后续任务无需重复登录。

使用方法：
    cd goodhrpy
    source .venv/bin/activate
    python login.py              # 登录默认 Boss 直聘
    python login.py --platform lagou  # 登录拉勾网
    python login.py --headless   # 无头模式（不推荐登录时使用）
"""

import argparse
import asyncio
import sys
from pathlib import Path

from cloakbrowser import launch_persistent_context_async
from playwright.async_api import BrowserContext

from core.settings import config
from utils.logger import get_logger, setup_logger

logger = get_logger("login")

_PROJECT_ROOT = Path(__file__).resolve().parent
_DATA_DIR = _PROJECT_ROOT / "data"

PLATFORMS = {
    "boss": {
        "name": "Boss直聘",
        "login_url": "https://www.zhipin.com/web/user/?ka=header-login",
        "home_url": "https://www.zhipin.com",
        "logged_in_check": "document.querySelectorAll('.nav-figure, .user-info, .info-avatar').length > 0",
    },
    "lagou": {
        "name": "拉勾网",
        "login_url": "https://passport.lagou.com/login/login.html",
        "home_url": "https://www.lagou.com",
        "logged_in_check": "document.querySelectorAll('.lg-user, .user-info').length > 0",
    },
    "liepin": {
        "name": "猎聘网",
        "login_url": "https://c.liepin.com/?loginType=1",
        "home_url": "https://www.liepin.com",
        "logged_in_check": "document.querySelectorAll('.user-info, .user-avatar').length > 0",
    },
    "zhaopin": {
        "name": "智联招聘",
        "login_url": "https://passport.zhaopin.com/login",
        "home_url": "https://www.zhaopin.com",
        "logged_in_check": "document.querySelectorAll('.user-info, .user-name').length > 0",
    },
}


async def do_login(platform_key: str, headless: bool) -> bool:
    """
    执行登录流程

    启动持久化浏览器 → 打开登录页 → 等待用户扫码 → 保存会话

    Args:
        platform_key: 平台标识（boss/lagou/liepin/zhaopin）
        headless: 是否使用无头模式

    Returns:
        bool: 是否登录成功
    """
    platform = PLATFORMS.get(platform_key)
    if not platform:
        logger.error(f"不支持的平台: {platform_key}")
        return False

    profile_dir = str(_DATA_DIR / "profiles" / platform_key)
    user_data_dir = Path(profile_dir)
    user_data_dir.mkdir(parents=True, exist_ok=True)

    logger.info(f"正在启动浏览器，打开 {platform['name']} 登录页面...")
    logger.info("请使用手机扫码登录")

    context: BrowserContext | None = None

    try:
        context = await launch_persistent_context_async(
            user_data_dir=profile_dir,
            headless=headless,
            humanize=True,
        )

        if len(context.pages) > 0:
            page = context.pages[0]
        else:
            page = await context.new_page()

        await page.goto(platform["login_url"], wait_until="domcontentloaded", timeout=30000)
        logger.info(f"已打开登录页面: {platform['login_url']}")

        print("\n" + "=" * 50)
        print(f"  {platform['name']} 登录")
        print("  请在弹出的浏览器窗口中扫码登录")
        print("  登录成功后此脚本会自动检测")
        print("  按 Ctrl+C 可随时退出")
        print("=" * 50 + "\n")

        max_wait = 180
        for i in range(max_wait):
            try:
                is_logged_in = await page.evaluate(platform["logged_in_check"])
                if is_logged_in:
                    print(f"\n✅ {platform['name']} 登录成功！")
                    logger.info(f"{platform['name']} 登录成功，会话已保存到: {profile_dir}")

                    await page.goto(platform["home_url"], wait_until="domcontentloaded", timeout=15000)
                    await page.wait_for_timeout(2000)
                    print("浏览器保持打开，你可以手动浏览。")
                    print("关闭浏览器窗口或按 Ctrl+C 退出。\n")

                    try:
                        while True:
                            await page.wait_for_timeout(5000)
                    except (KeyboardInterrupt, Exception):
                        pass

                    return True
            except Exception:
                pass

            dots = "." * ((i % 3) + 1)
            print(f"\r  等待登录中{dots}   (已等待 {i+1}s)", end="", flush=True)
            await page.wait_for_timeout(1000)

        print(f"\n❌ 等待超时（{max_wait}秒），请重新运行脚本")
        return False

    except KeyboardInterrupt:
        print("\n\n用户中断，浏览器将关闭")
        return False
    except Exception as e:
        logger.error(f"登录过程出错: {e}")
        print(f"\n❌ 出错: {e}")
        return False
    finally:
        if context:
            try:
                await context.close()
            except Exception:
                pass


def main():
    """登录脚本入口"""
    parser = argparse.ArgumentParser(description="GoodHR - 平台登录工具")
    parser.add_argument(
        "--platform", "-p",
        default="boss",
        choices=list(PLATFORMS.keys()),
        help="要登录的平台（默认 boss）",
    )
    parser.add_argument(
        "--headless",
        action="store_true",
        help="无头模式（不弹窗，登录时不建议使用）",
    )
    args = parser.parse_args()

    setup_logger(log_dir=_DATA_DIR / "logs")

    success = asyncio.run(do_login(args.platform, args.headless))

    if success:
        print("\n📝 会话已保存，后续运行筛选任务时将自动使用此登录状态。")
    else:
        print("\n⚠️ 登录未完成，请重新运行脚本。")

    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
