"""
GoodHR 自动化工具 - 任务编排器

使用严格状态机管理任务生命周期，整个流程在 _run() 方法中线性执行，
方便查看完整流程。所有方法调用均加异常处理，单个候选人出错不影响后续。
"""

import asyncio
import gc
import random
from datetime import datetime
from enum import Enum
from pathlib import Path
from typing import Callable, Optional

from sqlalchemy import select

from core.browser import BrowserManager
from core.filter.ai_filter import AIFilter
from core.filter.keyword import KeywordFilter
from core.humanize import random_delay, scroll_to_load
from core.platform.base import BaseParser, CandidateInfo
from core.platform.boss import BossParser
from core.platform.zhaopin import ZhaopinParser
from core.settings import config
from models.candidate import Candidate, CandidateStatus
from models.database import async_session
from models.position import Position
from models.task_log import TaskLog
from utils.logger import get_logger

logger = get_logger("task")


class TaskMode(str, Enum):
    """任务模式枚举"""

    AI = "ai"
    KEYWORD = "keyword"


class TaskStatus(str, Enum):
    """任务状态枚举"""

    IDLE = "idle"
    RUNNING = "running"
    STOPPING = "stopping"


_VALID_TRANSITIONS = {
    TaskStatus.IDLE: [TaskStatus.RUNNING],
    TaskStatus.RUNNING: [TaskStatus.STOPPING, TaskStatus.IDLE],
    TaskStatus.STOPPING: [TaskStatus.IDLE],
}


def get_parser(platform_id: str = "boss") -> BaseParser:
    """
    根据平台 ID 获取对应的解析器实例

    Args:
        platform_id: 平台标识

    Returns:
        BaseParser: 平台解析器实例

    Raises:
        ValueError: 不支持的平台 ID
    """
    parsers = {
        "boss": BossParser,
        "zhaopin": ZhaopinParser,
    }
    parser_cls = parsers.get(platform_id)
    if not parser_cls:
        raise ValueError(f"不支持的平台: {platform_id}，当前支持: {list(parsers.keys())}")
    return parser_cls()


