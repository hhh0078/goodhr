"""
GoodHR 自动化工具 - Boss直聘平台解析器

实现 Boss直聘（zhipin.com）特定的候选人提取和操作逻辑。
Boss直聘推荐牛人页面的 DOM 结构和交互逻辑与其他平台不同，
此解析器封装了 Boss 特有的选择器和操作方法。
"""

import time
from typing import List, Optional

from playwright.async_api import Page

from core.platform.base import BaseParser, CandidateInfo, PlatformConfig
from utils.logger import get_logger

logger = get_logger("boss")

BOSS_CONFIG = PlatformConfig(
    id="boss",
    name="Boss直聘",
    domain="zhipin.com",
    card_container=".card-list",
    card_selectors=[".candidate-card-wrap", ".geek-info-card", ".card-container"],
    name_selector=".name",
    basic_info_selectors=[".job-card-left"],
    education_selectors=[".base-info.join-text-wrap", ".geek-info-detail"],
    university_selector=".content.join-text-wrap",
    description_selector=".content",
    greet_btn_selectors=[".btn.btn-greet", ".btn.btn-getcontact"],
    continue_btn_selectors=[".btn.btn-continue.btn-outline"],
    detail_open_selectors=[
        ".card-inner.common-wrap",
        ".card-inner.clear-fix",
        ".card-inner.blue-collar-wrap",
        ".card-inner.new-geek-wrap",
        ".card-inner.common-wrap.css-type-1",
        ".candidate-card-wrap.css-type-1",
        ".card-container",
        ".geek-info-card",
    ],
    detail_close_selectors=[".boss-popup__close", ".resume-custom-close"],
    detail_modal_selectors=[
        ".resume-custom-container",
        ".boss-popup__content",
        ".detail-content",
        ".resume-detail",
    ],
    extra_selectors=[
        {"selector": ".salary-text", "label": "薪资"},
        {"selector": ".job-info-primary", "label": "基本信息"},
        {"selector": ".tags-wrap", "label": "标签"},
        {"selector": ".content.join-text-wrap", "label": "公司信息"},
    ],
)


