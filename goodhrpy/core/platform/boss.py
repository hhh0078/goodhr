"""
GoodHR 自动化工具 - Boss直聘平台解析器

实现 Boss直聘（zhipin.com）推荐牛人页面的候选人提取和操作逻辑。
Boss 页面主体经常渲染在 iframe 中，因此本解析器所有查找和点击都遵循：
先查主页面，主页面没有再遍历所有 iframe。
"""

from typing import List, Optional, Tuple

from playwright.async_api import Frame, Locator, Page

from core.platform.base import BaseParser, CandidateInfo, PlatformConfig
from utils.logger import get_logger

logger = get_logger("boss")

BOSS_CONFIG = PlatformConfig(
    id="boss",
    name="Boss直聘",
    domain="zhipin.com",
    card_container=".card-list",
    card_selectors=[
        ".candidate-card-wrap",
        ".geek-info-card",
        ".card-container",
        ".card-inner.clear-fix",
        ".card-inner.common-wrap",
    ],
    name_selector=".name",
    basic_info_selectors=[".job-card-left", "[class*='job-card-left']"],
    education_selectors=[".base-info.join-text-wrap", ".geek-info-detail"],
    university_selector=".content.join-text-wrap",
    description_selector=".content",
    greet_btn_selectors=[
        ".btn.btn-greet",
        ".btn.btn-getcontact",
        "[class*='btn-greet']",
        "[class*='btn-getcontact']",
        "[class*='prop-card-chat']",
    ],
    continue_btn_selectors=[".btn.btn-continue.btn-outline", "[class*='btn-continue']"],
    detail_open_selectors=[
        ".card-inner.common-wrap",
        ".card-inner.clear-fix",
        ".candidate-card-wrap",
        ".geek-info-card",
    ],
    detail_close_selectors=[
        ".boss-popup__close",
        ".resume-custom-close",
        "[class*='boss-popup__close']",
        "[class*='resume-custom-close']",
    ],
    detail_modal_selectors=[
        ".boss-popup__body",
        ".resume-detail",
        ".geek-detail",
        "#resume",
        "[class*='resume']",
        "[class*='geek-detail']",
    ],
    extra_selectors=[
        {"selector": ".salary-text", "label": "薪资"},
        {"selector": ".job-info-primary", "label": "基本信息"},
        {"selector": ".tags-wrap", "label": "标签"},
        {"selector": ".content.join-text-wrap", "label": "公司信息"},
        {"selector": ".active-text", "label": "活跃状态"},
        {"selector": ".colleague-collaboration", "label": "同事沟通"},
    ],
)


