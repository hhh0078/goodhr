"""
GoodHR 自动化工具 - Boss直聘平台解析器

实现 Boss直聘（zhipin.com）特定的候选人提取和操作逻辑。
Boss直聘推荐牛人页面的 DOM 结构和交互逻辑与其他平台不同，
此解析器封装了 Boss 特有的选择器和操作方法。
"""

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
    detail_open_selectors=[".card-inner.common-wrap", ".card-inner.clear-fix", ".candidate-card-wrap"],
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

    实现 Boss直聘推荐牛人页面的候选人提取和打招呼操作。
    Boss直聘的特点是信息较充足，AI 粗筛后可直接打招呼，无需打开详情页。
    """

    platform_id = "boss"
    platform_name = "Boss直聘"

    def __init__(self):
        """初始化 Boss直聘解析器"""
        super().__init__(BOSS_CONFIG)

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
                if "zhipin.com/web/chat" in current_url:
                    logger.info("Boss直聘推荐页加载完成")
                    return True
            except Exception:
                pass

            elapsed = time.time() - start_time
            if elapsed >= timeout:
                logger.warning(f"Boss直聘推荐页等待超时（{timeout}秒）")
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
            results = await page.evaluate(js_code)
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
        """
        点击 Boss直聘候选人的打招呼按钮

        通过 CDP 鼠标事件点击，而非 JS 注入，
        配合 humanize=True 实现仿真人点击行为。

        Args:
            page: Playwright Page 实例
            candidate_index: 候选人卡片索引

        Returns:
            bool: 是否成功打招呼
        """
        for selector in self.config.greet_btn_selectors:
            try:
                cards = page.locator(self.config.card_selectors[0] if self.config.card_selectors else ".candidate-card-wrap")
                card = cards.nth(candidate_index)
                greet_btn = card.locator(selector).first

                if await greet_btn.is_visible(timeout=3000):
                    await greet_btn.click()
                    logger.info(f"已向第 {candidate_index} 个候选人打招呼")
                    return True
            except Exception:
                continue

        try:
            cards = page.locator(self.config.card_selectors[0] if self.config.card_selectors else ".candidate-card-wrap")
            card = cards.nth(candidate_index)
            await card.click()
            await page.wait_for_timeout(1000)

            for selector in self.config.greet_btn_selectors:
                try:
                    btn = page.locator(selector).first
                    if await btn.is_visible(timeout=2000):
                        await btn.click()
                        logger.info(f"已向第 {candidate_index} 个候选人打招呼（详情模式）")
                        return True
                except Exception:
                    continue
        except Exception as e:
            logger.warning(f"打招呼失败（索引 {candidate_index}）: {e}")

        return False

    async def open_detail(self, page: Page, candidate_index: int) -> Optional[str]:
        """
        点击候选人卡片打开详情弹框

        Boss直聘点击卡片内部区域可打开候选人详情弹框，
        弹框中包含更完整的工作经历和项目经验等信息。

        Args:
            page: Playwright Page 实例
            candidate_index: 候选人卡片索引

        Returns:
            Optional[str]: 详情页额外信息文本，打开失败返回 None
        """
        card_selectors = self.config.card_selectors or [".candidate-card-wrap"]

        for card_sel in card_selectors:
            try:
                cards = page.locator(card_sel)
                if await cards.count() <= candidate_index:
                    continue
                card = cards.nth(candidate_index)

                for open_sel in self.config.detail_open_selectors:
                    try:
                        detail_el = card.locator(open_sel).first
                        if await detail_el.is_visible(timeout=2000):
                            await detail_el.click()
                            await page.wait_for_timeout(1500)

                            detail_text = await self._extract_detail_info(page)
                            if detail_text:
                                logger.info(f"已打开第 {candidate_index} 个候选人详情")
                                return detail_text
                    except Exception:
                        continue
            except Exception:
                continue

        logger.warning(f"打开候选人详情失败（索引 {candidate_index}）")
        return None

    async def _extract_detail_info(self, page: Page) -> Optional[str]:
        """
        从 Boss直聘详情弹框中提取更完整的候选人信息

        详情弹框中包含工作经历、项目经验、教育经历等完整简历信息。

        Args:
            page: Playwright Page 实例

        Returns:
            Optional[str]: 详情页文本内容，提取失败返回 None
        """
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
            result = await page.evaluate(js_code)
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
            return await page.evaluate(js_code)
        except Exception:
            return False

    async def has_more_candidates(self, page: Page) -> bool:
        """
        检查是否还有更多候选人可以加载

        Boss直聘推荐页通过滚动加载更多候选人，
        当滚动到底部不再有新卡片出现时返回 False。

        Args:
            page: Playwright Page 实例

        Returns:
            bool: 是否还有更多候选人
        """
        js_code = """() => {
            const selectors = ['.candidate-card-wrap', '.geek-info-card', '.card-container'];
            for (const sel of selectors) {
                const cards = document.querySelectorAll(sel);
                if (cards.length > 0) return cards.length;
            }
            return 0;
        }"""
        try:
            count = await page.evaluate(js_code)
            return count > 0
        except Exception:
            return False

    async def check_login_status(self, page: Page) -> bool:
        """
        检查是否已登录 Boss直聘

        先检查当前页面 URL，如果在 Boss直聘域下且不在登录页，
        则尝试从 DOM 中查找用户信息元素确认登录状态。
        不会主动导航到首页，避免打断用户当前操作。

        Args:
            page: Playwright Page 实例

        Returns:
            bool: 是否已登录
        """
        try:
            current_url = page.url

            if "zhipin.com/web/user" in current_url and "login" in current_url.lower():
                return False

            if "zhipin.com" not in current_url:
                await page.goto("https://www.zhipin.com", wait_until="domcontentloaded", timeout=15000)
                await page.wait_for_timeout(2000)

            login_indicator = await page.evaluate("""() => {
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
