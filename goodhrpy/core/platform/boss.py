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

        Args:
            page: Playwright Page 实例

        Returns:
            bool: 是否已登录
        """
        try:
            await page.goto("https://www.zhipin.com", wait_until="domcontentloaded", timeout=15000)
            await page.wait_for_timeout(2000)

            login_indicator = await page.evaluate("""() => {
                const navItems = document.querySelectorAll('.nav-figure, .user-info, .info-avatar');
                return navItems.length > 0;
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

        Args:
            page: Playwright Page 实例
            timeout: 等待超时（毫秒），默认 2 分钟

        Returns:
            bool: 是否登录成功
        """
        logger.info("等待用户登录 Boss直聘，请扫码...")
        try:
            await page.goto("https://www.zhipin.com/web/user/?ka=header-login", wait_until="domcontentloaded")
        except Exception:
            pass

        start_time = page.evaluate("Date.now()")
        while True:
            try:
                is_logged_in = await self.check_login_status(page)
                if is_logged_in:
                    logger.info("Boss直聘登录成功")
                    return True
            except Exception:
                pass

            await page.wait_for_timeout(3000)

            elapsed = await page.evaluate(f"Date.now() - {start_time}")
            if elapsed > timeout:
                logger.warning("登录等待超时")
                return False