class BossParser(BaseParser):
    """Boss直聘平台解析器。"""

    platform_id = "boss"
    platform_name = "Boss直聘"

    def __init__(self):
        super().__init__(BOSS_CONFIG)

    def get_entry_url(self, position_name: str = "") -> str:
        return "https://www.zhipin.com/web/chat/recommend"

    async def ensure_on_page(self, page: Page, timeout: int = 60) -> bool:
        import time

        try:
            await page.goto(self.get_entry_url(), wait_until="domcontentloaded", timeout=30000)
        except Exception as e:
            logger.warning(f"导航到 Boss 推荐页失败: {e}")

        start_time = time.time()
        while True:
            if await self.check_login_status(page):
                logger.info("Boss直聘推荐页加载完成")
                return True

            if time.time() - start_time >= timeout:
                logger.warning(f"Boss直聘推荐页等待超时（{timeout}秒）")
                return False

            await page.wait_for_timeout(1000)

    async def check_login_status(self, page: Page) -> bool:
        try:
            if "zhipin.com" not in page.url:
                await page.goto("https://www.zhipin.com", wait_until="domcontentloaded", timeout=15000)

            for i in range(3):
                current_url = page.url
                if "/web/chat/recommend" in current_url or "/web/geek" in current_url:
                    return True

                frame, _, _ = await self._find_frame_with_cards(page, timeout=1000)
                if frame:
                    return True

                if i < 2:
                    await page.wait_for_timeout(1000)

            return False
        except Exception as e:
            logger.error(f"检查 Boss 登录状态失败: {e}")
            return False

    async def wait_for_login(self, page: Page, timeout: int = 120000) -> bool:
        import time

        logger.info("等待用户登录 Boss直聘，请扫码...")
        try:
            await page.goto(
                "https://www.zhipin.com/web/user/?ka=header-login",
                wait_until="domcontentloaded",
            )
        except Exception:
            pass

        start_time = time.time()
        while True:
            if await self.check_login_status(page):
                logger.info("Boss直聘登录成功")
                return True

            if (time.time() - start_time) * 1000 > timeout:
                logger.warning("Boss直聘登录等待超时")
                return False

            await page.wait_for_timeout(3000)

    async def extract_candidates(self, page: Page) -> List[CandidateInfo]:
        js_code = """(selectors) => {
            const pick = (root, selectorList) => {
                for (const sel of selectorList) {
                    const el = root.querySelector(sel);
                    if (el) return el;
                }
                return null;
            };
            const text = (el) => (el && el.textContent ? el.textContent.trim() : '');
            const splitInfo = (value) => value.split(/[\\s|·/]+/).map(x => x.trim()).filter(Boolean);
            const extractAge = (value) => {
                const m = (value || '').match(/\\d+岁/);
                return m ? m[0] : '';
            };
            const extractExperience = (parts) => {
                return parts.find(p => /\\d+年|\\d+个月|经验|应届/.test(p)) || '';
            };
            const extractEducation = (parts) => {
                return parts.find(p => /(中专|大专|本科|硕士|博士|学历不限)/.test(p)) || '';
            };

            let cards = [];
            let matchedSelector = '';
            for (const sel of selectors.cards) {
                cards = Array.from(document.querySelectorAll(sel));
                if (cards.length > 0) {
                    matchedSelector = sel;
                    break;
                }
            }

            return cards.map((card, index) => {
                const nameEl = pick(card, ['.name', '[class*="name"]']);
                const basicEl = pick(card, ['.job-card-left', '[class*="job-card-left"]']);
                const eduEl = pick(card, [
                    '.base-info.join-text-wrap',
                    '.geek-info-detail',
                    '[class*="base-info"]',
                    '[class*="geek-info-detail"]'
                ]);
                const descEls = Array.from(card.querySelectorAll(
                    '.content, [class*="content"], .tags-wrap, [class*="tags-wrap"]'
                ));
                const salaryEl = pick(card, ['.salary-text', '[class*="salary-text"]']);
                const activeEl = pick(card, [
                    '.active-text',
                    '[class*="active-text"], .online-marker, [class*="online-marker"]'
                ]);

                const basicText = text(basicEl);
                const eduText = text(eduEl);
                const parts = splitInfo([basicText, eduText].filter(Boolean).join(' '));
                const extraTexts = [];
                for (const extra of selectors.extras) {
                    const el = card.querySelector(extra.selector);
                    const value = text(el);
                    if (value) extraTexts.push(extra.label + ':' + value);
                }

                const descTexts = descEls
                    .map(el => text(el))
                    .filter(Boolean)
                    .filter((value, idx, arr) => arr.indexOf(value) === idx);

                const name = text(nameEl).split(/\\s+/)[0] || '';
                const age = extractAge(basicText) || extractAge(eduText);
                const education = extractEducation(parts) || eduText;
                const experience = extractExperience(parts);
                const salary = text(salaryEl);
                const hasOnlineMarker = card.querySelector('.online-marker, [class*="online-marker"]');
                const active = text(activeEl) || (hasOnlineMarker ? '在线' : '');

                const rawParts = [
                    name ? '姓名:' + name : '',
                    age ? '年龄:' + age : '',
                    education ? '学历:' + education : '',
                    experience ? '经验:' + experience : '',
                    salary ? '薪资:' + salary : '',
                    active ? '活跃状态:' + active : '',
                    ...extraTexts,
                    ...descTexts.map(t => '描述:' + t),
                ].filter(Boolean);

                return {
                    name,
                    age,
                    education,
                    experience,
                    skills: descTexts.join(' '),
                    salary,
                    raw_text: rawParts.join(' | '),
                    element_index: index,
                    platform_user_id: card.getAttribute('data-geek-id') || card.getAttribute('data-uid') || '',
                    matched_selector: matchedSelector,
                };
            }).filter(item => item.name || item.raw_text);
        }"""

        frame, selector, _ = await self._find_frame_with_cards(page)
        if not frame:
            logger.warning("Boss直聘未找到候选人卡片，可能选择器不匹配")
            return []

        try:
            results = await frame.evaluate(
                js_code,
                {
                    "cards": self.config.card_selectors,
                    "extras": self.config.extra_selectors,
                },
            )
            candidates = [
                CandidateInfo(
                    name=item.get("name", ""),
                    age=item.get("age", ""),
                    education=item.get("education", ""),
                    experience=item.get("experience", ""),
                    skills=item.get("skills", ""),
                    salary=item.get("salary", ""),
                    raw_text=item.get("raw_text", ""),
                    element_index=item.get("element_index", -1),
                    platform_user_id=item.get("platform_user_id", ""),
                )
                for item in results
            ]
            logger.info(f"Boss直聘提取到 {len(candidates)} 个候选人（选择器: {selector}）")
            return candidates
        except Exception as e:
            logger.error(f"Boss直聘提取候选人失败: {e}")
            return []

    async def click_greet(self, page: Page, candidate_index: int) -> bool:
        found = await self._card_locator(page, candidate_index)
        if not found:
            logger.warning(f"Boss直聘打招呼失败：未找到第 {candidate_index} 个候选人卡片")
            return False

        frame, card = found
        try:
            await card.hover(timeout=3000)
            await page.wait_for_timeout(500)
        except Exception:
            pass

        for btn_sel in self.config.greet_btn_selectors:
            try:
                btn = card.locator(btn_sel).first
                if await btn.is_visible(timeout=2000):
                    if not await self._mouse_click_locator(page, btn):
                        continue
                    await page.wait_for_timeout(1000)
                    logger.info(f"已点击 Boss 第 {candidate_index} 个候选人打招呼按钮（卡片内）")
                    return True
            except Exception:
                continue

        for btn_sel in self.config.greet_btn_selectors:
            try:
                btn = frame.locator(btn_sel).nth(candidate_index)
                if await btn.is_visible(timeout=1500):
                    if not await self._mouse_click_locator(page, btn):
                        continue
                    await page.wait_for_timeout(1000)
                    logger.info(f"已点击 Boss 第 {candidate_index} 个候选人打招呼按钮（全局）")
                    return True
            except Exception:
                continue

        logger.warning(f"Boss直聘打招呼按钮未找到（索引 {candidate_index}）")
        return False

    async def open_detail(self, page: Page, candidate_index: int) -> Optional[str]:
        found = await self._card_locator(page, candidate_index)
        if not found:
            logger.warning(f"Boss直聘打开详情失败：未找到第 {candidate_index} 个候选人卡片")
            return None

        _, card = found
        for open_sel in self.config.detail_open_selectors:
            try:
                target = card.locator(open_sel).first
                if await target.is_visible(timeout=2000):
                    if not await self._mouse_click_locator(page, target):
                        continue
                    await page.wait_for_timeout(1500)
                    detail = await self._extract_detail_info(page)
                    if detail:
                        logger.info(f"已打开 Boss 第 {candidate_index} 个候选人详情")
                        return detail
            except Exception:
                continue

        try:
            if not await self._mouse_click_locator(page, card):
                logger.warning(f"Boss直聘打开详情失败（索引 {candidate_index}）：卡片不可点击")
                return None
            await page.wait_for_timeout(1500)
            return await self._extract_detail_info(page)
        except Exception as e:
            logger.warning(f"Boss直聘打开详情失败（索引 {candidate_index}）: {e}")
            return None

    async def close_detail(self, page: Page) -> bool:
        try:
            await page.keyboard.press("Escape")
            await page.wait_for_timeout(800)
            if not await self._check_detail_open(page):
                logger.info("Boss直聘 ESC 关闭详情成功")
                return True
        except Exception:
            pass

        for frame in self._frames_main_first(page):
            for selector in self.config.detail_close_selectors:
                try:
                    btn = frame.locator(selector).first
                    if await btn.is_visible(timeout=1000):
                        if not await self._mouse_click_locator(page, btn):
                            continue
                        await page.wait_for_timeout(800)
                        logger.info("Boss直聘点击关闭按钮成功")
                        return True
                except Exception:
                    continue

        logger.warning("Boss直聘关闭详情失败：未找到关闭按钮")
        return False

    async def wait_for_cards(self, page: Page, timeout: int = 10000) -> bool:
        frame, selector, count = await self._find_frame_with_cards(page, timeout=timeout)
        if frame and count > 0:
            logger.debug(f"Boss直聘候选人卡片已加载: {count}（选择器: {selector}）")
            return True
        logger.warning("Boss直聘候选人卡片未加载")
        return False

    async def screenshot_detail(self, page: Page) -> Optional[bytes]:
        for frame in self._frames_main_first(page):
            for selector in self.config.detail_modal_selectors:
                try:
                    modal = frame.locator(selector).first
                    if await modal.is_visible(timeout=1500):
                        box = await modal.bounding_box()
                        if box and box["width"] >= 50 and box["height"] >= 50:
                            return await modal.screenshot(type="png")
                except Exception:
                    continue
        return await self._fallback_screenshot(page)

    async def _extract_detail_info(self, page: Page) -> Optional[str]:
        js_code = """(selectors) => {
            for (const sel of selectors) {
                const el = document.querySelector(sel);
                if (el && el.textContent && el.textContent.trim().length > 30) {
                    return el.textContent.trim();
                }
            }
            const resume = document.getElementById('resume');
            if (resume && resume.textContent.trim().length > 30) {
                return resume.textContent.trim();
            }
            return null;
        }"""

        for frame in self._frames_main_first(page):
            try:
                result = await frame.evaluate(js_code, self.config.detail_modal_selectors)
                if result:
                    return result
            except Exception:
                continue
        return None

    async def _check_detail_open(self, page: Page) -> bool:
        for frame in self._frames_main_first(page):
            for selector in self.config.detail_modal_selectors + self.config.detail_close_selectors:
                try:
                    locator = frame.locator(selector).first
                    if await locator.is_visible(timeout=500):
                        return True
                except Exception:
                    continue
        return False

    async def _card_locator(self, page: Page, candidate_index: int) -> Optional[Tuple[Frame, Locator]]:
        frame, selector, count = await self._find_frame_with_cards(page)
        if not frame or not selector or count <= candidate_index:
            return None
        return frame, frame.locator(selector).nth(candidate_index)

    async def _mouse_click_locator(self, page: Page, locator: Locator) -> bool:
        """
        通过 Playwright 鼠标坐标点击元素，避免 locator.click 自动滚动在 iframe 内失败。

        locator.bounding_box() 返回的是相对主页面视口的坐标，即使元素在 iframe 中，
        也可以直接交给 page.mouse.click 使用。
        """
        try:
            viewport = page.viewport_size or {"width": 0, "height": 0}
            viewport_width = viewport.get("width", 0)
            viewport_height = viewport.get("height", 0)

            for attempt in range(8):
                box = await locator.bounding_box(timeout=2000)
                if not box or box["width"] <= 0 or box["height"] <= 0:
                    return False

                x = box["x"] + box["width"] / 2
                y = box["y"] + box["height"] / 2
                if self._point_in_viewport(x, y, viewport_width, viewport_height):
                    await page.mouse.move(x, y)
                    await page.mouse.click(x, y)
                    await page.wait_for_timeout(300)
                    return True

                if not viewport_width or not viewport_height:
                    return False

                wheel_x = min(max(x, 10), viewport_width - 10)
                wheel_y = min(max(y, 10), viewport_height - 10)
                await page.mouse.move(wheel_x, wheel_y)

                if y > viewport_height:
                    delta_y = min(max(y - viewport_height * 0.65, 300), 1000)
                elif y < 0:
                    delta_y = -min(max(abs(y) + viewport_height * 0.25, 300), 1000)
                else:
                    delta_y = 300 if attempt % 2 == 0 else -300

                logger.debug(
                    "Boss直聘目标不在视口内，滚动后重试点击 "
                    f"attempt={attempt + 1}, point=({x:.1f},{y:.1f}), delta={delta_y:.1f}"
                )
                await page.mouse.wheel(0, delta_y)
                await page.wait_for_timeout(400)

            logger.debug("Boss直聘鼠标坐标点击失败：多次滚动后目标仍不在视口内")
            return False
        except Exception as e:
            logger.debug(f"Boss直聘鼠标坐标点击失败: {e}")
            return False

    @staticmethod
    def _point_in_viewport(x: float, y: float, width: int, height: int) -> bool:
        if not width or not height:
            return True
        return 0 <= x <= width and 0 <= y <= height

    async def _find_frame_with_cards(
        self,
        page: Page,
        timeout: int = 3000,
    ) -> Tuple[Optional[Frame], str, int]:
        return await self._find_frame_with_any_selector(page, self.config.card_selectors, timeout=timeout)

    async def _find_frame_with_any_selector(
        self,
        page: Page,
        selectors: List[str],
        timeout: int = 3000,
    ) -> Tuple[Optional[Frame], str, int]:
        frames = self._frames_main_first(page)

        for frame in frames[:1]:
            found = await self._find_selector_in_frame(frame, selectors, timeout)
            if found[2] > 0:
                return found

        for frame in frames[1:]:
            found = await self._find_selector_in_frame(frame, selectors, timeout)
            if found[2] > 0:
                return found

        return None, "", 0

    async def _find_selector_in_frame(
        self,
        frame: Frame,
        selectors: List[str],
        timeout: int,
    ) -> Tuple[Optional[Frame], str, int]:
        for selector in selectors:
            try:
                locator = frame.locator(selector)
                count = await locator.count()
                if count > 0:
                    try:
                        if await locator.first.is_visible(timeout=min(timeout, 1500)):
                            return frame, selector, count
                    except Exception:
                        return frame, selector, count
            except Exception:
                continue
        return None, "", 0

    def _frames_main_first(self, page: Page) -> List[Frame]:
        frames: List[Frame] = []

        def walk(frame: Frame) -> None:
            frames.append(frame)
            for child in frame.child_frames:
                walk(child)

        walk(page.main_frame)
        return frames
