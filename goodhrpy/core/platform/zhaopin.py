"""
GoodHR 自动化工具 - 智联招聘平台解析器

实现智联招聘（zhaopin.com）特定的候选人提取和操作逻辑。
智联招聘 HR 端的推荐候选人页面 DOM 结构与 Boss直聘不同，
此解析器封装了智联特有的选择器和操作方法。
"""

from typing import List, Optional

from playwright.async_api import Page

from core.platform.base import BaseParser, CandidateInfo, PlatformConfig
from utils.logger import get_logger

logger = get_logger("zhaopin")

ZHAOPIN_CONFIG = PlatformConfig(
    id="zhaopin",
    name="智联招聘",
    domain="zhaopin.com",
    card_container="[role='group']",
    card_selectors=[
        ".recommend-item__inner-content"
        ".recommend-item__inner",
        ".recommend-resume-item__inner",
        ".new-shortcut-resume--wrapper",
    ],
    name_selector=".talent-basic-info__name--inner",
    basic_info_selectors=[".talent-basic-info__basic"],
    education_selectors=[".resume-item__content.resume-card-exp"],
    university_selector=".school-name",
    description_selector=".resume-item__content",
    greet_btn_selectors=[
        "[class*='is-mr-16']",
    ],
    continue_btn_selectors=[".btn-next"],
    detail_open_selectors=[".resume-item__content", ".resume-card-exp"],
    detail_close_selectors=[".km-icon.sati-times-circle-s", ".close-btn"],
    detail_modal_selectors=[
        ".new-resume-detail--inner",
        ".km-scrollbar__view",
        ".km-scrollbar__wrap",
        ".new-shortcut-resume--wrapper",
    ],
    extra_selectors=[
        {"selector": ".talent-basic-info__extra--content", "label": "薪资"},
    ],
)