class BossParser(BaseParser):
    """
    Boss直聘平台解析器

    Boss直聘使用 iframe 渲染候选人列表和详情弹框，所有 DOM
    查找都需要先定位 iframe 再在其内部操作。_get_frame 做
    3 次 × 500ms 重试应对 iframe 延迟加载。
    """

    platform_id = "boss"
    platform_name = "Boss直聘"

    _IFRAME_SELECTORS = "iframe[src*=\"recommend\"], iframe[src*=\"chat\"]"

    def __init__(self):
        super().__init__(BOSS_CONFIG)
        self._frame = None

    async def _get_frame(self, page: Page) -> Optional[object]:
        if self._frame:
            try:
                await self._frame.evaluate("1")
                return self._frame
            except Exception:
                self._frame = None

        for frame in page.frames:
            if frame == page.main_frame:
                continue
            if "recommend" in frame.url or "chat" in frame.url:
                self._frame = frame
                logger.debug(f"[{self.platform_name}] 找到 iframe: {frame.url[:80]}")
                return frame

        try:
            iframe_locator = page.locator(self._IFRAME_SELECTORS).first
            if await iframe_locator.is_visible(timeout=1000):
                el_handle = await iframe_locator.element_handle()
                if el_handle:
                    frame = await el_handle.content_frame()
                    if frame:
                        self._frame = frame
                        logger.debug(f"[{self.platform_name}] 通过 element_handle 找到 iframe")
                        return frame
        except Exception:
            pass

        return None

    async def _eval(self, page: Page, js_code: str, *args):
        frame = await self._get_frame(page)
        if frame:
            return await frame.evaluate(js_code, *args)
        return await page.evaluate(js_code, *args)

    async def _locate(self, page: Page, selector: str):
        frame = await self._get_frame(page)
        if frame:
            return frame.locator(selector)
        return page.locator(selector)

    async def wait_for_cards(self, page: Page, timeout: int = 10000) -> bool:
        if not self.config.card_selectors:
            return True

        start = time.time()
        while time.time() - start < timeout / 1000:
            js_code = """(selectors) => {
                for (const sel of selectors) {
                    const els = document.querySelectorAll(sel);
                    for (const el of els) {
                        if (el.offsetParent !== null) return true;
                    }
                }
                return false;
            }"""
            try:
                if await self._eval(page, js_code, self.config.card_selectors):
                    return True
            except Exception:
                pass
            await page.wait_for_timeout(500)
        return False

    def get_entry_url(self, position_name: str = "") -> str:
        """
        获取 Boss直聘推荐牛人页入口 URL

        Args:
            position_name: 岗位名称（Boss 直聘不需要，推荐页根据当前登录账号自动匹配）

        Returns:
            str: 推荐牛人页 URL
        """
        return "https://www.zhipin.com/web/chat/recommend"

    async def ensure_on_page(self, page: Page, timeout: int = 60) -> bool:
        """
        确保页面在 Boss直聘推荐页

        先导航到推荐页入口 URL，然后每秒检查当前 URL 是否包含
        zhipin.com/web/chat/recommend（推荐页特征）。如果已登录，
        页面会自动跳转到推荐页；如果未登录，会跳转到登录页。
        超时未跳转则返回 False。

        Args:
            page: Playwright Page 实例
            timeout: 超时秒数，默认 60 秒

        Returns:
            bool: 是否成功进入推荐页
        """
        import time

        entry_url = self.get_entry_url()
        try:
            await page.goto(entry_url, wait_until="domcontentloaded", timeout=30000)
        except Exception as e:
            logger.warning(f"导航到Boss推荐页失败: {e}")

        start_time = time.time()
        while True:
            try:
                current_url = page.url
                if "zhipin.com/web/chat/recommend" in current_url:
                    logger.info(f"Boss直聘推荐页加载完成: {current_url}")
                    await page.wait_for_timeout(3000)
                    return True
            except Exception:
                pass

            elapsed = time.time() - start_time
            if elapsed >= timeout:
                logger.warning(f"Boss直聘推荐页等待超时（{timeout}秒），当前URL: {page.url}")
                return False

            await page.wait_for_timeout(1000)

    async def extract_candidates(self, page: Page) -> List[CandidateInfo]:
        """
        从 Boss直聘推荐页提取候选人信息

        通过 page.evaluate() 执行 JS 读取候选人卡片的 DOM 信息，
        包括姓名、基本信息、学历、薪资、标签等。

        Args:
            page: Playwright Page 实例

        Returns:
            List[CandidateInfo]: 提取到的候选人信息列表
        """
        js_code = """() => {
            const candidates = [];
            const cardSelectors = ['.candidate-card-wrap', '.geek-info-card', '.card-container'];
            let cards = [];

            for (const sel of cardSelectors) {
                cards = document.querySelectorAll(sel);
                if (cards.length > 0) break;
            }

            cards.forEach((card, index) => {
                const info = {
                    name: '',
                    age: '',
                    education: '',
                    experience: '',
                    skills: '',
                    salary: '',
                    raw_text: '',
                    element_index: index,
                    platform_user_id: ''
                };

                const nameEl = card.querySelector('.name');
                if (nameEl) info.name = nameEl.textContent.trim().split(/\\s+/)[0];

                const basicEls = card.querySelectorAll('.job-card-left, .base-info');
                const basicTexts = [];
                basicEls.forEach(el => {
                    const t = el.textContent.trim();
                    if (t) basicTexts.push(t);
                });

                const eduEls = card.querySelectorAll('.base-info.join-text-wrap, .geek-info-detail');
                eduEls.forEach(el => {
                    const t = el.textContent.trim();
                    if (t) basicTexts.push(t);
                });

                const extraTexts = [];
                const salaryEl = card.querySelector('.salary-text');
                if (salaryEl) {
                    info.salary = salaryEl.textContent.trim();
                    extraTexts.push('[薪资]' + info.salary);
                }

                const tagsEl = card.querySelector('.tags-wrap');
                if (tagsEl) {
                    info.skills = tagsEl.textContent.trim();
                    extraTexts.push('[标签]' + info.skills);
                }

                const uniEl = card.querySelector('.content.join-text-wrap');
                if (uniEl) extraTexts.push('[公司]' + uniEl.textContent.trim());

                info.raw_text = [...basicTexts, ...extraTexts].join(' | ');
                candidates.push(info);
            });

            return candidates;
        }"""

        try:
            results = await self._eval(page, js_code)
            candidates = []
            for item in results:
                candidates.append(CandidateInfo(
                    name=item.get("name", ""),
                    age=item.get("age", ""),
                    education=item.get("education", ""),
                    experience=item.get("experience", ""),
                    skills=item.get("skills", ""),
                    salary=item.get("salary", ""),
                    raw_text=item.get("raw_text", ""),
                    element_index=item.get("element_index", -1),
                    platform_user_id=item.get("platform_user_id", ""),
                ))
            logger.info(f"Boss直聘提取到 {len(candidates)} 个候选人")
            return candidates
        except Exception as e:
            logger.error(f"提取候选人失败: {e}")
            return []

    async def click_greet(self, page: Page, candidate_index: int) -> bool:
        card_selector = self.config.card_selectors[0] if self.config.card_selectors else ".candidate-card-wrap"

        try:
            cards = await self._locate(page, card_selector)
            card = cards.nth(candidate_index)
            await card.hover()
            await page.wait_for_timeout(300)
        except Exception:
            pass

        for selector in self.config.greet_btn_selectors:
            try:
                cards = await self._locate(page, card_selector)
                card = cards.nth(candidate_index)
                greet_btn = card.locator(selector).first

                if await greet_btn.is_visible(timeout=1500):
                    await greet_btn.click()
                    logger.info(f"已向第 {candidate_index} 个候选人打招呼")
                    return True
            except Exception:
                continue

        all_greet_selectors = self.config.greet_btn_selectors + self.config.continue_btn_selectors
        for selector in all_greet_selectors:
            try:
                btn = (await self._locate(page, selector)).first
                if await btn.is_visible(timeout=1500):
                    await btn.click()
                    logger.info(f"已向第 {candidate_index} 个候选人打招呼（页面级按钮: {selector}）")
                    return True
            except Exception:
                continue

        try:
            cards = await self._locate(page, card_selector)
            card = cards.nth(candidate_index)
            await card.click()
            await page.wait_for_timeout(1000)

            for selector in all_greet_selectors:
                try:
                    btn = (await self._locate(page, selector)).first
                    if await btn.is_visible(timeout=1500):
                        await btn.click()
                        logger.info(f"已向第 {candidate_index} 个候选人打招呼（点击卡片后）")
                        return True
                except Exception:
                    continue
        except Exception as e:
            logger.warning(f"打招呼失败（索引 {candidate_index}）: {e}")

        logger.warning(f"所有打招呼方式均失败（索引 {candidate_index}）")
        return False

    async def open_detail(self, page: Page, candidate_index: int) -> Optional[str]:
        try:
            frame = await self._get_frame(page)
            ctx = frame if frame else page
            logger.info(f"open_detail: 使用{'iframe' if frame else '主页面'}，索引={candidate_index}")

            card_selectors = [".candidate-card-wrap", ".geek-info-card", ".card-container"]
            card = None
            for sel in card_selectors:
                locator = ctx.locator(sel)
                count = await locator.count()
                if count > candidate_index:
                    card = locator.nth(candidate_index)
                    logger.info(f"open_detail: 用 {sel} 找到 {count} 张卡片")
                    break

            if not card:
                logger.warning(f"open_detail: 未找到卡片（索引 {candidate_index}）")
                return None

            await card.hover(force=True, timeout=3000)
            await page.wait_for_timeout(500)

            detail_selectors = [
                ".card-inner.common-wrap",
                ".card-inner.clear-fix",
                ".card-inner.blue-collar-wrap",
                ".card-inner.new-geek-wrap",
            ]
            clicked = False
            for sel in detail_selectors:
                try:
                    target = card.locator(sel).first
                    if await target.count() > 0:
                        await target.click(force=True, timeout=3000)
                        logger.info(f"open_detail: 点击子元素 {sel}")
                        clicked = True
                        break
                except Exception:
                    continue

            if not clicked:
                await card.click(force=True, timeout=3000)
                logger.info(f"open_detail: 直接点击卡片")

            await page.wait_for_timeout(1500)
            detail_text = await self._extract_detail_info(page)
            return detail_text
        except Exception as e:
            logger.warning(f"open_detail 异常: {e}")
            return None

    async def _extract_detail_info(self, page: Page) -> Optional[str]:
        js_code = """() => {
            const detailSelectors = [
                '.resume-custom-container',
                '.boss-popup__content',
                '.detail-content',
                '.resume-detail',
            ];

            for (const sel of detailSelectors) {
                const el = document.querySelector(sel);
                if (el && el.textContent.trim().length > 50) {
                    return el.textContent.trim();
                }
            }
            return null;
        }"""

        try:
            result = await self._eval(page, js_code)
            return result
        except Exception as e:
            logger.warning(f"提取详情信息失败: {e}")
            return None

    async def close_detail(self, page: Page) -> bool:
        """
        关闭 Boss直聘候选人详情弹框

        优先尝试按 ESC 键关闭弹框，如果失败则点击关闭按钮。
        关闭后等待 500ms 确保弹框完全关闭。

        Args:
            page: Playwright Page 实例

        Returns:
            bool: 是否成功关闭
        """
        try:
            await page.keyboard.press("Escape")
            await page.wait_for_timeout(800)

            still_open = await self._check_detail_open(page)
            if not still_open:
                logger.info("ESC 关闭详情弹框成功")
                return True

            logger.warning("ESC 未能关闭弹框")
            return False
        except Exception as e:
            logger.warning(f"关闭详情弹框失败: {e}")
            return False

    async def screenshot_detail(self, page: Page) -> Optional[bytes]:
        modal_selectors = self.config.detail_modal_selectors
        if not modal_selectors:
            return await self._fallback_screenshot(page)

        viewport = page.viewport_size
        vw = viewport["width"] if viewport else 1920
        vh = viewport["height"] if viewport else 1080

        frame = self._frame
        iframe_offset = {"x": 0, "y": 0}
        if frame:
            try:
                iframe_locator = page.locator(self._IFRAME_SELECTORS).first
                iframe_box = await iframe_locator.bounding_box()
                if iframe_box:
                    iframe_offset = {"x": iframe_box["x"], "y": iframe_box["y"]}
            except Exception:
                pass

        search_page = frame if frame else page

        for selector in modal_selectors:
            try:
                locator = search_page.locator(selector).first
                if not await locator.is_visible(timeout=3000):
                    continue

                box = await locator.bounding_box()
                if not box or box["width"] < 50 or box["height"] < 50:
                    continue

                box = {
                    "x": box["x"] + iframe_offset["x"],
                    "y": box["y"] + iframe_offset["y"],
                    "width": box["width"],
                    "height": box["height"],
                }

                is_full_overlay = box["width"] >= vw * 0.9 and box["height"] >= vh * 0.9
                if is_full_overlay:
                    logger.debug(f"[{self.platform_name}] 选择器 {selector} 匹配到全屏遮罩层，跳过")
                    continue

                needs_scroll = box["y"] + box["height"] > vh
                logger.info(
                    f"[{self.platform_name}] 弹框定位: 选择器={selector},"
                    f" box=({int(box['x'])},{int(box['y'])},{int(box['width'])},{int(box['height'])}),"
                    f" 视口={vw}x{vh}, iframe={frame is not None}, 需要滚动={needs_scroll}"
                )

                if not needs_scroll:
                    screenshot_bytes = await page.screenshot(type="png", clip=box)
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

    async def _check_detail_open(self, page: Page) -> bool:
        """
        检查详情弹框是否仍然打开

        通过查找 Boss直聘详情弹框特有元素判断弹框是否还存在。

        Args:
            page: Playwright Page 实例

        Returns:
            bool: 弹框是否仍然打开
        """
        js_code = """() => {
            const selectors = [
                '.resume-custom-container',
                '.boss-popup__content',
                '.boss-popup__close',
            ];
            for (const sel of selectors) {
                const el = document.querySelector(sel);
                if (el && el.offsetParent !== null) return true;
            }
            return false;
        }"""
        try:
            return await self._eval(page, js_code)
        except Exception:
            return False

    async def has_more_candidates(self, page: Page) -> bool:
        js_code = """() => {
            const selectors = ['.candidate-card-wrap', '.geek-info-card', '.card-container'];
            for (const sel of selectors) {
                const cards = document.querySelectorAll(sel);
                if (cards.length > 0) return cards.length;
            }
            return 0;
        }"""
        try:
            count = await self._eval(page, js_code)
            return count > 0
        except Exception:
            return False

    async def check_login_status(self, page: Page) -> bool:
        try:
            current_url = page.url

            if "zhipin.com/web/user" in current_url and "login" in current_url.lower():
                return False

            if "zhipin.com" not in current_url:
                await page.goto("https://www.zhipin.com", wait_until="domcontentloaded", timeout=15000)
                await page.wait_for_timeout(2000)

            login_indicator = await self._eval(page, """() => {
                const selectors = [
                    '.nav-figure', '.user-info', '.info-avatar',
                    '.nav-figure-user', '.user-avatar', '.header-user',
                    '[class*="user-info"]', '[class*="avatar"]',
                ];
                for (const sel of selectors) {
                    if (document.querySelectorAll(sel).length > 0) return true;
                }
                return false;
            }""")
            return login_indicator
        except Exception as e:
            logger.error(f"检查登录状态失败: {e}")
            return False

    async def wait_for_login(self, page: Page, timeout: int = 120000) -> bool:
        """
        等待用户手动登录 Boss直聘

        打开登录页面后等待用户扫码登录，
        检测到登录成功后返回 True。
        使用 Python time 模块计时，避免 JS evaluate 在异常页面上报错。

        Args:
            page: Playwright Page 实例
            timeout: 等待超时（毫秒），默认 2 分钟

        Returns:
            bool: 是否登录成功
        """
        import time

        logger.info("等待用户登录 Boss直聘，请扫码...")
        try:
            await page.goto("https://www.zhipin.com/web/user/?ka=header-login", wait_until="domcontentloaded")
        except Exception:
            pass

        start_time = time.time()
        while True:
            try:
                is_logged_in = await self.check_login_status(page)
                if is_logged_in:
                    logger.info("Boss直聘登录成功")
                    return True
            except Exception:
                pass

            await page.wait_for_timeout(3000)

            if (time.time() - start_time) * 1000 > timeout:
                logger.warning("登录等待超时")
                return False