class TaskOrchestrator:
    """
    任务编排器

    使用严格状态机管理候选人筛选任务的完整生命周期。
    整个流程在 _run() 方法中线性执行，方便查看。
    """

    def __init__(self):
        """初始化任务编排器"""
        self._browser_manager = BrowserManager()
        self._ai_filter: Optional[AIFilter] = None
        self._keyword_filter: Optional[KeywordFilter] = None
        self._parser: Optional[BaseParser] = None
        self._status = TaskStatus.IDLE
        self._task_log_id: Optional[int] = None
        self._match_count = 0
        self._total_count = 0
        self._skipped_count = 0
        self._failed_count = 0
        self._async_task: Optional[asyncio.Task] = None
        self._start_lock = asyncio.Lock()

        self._on_log: Optional[Callable] = None
        self._position_id: Optional[int] = None
        self._job_description: str = ""
        self._account_id: Optional[str] = None
        self._profile_dir: Optional[str] = None
        self._rest_remaining = 0
        self._candidates_since_rest = 0
        self._next_rest_after = 0

    @property
    def status(self) -> TaskStatus:
        """当前任务状态"""
        return self._status

    @property
    def match_count(self) -> int:
        """已打招呼数量"""
        return self._match_count

    @property
    def total_count(self) -> int:
        """已扫描总数"""
        return self._total_count

    @property
    def skipped_count(self) -> int:
        """已跳过数量"""
        return self._skipped_count

    @property
    def failed_count(self) -> int:
        """失败数量"""
        return self._failed_count

    def on_log(self, callback: Callable[[str, str], None]) -> None:
        """
        设置日志回调函数

        Args:
            callback: 回调函数，参数为 (message, level)
        """
        self._on_log = callback

    def _log(self, message: str, level: str = "info") -> None:
        """
        输出日志，同时触发回调

        Args:
            message: 日志消息
            level: 日志级别
        """
        log_func = getattr(logger, level, None)
        if log_func and callable(log_func):
            log_func(message)
        else:
            logger.info(message)
        if self._on_log:
            self._on_log(message, level)

    def _transition(self, new_status: TaskStatus) -> bool:
        """
        状态机转换，只允许合法转换

        Args:
            new_status: 目标状态

        Returns:
            bool: 是否转换成功
        """
        allowed = _VALID_TRANSITIONS.get(self._status, [])
        if new_status not in allowed:
            logger.warning(f"非法状态转换: {self._status.value} → {new_status.value}")
            return False
        old = self._status
        self._status = new_status
        logger.info(f"状态转换: {old.value} → {new_status.value}")
        return True

    async def start(
        self,
        position_id: int,
        mode: TaskMode = TaskMode.AI,
        match_limit: Optional[int] = None,
        platform_id: str = "boss",
        account_id: Optional[str] = None,
        profile_dir: Optional[str] = None,
    ) -> None:
        """
        启动候选人筛选任务

        使用 asyncio.Lock 防止并发启动。
        状态机保证只有 IDLE → RUNNING 的转换才真正启动任务。

        Args:
            position_id: 岗位 ID
            mode: 筛选模式（AI / 关键词）
            match_limit: 匹配上限，None 使用全局配置
            platform_id: 平台 ID
        """
        if self._start_lock.locked():
            self._log("任务启动中，请勿重复操作", "warning")
            return

        async with self._start_lock:
            if not self._transition(TaskStatus.RUNNING):
                self._log("当前状态不允许启动任务", "warning")
                return

            try:
                await self._run(position_id, mode, match_limit, platform_id, account_id, profile_dir)
            except asyncio.CancelledError:
                self._log("任务已被用户取消", "warning")
            except Exception as e:
                self._log(f"任务异常: {e}", "error")
            finally:
                await self._cleanup()

    async def stop(self) -> None:
        """
        停止当前任务

        设置状态为 STOPPING，主循环会检测到并退出。
        同时取消 asyncio Task 强制中断阻塞操作。
        """
        if self._status == TaskStatus.RUNNING:
            self._transition(TaskStatus.STOPPING)
            self._log("正在停止任务...")
            if self._async_task and not self._async_task.done():
                self._async_task.cancel()

    async def _run(
        self,
        position_id: int,
        mode: TaskMode,
        match_limit: Optional[int],
        platform_id: str,
        account_id: Optional[str],
        profile_dir: Optional[str],
    ) -> None:
        """
        任务主流程 - 所有逻辑都在这个方法里，方便查看完整流程

        流程：
        1. 校验参数（岗位、平台登录、筛选条件）→ 不通过直接退出
        2. 启动浏览器（使用 cookie / 持久化 profile）
        3. 打开推荐页 + 判断登录（每秒判断页面路由，最多等60秒）
        4. 等待用户登录（如未登录，弹出扫码页）
        5. 主循环：提取候选人 → 查看详情 → 筛选 → 关闭详情 → 打招呼
        6. 滚动加载更多 → 回到步骤5
        7. 任务结束，输出统计
        """
        self._async_task = asyncio.current_task()
        self._position_id = position_id
        self._account_id = account_id
        self._profile_dir = profile_dir
        self._match_count = 0
        self._total_count = 0
        self._skipped_count = 0
        self._failed_count = 0

        # ══════════════════════════════════════════
        # 步骤 1：校验参数（不通过直接退出）
        # ══════════════════════════════════════════
        self._log("=" * 50)
        self._log("步骤 1/7：校验任务参数...")

        try:
            self._parser = get_parser(platform_id)
        except ValueError as e:
            self._log(f"平台不支持: {e}", "error")
            return

        resolved_profile_dir = Path(profile_dir) if profile_dir else config.data_dir / "profiles" / platform_id
        if not resolved_profile_dir.exists() or not (resolved_profile_dir / "Default").exists():
            self._log(
                f"平台 [{self._parser.platform_name}] 未登录，请先在「平台登录」中扫码登录",
                "error",
            )
            return

        position = None
        try:
            position = await self._load_position(position_id)
        except Exception as e:
            self._log(f"加载岗位信息异常: {e}", "error")
            return

        if not position:
            self._log(
                f"岗位 ID {position_id} 不存在，请先在「岗位管理」中创建岗位",
                "error",
            )
            return

        self._job_description = position.description or ""

        if mode == TaskMode.AI:
            if not config.ai.api_key:
                self._log("AI 模式需要配置 API Key，请在「系统配置」中填写", "error")
                return
            try:
                self._ai_filter = AIFilter()
            except Exception as e:
                self._log(f"初始化 AI 筛选器失败: {e}", "error")
                return
        else:
            keywords = [k.strip() for k in (position.keywords or "").split(",") if k.strip()]
            exclude_keywords = [k.strip() for k in (position.exclude_keywords or "").split(",") if k.strip()]
            if not keywords:
                self._log("关键词模式需要设置关键词，请编辑岗位添加关键词", "error")
                return
            try:
                self._keyword_filter = KeywordFilter(
                    keywords=keywords,
                    exclude_keywords=exclude_keywords,
                    is_and_mode=position.is_and_mode,
                )
            except Exception as e:
                self._log(f"初始化关键词筛选器失败: {e}", "error")
                return

        match_limit = match_limit or config.task.match_limit
        self._prepare_mid_task_rest()

        try:
            self._task_log_id = await self._create_task_log(position_id, position.name)
        except Exception as e:
            self._log(f"创建任务日志失败: {e}", "warning")

        self._log(
            f"任务启动: 岗位={position.name}, 平台={self._parser.platform_name}, "
            f"模式={mode.value}, 上限={match_limit}"
        )
        self._log("=" * 50)

        # ══════════════════════════════════════════
        # 步骤 2：启动浏览器（使用 cookie 持久化）
        # ══════════════════════════════════════════
        self._log("步骤 2/7：启动浏览器...")
        page = None
        try:
            user_data_dir = str(resolved_profile_dir)
            await self._browser_manager.start(persistent=True, user_data_dir=user_data_dir)
            page = await self._browser_manager.new_page("main")
        except Exception as e:
            self._log(f"浏览器启动失败: {e}", "error")
            return

        if not page:
            self._log("浏览器页面创建失败", "error")
            return

        # ══════════════════════════════════════════
        # 步骤 3：打开推荐页 + 判断登录
        #   - 导航到推荐页入口 URL
        #   - 每秒判断页面路由，如果是推荐页则已登录
        #   - 如果不是推荐页，最多等60秒（页面可能还在跳转）
        # ══════════════════════════════════════════
        self._log("步骤 3/7：打开推荐页，检查登录状态...")
        on_page = False
        try:
            on_page = await self._parser.ensure_on_page(page, timeout=60)
        except Exception as e:
            self._log(f"导航到推荐页异常: {e}", "error")

        # ══════════════════════════════════════════
        # 步骤 4：等待用户登录（如未登录）
        #   - ensure_on_page 超时说明未登录
        #   - 打开登录页等待用户扫码
        # ══════════════════════════════════════════
        if not on_page:
            self._log("步骤 4/7：未登录，请在浏览器窗口中扫码登录...", "warning")
            try:
                is_logged_in = await self._parser.wait_for_login(page)
            except Exception as e:
                self._log(f"等待登录异常: {e}", "error")
                return

            if not is_logged_in:
                self._log("登录超时，任务结束", "error")
                return

            self._log("登录成功，再次导航到推荐页...")
            try:
                on_page = await self._parser.ensure_on_page(page, timeout=60)
                if not on_page:
                    self._log("登录后仍无法进入推荐页，任务结束", "error")
                    return
            except Exception as e:
                self._log(f"登录后导航异常: {e}", "error")
                return
        else:
            self._log("步骤 4/7：已登录，跳过登录步骤")

        self._log("推荐页加载完成，开始筛选候选人")

        # ══════════════════════════════════════════
        # 步骤 5：主循环 - 提取候选人 → 处理候选人
        # ══════════════════════════════════════════
        self._log("步骤 5/7：开始候选人筛选主循环...")
        processed_indices = set()
        no_new_count = 0

        while self._status == TaskStatus.RUNNING:
            if self._match_count >= match_limit:
                self._log(f"已达到匹配上限 {match_limit}，任务完成")
                break

            # 5.1 等待候选人卡片加载
            try:
                cards_loaded = await self._parser.wait_for_cards(page)
                if not cards_loaded:
                    self._log("候选人卡片未加载，尝试滚动加载...")
                    try:
                        await scroll_to_load(page, config.task, max_scrolls=3)
                    except Exception:
                        pass
                    try:
                        cards_loaded = await self._parser.wait_for_cards(page, timeout=5000)
                    except Exception:
                        pass
                    if not cards_loaded:
                        self._log("候选人卡片仍未加载，可能页面结构不匹配", "warning")
            except Exception as e:
                self._log(f"等待卡片加载异常: {e}", "warning")

            # 5.2 提取候选人（平台自己实现）
            candidates = []
            try:
                candidates = await self._parser.extract_candidates(page)
            except Exception as e:
                self._log(f"提取候选人异常: {e}", "error")
                candidates = []

            new_candidates = [c for c in candidates if c.element_index not in processed_indices]

            # 5.3 没有新候选人，尝试滚动加载
            if not new_candidates:
                no_new_count += 1
                if no_new_count >= 3:
                    self._log("连续 3 次无新候选人，任务完成")
                    break

                self._log(f"当前屏无新候选人，尝试滚动加载（第{no_new_count}次）...")
                try:
                    await scroll_to_load(page, config.task, max_scrolls=3)
                except Exception as e:
                    self._log(f"滚动加载异常: {e}", "warning")

                try:
                    candidates = await self._parser.extract_candidates(page)
                    new_candidates = [c for c in candidates if c.element_index not in processed_indices]
                except Exception as e:
                    self._log(f"滚动后提取候选人异常: {e}", "error")
                    new_candidates = []

                if not new_candidates:
                    continue
            else:
                no_new_count = 0

            self._log(f"提取到 {len(new_candidates)} 个新候选人")

            # 5.4 逐个处理候选人（任何异常跳过，处理下一个）
            for candidate in new_candidates:
                if self._status != TaskStatus.RUNNING:
                    break
                if self._match_count >= match_limit:
                    break

                processed_indices.add(candidate.element_index)
                self._total_count += 1

                try:
                    await self._process_candidate(page, candidate, mode)
                    await self._maybe_take_mid_task_rest()
                except Exception as e:
                    self._log(
                        f"[{self._total_count}] 处理候选人异常，跳过: {e}",
                        "error",
                    )
                    self._failed_count += 1
                    try:
                        await self._save_candidate(candidate, CandidateStatus.FAILED, f"处理异常: {e}")
                    except Exception:
                        pass

            gc.collect()

            # 5.5 更新任务日志
            try:
                await self._update_task_log()
            except Exception as e:
                logger.warning(f"更新任务日志失败: {e}")

            # 5.6 检查是否需要停止
            if self._status == TaskStatus.STOPPING:
                self._log("任务已被用户停止")
                break

            # 5.7 滚动加载更多
            try:
                await scroll_to_load(page, config.task, max_scrolls=2)
            except Exception as e:
                self._log(f"滚动加载异常: {e}", "warning")

        # ══════════════════════════════════════════
        # 步骤 6：任务结束，输出统计
        # ══════════════════════════════════════════
        if self._match_count >= match_limit:
            final_status = "completed"
        elif self._status == TaskStatus.STOPPING:
            final_status = "stopped"
        else:
            final_status = "completed"

        self._log(
            f"步骤 6/7：任务结束 - 扫描={self._total_count}, "
            f"打招呼={self._match_count}, 跳过={self._skipped_count}"
        )
        try:
            await self._finish_task_log(final_status)
        except Exception as e:
            logger.warning(f"更新任务日志失败: {e}")

        # ══════════════════════════════════════════
        # 步骤 7：清理资源（在 finally 中调用 _cleanup）
        # ══════════════════════════════════════════
        self._log("步骤 7/7：清理资源...")

    async def _process_candidate(self, page, candidate: CandidateInfo, mode: TaskMode) -> None:
        """
        处理单个候选人：查看详情 → 筛选 → 关闭详情 → 打招呼

        每个步骤都有独立异常处理，某步失败不影响后续步骤。

        Args:
            page: Playwright Page 实例
            candidate: 候选人信息
            mode: 筛选模式
        """
        candidate_summary = candidate.name or "未知"
        detail_parts = []
        if candidate.age:
            detail_parts.append(f"年龄:{candidate.age}")
        if candidate.education:
            detail_parts.append(f"学历:{candidate.education}")
        if candidate.experience:
            detail_parts.append(f"经验:{candidate.experience}")
        if candidate.salary:
            detail_parts.append(f"薪资:{candidate.salary}")
        if candidate.skills:
            detail_parts.append(f"技能:{candidate.skills}")
        if detail_parts:
            candidate_summary += f" ({', '.join(detail_parts)})"

        self._log(f"[{self._total_count}] 筛选候选人: {candidate_summary}")
        await self._random_task_delay(
            "候选人列表查看",
            config.task.list_view_delay_min,
            config.task.list_view_delay_max,
        )

        passed: Optional[bool] = None
        reason = "未知"
        should_open_detail = True

        if mode == TaskMode.KEYWORD:
            try:
                passed, reason = await self._do_filter(candidate, mode)
                if not passed:
                    self._skipped_count += 1
                    self._log(f"  → 关键词列表初筛未通过 ({reason})", "warning")
                    try:
                        await self._save_candidate(candidate, CandidateStatus.SKIPPED, reason)
                    except Exception:
                        pass
                    return
            except Exception as e:
                self._log(f"  → 关键词列表初筛异常: {e}", "error")
                passed, reason = False, f"筛选异常: {e}"

            probability = min(max(config.task.keyword_detail_open_probability, 0), 100)
            roll = random.uniform(0, 100)
            should_open_detail = roll < probability
            decision = "打开详情" if should_open_detail else "跳过详情"
            self._log(
                f"  → 关键词模式列表初筛通过 ({reason})；"
                f"默认真人摸鱼时间详情概率 {probability}%，本次随机值 {roll:.1f}，{decision}"
            )
        elif mode == TaskMode.AI:
            self._log("  → AI 模式按详情流程处理，详情是否使用由 AI 筛选内容决定")

        # 1. 查看详情（平台自己实现）
        detail_opened = False
        if should_open_detail:
            try:
                detail_text = await self._parser.open_detail(page, candidate.element_index)
                if detail_text:
                    detail_opened = True
                    if candidate.raw_text:
                        candidate.raw_text = candidate.raw_text + " | " + detail_text
                    else:
                        candidate.raw_text = detail_text
                    detail_preview = detail_text[:200] + "..." if len(detail_text) > 200 else detail_text
                    self._log(f"  → 详情(DOM): {detail_preview}")
                    await self._random_task_delay(
                        "详情弹框打开后查看",
                        config.task.detail_view_delay_min,
                        config.task.detail_view_delay_max,
                    )
            except Exception as e:
                self._log(f"  → 打开详情异常: {e}", "warning")

        # 1.5 根据 detail_mode 选择详情获取方式
        detail_mode = config.task.detail_mode
        if detail_mode == "ocr" and detail_opened:
            try:
                screenshot_bytes = await self._parser.screenshot_detail(page)
                if screenshot_bytes:
                    screenshot_path = await self._save_screenshot(candidate, screenshot_bytes)
                    if screenshot_path:
                        self._log(f"  → 截图已保存: {screenshot_path}")

                    from utils.ocr import ocr_image_async
                    ocr_text = await ocr_image_async(screenshot_bytes)
                    if ocr_text:
                        if candidate.raw_text:
                            candidate.raw_text = candidate.raw_text + "\n[OCR]\n" + ocr_text
                        else:
                            candidate.raw_text = ocr_text
                        ocr_preview = ocr_text[:200] + "..." if len(ocr_text) > 200 else ocr_text
                        self._log(f"  → 详情(OCR): {ocr_preview}")
                    else:
                        self._log("  → OCR 识别结果为空", "warning")
            except Exception as e:
                self._log(f"  → 截图/OCR 异常: {e}", "warning")

        # 2. 筛选
        if mode == TaskMode.AI or (mode == TaskMode.KEYWORD and detail_opened):
            try:
                passed, reason = await self._do_filter(candidate, mode)
                if mode == TaskMode.KEYWORD and detail_opened:
                    self._log(f"  → 关键词详情复筛结果: {reason}")
            except Exception as e:
                self._log(f"  → 筛选异常: {e}", "error")
                passed, reason = False, f"筛选异常: {e}"

        if passed is None:
            passed, reason = False, "未执行筛选"

        # 3. 关闭详情（无论筛选是否通过都要关闭）
        if detail_opened:
            try:
                await self._parser.close_detail(page)
                await asyncio.sleep(0.5)
            except Exception as e:
                self._log(f"  → 关闭详情异常: {e}", "warning")
                try:
                    await page.keyboard.press("Escape")
                    await asyncio.sleep(0.5)
                except Exception:
                    pass

        # 4. 根据筛选结果处理
        if not passed:
            self._skipped_count += 1
            self._log(f"  → 未通过 ({reason})", "warning")
            try:
                await self._save_candidate(candidate, CandidateStatus.SKIPPED, reason)
            except Exception:
                pass
            return

        self._log(f"  → 筛选通过 ({reason})", "success")
        await self._random_task_delay(
            "打招呼前",
            config.task.greet_delay_min,
            config.task.greet_delay_max,
        )

        # 5. 打招呼（平台自己实现）
        try:
            greet_success = await self._parser.click_greet(page, candidate.element_index)
        except Exception as e:
            self._log(f"  → 打招呼异常: {e}", "error")
            greet_success = False

        if greet_success:
            self._match_count += 1
            self._log(f"  → 打招呼成功 {self._match_count} - {candidate.name}", "success")
            try:
                await self._save_candidate(candidate, CandidateStatus.GREETED, reason)
            except Exception:
                pass
            try:
                await random_delay(config.task.scroll_delay_min, config.task.scroll_delay_max)
            except Exception:
                pass
        else:
            self._log(f"  → 打招呼失败 - {candidate.name}", "warning")
            self._failed_count += 1
            try:
                await self._save_candidate(candidate, CandidateStatus.FAILED, "打招呼失败")
            except Exception:
                pass

    async def _random_task_delay(self, label: str, min_seconds: float, max_seconds: float) -> None:
        """
        执行任务节点的随机等待，并把实际等待时间输出到运行日志。

        Args:
            label: 日志中的等待场景
            min_seconds: 最小等待秒数
            max_seconds: 最大等待秒数
        """
        low = max(float(min_seconds or 0), 0.0)
        high = max(float(max_seconds or 0), 0.0)
        if high < low:
            low, high = high, low

        if high <= 0:
            return

        delay = random.uniform(low, high)
        self._log(f"  → {label}随机延迟 {delay:.1f} 秒（范围 {low:.1f}-{high:.1f} 秒）")
        await asyncio.sleep(delay)

    def _prepare_mid_task_rest(self) -> None:
        """为本次任务生成随机休息计划。"""
        min_times, max_times = self._normalized_int_range(config.task.rest_times_min, config.task.rest_times_max)
        self._rest_remaining = random.randint(min_times, max_times) if max_times > 0 else 0
        self._candidates_since_rest = 0
        self._next_rest_after = self._pick_next_rest_after()

        if self._rest_remaining > 0 and self._next_rest_after > 0:
            self._log(
                "默认真人摸鱼时间已启用："
                f"本次任务随机休息 {self._rest_remaining} 次，"
                f"首次处理 {self._next_rest_after} 个候选人后休息"
            )
        else:
            self._log("默认真人摸鱼时间未启用：本次任务不安排中途休息")

    async def _maybe_take_mid_task_rest(self) -> None:
        """处理一定数量候选人后，按配置随机休息。"""
        if self._rest_remaining <= 0 or self._next_rest_after <= 0:
            return

        self._candidates_since_rest += 1
        if self._candidates_since_rest < self._next_rest_after:
            return

        low, high = self._normalized_float_range(config.task.rest_duration_min, config.task.rest_duration_max)
        if high <= 0:
            self._rest_remaining = 0
            return

        minutes = random.uniform(low, high)
        self._log(
            "  → 默认真人摸鱼时间："
            f"已连续处理 {self._candidates_since_rest} 个候选人，"
            f"随机休息 {minutes:.1f} 分钟（剩余休息 {self._rest_remaining - 1} 次）"
        )
        await asyncio.sleep(minutes * 60)

        self._rest_remaining -= 1
        self._candidates_since_rest = 0
        self._next_rest_after = self._pick_next_rest_after()
        if self._rest_remaining > 0 and self._next_rest_after > 0:
            self._log(f"  → 休息结束；下次处理 {self._next_rest_after} 个候选人后再次休息")
        else:
            self._log("  → 休息结束；本次任务不再安排中途休息")

    def _pick_next_rest_after(self) -> int:
        low, high = self._normalized_int_range(
            config.task.rest_after_candidates_min,
            config.task.rest_after_candidates_max,
        )
        return random.randint(low, high) if high > 0 else 0

    @staticmethod
    def _normalized_int_range(min_value: int, max_value: int) -> tuple[int, int]:
        low = max(int(min_value or 0), 0)
        high = max(int(max_value or 0), 0)
        if high < low:
            low, high = high, low
        return low, high

    @staticmethod
    def _normalized_float_range(min_value: float, max_value: float) -> tuple[float, float]:
        low = max(float(min_value or 0), 0.0)
        high = max(float(max_value or 0), 0.0)
        if high < low:
            low, high = high, low
        return low, high

    async def _save_screenshot(self, candidate: CandidateInfo, screenshot_bytes: bytes) -> Optional[str]:
        """
        保存候选人详情截图到本地文件

        截图保存到 data/screenshots/ 目录下，文件名包含候选人姓名和时间戳。
        目录不存在时自动创建。

        Args:
            candidate: 候选人信息（用于生成文件名）
            screenshot_bytes: PNG 格式的截图字节数据

        Returns:
            Optional[str]: 保存的文件路径，保存失败返回 None
        """
        try:
            screenshot_dir = config.data_dir / "screenshots"
            screenshot_dir.mkdir(parents=True, exist_ok=True)

            safe_name = "".join(
                c for c in (candidate.name or "unknown") if c.isalnum() or "\u4e00" <= c <= "\u9fff"
            )[:20]
            timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
            filename = f"{safe_name}_{timestamp}.png"
            filepath = screenshot_dir / filename

            with open(filepath, "wb") as f:
                f.write(screenshot_bytes)

            return str(filepath)
        except Exception as e:
            logger.warning(f"保存截图失败: {e}")
            return None

    async def _do_filter(self, candidate: CandidateInfo, mode: TaskMode) -> tuple[bool, str]:
        """
        执行筛选逻辑

        Args:
            candidate: 候选人信息
            mode: 筛选模式

        Returns:
            tuple[bool, str]: (是否通过, 原因)
        """
        if mode == TaskMode.AI and self._ai_filter:
            result = await self._ai_filter.filter(candidate, self._job_description)
            reason = f"{result.msg}(-¥{result.cost})"
            return result.isok, reason

        if self._keyword_filter:
            result = await self._keyword_filter.filter(candidate)
            if not result.passed:
                fallback = await self._keyword_filter.fallback_filter(candidate)
                if fallback:
                    return True, result.reason
            return result.passed, result.reason

        return True, "无筛选条件"

    async def _cleanup(self) -> None:
        try:
            if self._ai_filter:
                await self._ai_filter.close()
                self._ai_filter = None
        except Exception as e:
            logger.warning(f"关闭 AI 筛选器失败: {e}")

        try:
            await self._browser_manager.stop()
        except Exception as e:
            logger.warning(f"关闭浏览器失败: {e}")

        self._keyword_filter = None
        self._parser = None
        self._async_task = None
        self._transition(TaskStatus.IDLE)

        try:
            from utils.ocr import close_ocr
            close_ocr()
        except Exception:
            pass

        gc.collect()

    async def _load_position(self, position_id: int) -> Optional[Position]:
        """
        从数据库加载岗位信息

        Args:
            position_id: 岗位 ID

        Returns:
            Position 或 None
        """
        async with async_session() as session:
            result = await session.execute(select(Position).where(Position.id == position_id))
            return result.scalar_one_or_none()

    async def _save_candidate(self, candidate: CandidateInfo, status: str, reason: str) -> None:
        """
        保存候选人信息到数据库

        Args:
            candidate: 候选人信息
            status: 处理状态
            reason: 筛选原因
        """
        async with async_session() as session:
            db_candidate = Candidate(
                position_id=self._position_id,
                name=candidate.name,
                age=candidate.age,
                education=candidate.education,
                experience=candidate.experience,
                skills=candidate.skills,
                salary=candidate.salary,
                raw_data=candidate.raw_text,
                filter_reason=reason,
                status=status,
                platform=self._parser.platform_id if self._parser else "",
                platform_user_id=candidate.platform_user_id,
            )
            session.add(db_candidate)
            await session.commit()

    async def _create_task_log(self, position_id: int, position_name: str) -> int:
        """
        创建任务日志记录

        Args:
            position_id: 岗位 ID
            position_name: 岗位名称

        Returns:
            int: 日志记录 ID
        """
        async with async_session() as session:
            task_log = TaskLog(
                position_id=position_id,
                position_name=position_name,
                status="running",
            )
            session.add(task_log)
            await session.commit()
            await session.refresh(task_log)
            return task_log.id

    async def _update_task_log(self) -> None:
        """更新任务日志的统计数据"""
        if not self._task_log_id:
            return
        async with async_session() as session:
            result = await session.execute(select(TaskLog).where(TaskLog.id == self._task_log_id))
            task_log = result.scalar_one_or_none()
            if task_log:
                task_log.total_count = self._total_count
                task_log.greeted_count = self._match_count
                task_log.skipped_count = self._skipped_count
                await session.commit()

    async def _finish_task_log(self, status: str, error_message: str = "") -> None:
        """
        结束任务日志

        Args:
            status: 最终状态
            error_message: 错误消息
        """
        if not self._task_log_id:
            return
        async with async_session() as session:
            result = await session.execute(select(TaskLog).where(TaskLog.id == self._task_log_id))
            task_log = result.scalar_one_or_none()
            if task_log:
                task_log.status = status
                task_log.total_count = self._total_count
                task_log.greeted_count = self._match_count
                task_log.skipped_count = self._skipped_count
                task_log.finished_at = datetime.now()
                if error_message:
                    task_log.error_message = error_message
                await session.commit()


task_orchestrator = TaskOrchestrator()