class ZhaopinParser(BaseParser):
    """
    智联招聘平台解析器

    实现智联招聘推荐候选人页面的候选人提取和打招呼操作。
    智联招聘 HR 端入口为 rd.zhaopin.com（招聘者端）。
    """

    platform_id = "zhaopin"
    platform_name = "智联招聘"

    def __init__(self):
        """初始化智联招聘解析器"""
        super().__init__(ZHAOPIN_CONFIG)

    def get_entry_url(self, position_name: str = "") -> str:
        """
        获取智联招聘推荐候选人页入口 URL

        Args:
            position_name: 岗位名称（智联招聘推荐页根据当前登录账号自动匹配）

        Returns:
            str: 推荐候选人页 URL
        """
        return "https://rd6.zhaopin.com/app/recommend"

    async def ensure_on_page(self, page: Page, timeout: int = 60) -> bool:
        """
        确保页面在智联招聘推荐页

        先导航到推荐页入口 URL，然后每秒检查当前 URL 是否包含
        rd6.zhaopin.com/app（推荐页特征）。如果已登录，页面会自动
        跳转到推荐页；如果未登录，会停留在登录页。
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
            logger.warning(f"导航到智联推荐页失败: {e}")

        start_time = time.time()
        while True:
            try:
                current_url = page.url
                if "rd6.zhaopin.com/app" in current_url:
                    logger.info("智联招聘推荐页加载完成")
                    return True
            except Exception:
                pass

            elapsed = time.time() - start_time
            if elapsed >= timeout:
                logger.warning(f"智联招聘推荐页等待超时（{timeout}秒）")
                return False

            await page.wait_for_timeout(1000)

    async def extract_candidates(self, page: Page) -> List[CandidateInfo]:
        """
        从智联招聘推荐页提取候选人信息

        通过 page.evaluate() 执行 JS 读取候选人卡片的 DOM 信息。
        选择器来自旧版 Chrome 扩展 zhilian.js 的实际验证。

        Args:
            page: Playwright Page 实例

        Returns:
            List[CandidateInfo]: 提取到的候选人信息列表
        """
        js_code = """() => {
            const candidates = [];
            const cardSelectors = [
                '.recommend-item__inner',
                '.recommend-resume-item__inner',
                '.new-shortcut-resume--wrapper'
            ];
            let cards = [];

            for (const sel of cardSelectors) {
                cards = document.querySelectorAll(sel);
                if (cards.length > 0) break;
            }

            if (cards.length === 0) {
                const groupEl = document.querySelector('[role="group"]');
                if (groupEl) {
                    cards = groupEl.children;
                }
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

                const nameEl = card.querySelector('.talent-basic-info__name--inner');
                if (nameEl) info.name = nameEl.textContent.trim().split(/\\s+/)[0];

                const basicEl = card.querySelector('.talent-basic-info__basic');
                if (basicEl) {
                    const basicText = basicEl.textContent.trim();
                    const parts = basicText.split(/\\s+/).filter(Boolean);
                    parts.forEach(part => {
                        if (/\\d+岁/.test(part)) {
                            info.age = part;
                        } else if (/\\d+年|\\d+个月|应届/.test(part)) {
                            info.experience = part;
                        } else if (/[大专本科硕士博士]/.test(part) && !info.education) {
                            info.education = part;
                        }
                    });
                    if (!info.age && parts.length > 0) info.age = parts[0] || '';
                }

                const eduEl = card.querySelector('.resume-item__content.resume-card-exp');
                if (eduEl && !info.education) {
                    info.education = eduEl.textContent.trim();
                }

                const descEls = card.querySelectorAll('.resume-item__content');
                const descTexts = [];
                descEls.forEach(el => {
                    const t = el.textContent.trim();
                    if (t) descTexts.push(t);
                });

                const extraEl = card.querySelector('.talent-basic-info__extra--content');
                if (extraEl) {
                    info.salary = extraEl.textContent.trim();
                }

                info.raw_text = [
                    info.name ? '姓名:' + info.name : '',
                    info.age ? '年龄:' + info.age : '',
                    info.education ? '学历:' + info.education : '',
                    info.experience ? '经验:' + info.experience : '',
                    info.salary ? '薪资:' + info.salary : '',
                    ...descTexts.map(t => '描述:' + t),
                ].filter(Boolean).join(' | ');

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
            logger.info(f"智联招聘提取到 {len(candidates)} 个候选人")
            return candidates
        except Exception as e:
            logger.error(f"提取候选人失败: {e}")
            return []

    async def click_greet(self, page: Page, candidate_index: int) -> bool:
        """
        点击智联招聘候选人的打招呼按钮

        智联招聘的打招呼按钮 class 为 small-screen-btn，
        位于候选人卡片内部。先尝试卡片内定位，再尝试全局定位。

        Args:
            page: Playwright Page 实例
            candidate_index: 候选人卡片索引

        Returns:
            bool: 是否成功打招呼
        """
        card_selectors = self.config.card_selectors or [".recommend-item__inner"]

        for card_sel in card_selectors:
            try:
                cards = page.locator(card_sel)
                if await cards.count() <= candidate_index:
                    continue
                card = cards.nth(candidate_index)

                for btn_sel in self.config.greet_btn_selectors:
                    try:
                        greet_btn = card.locator(btn_sel).first
                        if await greet_btn.is_visible(timeout=3000):
                            await greet_btn.click()
                            logger.info(f"已向第 {candidate_index} 个候选人打招呼")
                            return True
                    except Exception:
                        continue
            except Exception:
                continue

        try:
            for card_sel in card_selectors:
                cards = page.locator(card_sel)
                if await cards.count() > candidate_index:
                    card = cards.nth(candidate_index)
                    await card.click()
                    await page.wait_for_timeout(1000)

                    for btn_sel in self.config.greet_btn_selectors:
                        try:
                            btn = page.locator(btn_sel).first
                            if await btn.is_visible(timeout=2000):
                                await btn.click()
                                logger.info(f"已向第 {candidate_index} 个候选人打招呼（详情模式）")
                                return True
                        except Exception:
                            continue
                    break
        except Exception as e:
            logger.warning(f"打招呼失败（索引 {candidate_index}）: {e}")

        return False

    async def open_detail(self, page: Page, candidate_index: int) -> Optional[str]:
        """
        点击候选人卡片打开详情弹框

        智联招聘点击卡片内的 resume-item__content 元素可打开候选人详情弹框，
        弹框内包含更完整的候选人信息（工作经历、项目经验等）。

        Args:
            page: Playwright Page 实例
            candidate_index: 候选人卡片索引

        Returns:
            Optional[str]: 详情页额外信息文本，打开失败返回 None
        """
        card_selectors = self.config.card_selectors or [".recommend-item__inner"]

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
        从智联招聘详情弹框中提取更完整的候选人信息

        详情弹框中 .new-shortcut-resume--wrapper 包含完整的简历信息，
        包括工作经历、项目经验、技能标签等。

        Args:
            page: Playwright Page 实例

        Returns:
            Optional[str]: 详情页文本内容，提取失败返回 None
        """
        js_code = """() => {
            const detailSelectors = [
                '.new-resume-detail--inner',
                '.km-scrollbar__view',
                '.km-scrollbar__wrap',
                '.new-shortcut-resume--wrapper'
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
        关闭智联招聘候选人详情弹框

        优先按 ESC 键关闭弹框（模拟真实用户操作），
        如果 ESC 无效则点击关闭按钮（模拟鼠标点击）。
        不直接操作 DOM 元素（如 dispatchEvent），避免被反爬检测。

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

        通过查找详情弹框特有元素判断弹框是否还存在。

        Args:
            page: Playwright Page 实例

        Returns:
            bool: 弹框是否仍然打开
        """
        js_code = """() => {
            const selectors = [
                '.new-resume-detail--inner',
                '.km-scrollbar__view',
                '.km-icon.sati-times-circle-s',
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

    async def check_login_status(self, page: Page) -> bool:
        """
        检查是否已登录智联招聘

        通过判断页面 URL 是否包含 rd6.zhaopin.com/app 来确认登录状态。
        登录成功后智联会自动跳转到该地址，进行 3 次判断（每次间隔 1 秒），
        因为页面可能还未完成跳转。

        Args:
            page: Playwright Page 实例

        Returns:
            bool: 是否已登录
        """
        try:
            if "zhaopin.com" not in page.url:
                await page.goto("https://rd.zhaopin.com", wait_until="domcontentloaded", timeout=15000)

            for i in range(3):
                current_url = page.url
                if "rd6.zhaopin.com/app" in current_url:
                    return True
                if i < 2:
                    await page.wait_for_timeout(1000)

            return False
        except Exception as e:
            logger.error(f"检查登录状态失败: {e}")
            return False

    async def wait_for_login(self, page: Page, timeout: int = 120000) -> bool:
        """
        等待用户手动登录智联招聘

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

        logger.info("等待用户登录智联招聘，请扫码...")
        try:
            await page.goto(
                "https://passport.zhaopin.com/org/login?validateCampus=",
                wait_until="domcontentloaded",
            )
        except Exception:
            pass

        start_time = time.time()
        while True:
            try:
                is_logged_in = await self.check_login_status(page)
                if is_logged_in:
                    logger.info("智联招聘登录成功")
                    return True
            except Exception:
                pass

            await page.wait_for_timeout(3000)

            if (time.time() - start_time) * 1000 > timeout:
                logger.warning("登录等待超时")
                return False
