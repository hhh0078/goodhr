"""
GoodHR 自动化工具 - 任务编排器

协调浏览器、平台解析器、筛选引擎的运行流程，
实现候选人自动筛选和打招呼的完整任务循环。
支持 AI 模式和关键词模式，可动态启停。
"""

import asyncio
from datetime import datetime
from enum import Enum
from typing import Callable, Optional

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from core.browser import BrowserManager
from core.filter.ai_filter import AIFilter
from core.filter.keyword import KeywordFilter
from core.humanize import random_delay, scroll_to_load
from core.platform.base import BaseParser, CandidateInfo
from core.platform.boss import BossParser
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
    COMPLETED = "completed"
    FAILED = "failed"


def get_parser(platform_id: str = "boss") -> BaseParser:
    """
    根据平台 ID 获取对应的解析器实例

    Args:
        platform_id: 平台标识

    Returns:
        BaseParser: 平台解析器实例
    """
    parsers = {"boss": BossParser}
    parser_cls = parsers.get(platform_id, BossParser)
    return parser_cls()


class TaskOrchestrator:
    """
    任务编排器

    管理候选人筛选任务的完整生命周期：
    启动 → 登录验证 → 滚动提取 → 筛选 → 打招呼 → 记录结果

    支持动态启停、日志回调、运行状态查询。
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

        self._on_log: Optional[Callable] = None
        self._position_id: Optional[int] = None
        self._job_description: str = ""

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
        logger.log(getattr(logger, level, logger.info), message)
        if self._on_log:
            self._on_log(message, level)

    async def start(
        self,
        position_id: int,
        mode: TaskMode = TaskMode.AI,
        match_limit: Optional[int] = None,
        platform_id: str = "boss",
    ) -> None:
        """
        启动候选人筛选任务

        Args:
            position_id: 岗位 ID
            mode: 筛选模式（AI / 关键词）
            match_limit: 匹配上限，None 使用全局配置
            platform_id: 平台 ID
        """
        if self._status == TaskStatus.RUNNING:
            self._log("任务已在运行中", "warning")
            return

        self._position_id = position_id
        self._match_count = 0
        self._total_count = 0
        self._skipped_count = 0
        self._parser = get_parser(platform_id)

        position = await self._load_position(position_id)
        if not position:
            self._log(f"岗位 ID {position_id} 不存在", "error")
            return

        self._job_description = position.description

        if mode == TaskMode.AI:
            self._ai_filter = AIFilter()
            if not config.ai.api_key:
                self._log("AI模式需要配置 API Key", "error")
                return
        else:
            keywords = [k.strip() for k in position.keywords.split(",") if k.strip()]
            exclude_keywords = [k.strip() for k in position.exclude_keywords.split(",") if k.strip()]
            self._keyword_filter = KeywordFilter(
                keywords=keywords,
                exclude_keywords=exclude_keywords,
                is_and_mode=position.is_and_mode,
            )

        match_limit = match_limit or config.task.match_limit

        self._task_log_id = await self._create_task_log(position_id, position.name)
        self._status = TaskStatus.RUNNING
        self._log(f"任务启动: 岗位={position.name}, 模式={mode.value}, 上限={match_limit}")

        try:
            await self._browser_manager.start(persistent=True)
            page = await self._browser_manager.new_page("main")

            is_logged_in = await self._parser.check_login_status(page)
            if not is_logged_in:
                self._log("未登录，等待扫码登录...")
                is_logged_in = await self._parser.wait_for_login(page)
                if not is_logged_in:
                    await self._finish_task("failed", "登录超时")
                    return

            self._log("登录成功，导航到推荐页...")
            await self._parser.navigate_to_recommend(page)

            await self._run_loop(page, mode, match_limit)

        except Exception as e:
            self._log(f"任务异常: {e}", "error")
            await self._finish_task("failed", str(e))
        finally:
            if self._ai_filter:
                await self._ai_filter.close()
            await self._browser_manager.stop()

    async def stop(self) -> None:
        """停止当前任务"""
        if self._status != TaskStatus.RUNNING:
            return
        self._status = TaskStatus.STOPPING
        self._log("正在停止任务...")

    async def _run_loop(self, page, mode: TaskMode, match_limit: int) -> None:
        """
        主任务循环：滚动提取 → 筛选 → 打招呼

        Args:
            page: Playwright Page 实例
            mode: 筛选模式
            match_limit: 匹配上限
        """
        processed_indices = set()

        while self._status == TaskStatus.RUNNING:
            if self._match_count >= match_limit:
                self._log(f"已达到匹配上限 {match_limit}，自动停止", "warning")
                await self._finish_task("completed")
                return

            await self._parser.wait_for_cards(page)
            candidates = await self._parser.extract_candidates(page)

            new_candidates = [c for c in candidates if c.element_index not in processed_indices]

            if not new_candidates:
                self._log("当前屏无新候选人，尝试滚动加载...")
                await scroll_to_load(page, config.task, max_scrolls=3)
                candidates = await self._parser.extract_candidates(page)
                new_candidates = [c for c in candidates if c.element_index not in processed_indices]

                if not new_candidates:
                    self._log("已无更多候选人，任务完成")
                    await self._finish_task("completed")
                    return

            for candidate in new_candidates:
                if self._status != TaskStatus.RUNNING:
                    break

                if self._match_count >= match_limit:
                    break

                processed_indices.add(candidate.element_index)
                self._total_count += 1

                self._log(f"正在筛选 {candidate.name or '未知'} ({self._total_count})")

                passed, reason = await self._do_filter(candidate, mode)

                if not passed:
                    self._skipped_count += 1
                    self._log(f"  → 未通过 ({reason})", "warning")
                    await self._save_candidate(candidate, CandidateStatus.SKIPPED, reason)
                    continue

                self._log(f"  → 筛选通过 ({reason})", "success")

                greet_success = await self._parser.click_greet(page, candidate.element_index)
                if greet_success:
                    self._match_count += 1
                    self._log(f"打招呼成功 {self._match_count}/{match_limit} - {candidate.name}", "success")
                    await self._save_candidate(candidate, CandidateStatus.GREETED, reason)
                    await random_delay(config.task.scroll_delay_min, config.task.scroll_delay_max)
                else:
                    self._log(f"打招呼失败: {candidate.name}", "warning")
                    await self._save_candidate(candidate, CandidateStatus.FAILED, "打招呼失败")

            await self._update_task_log()

            if self._status == TaskStatus.STOPPING:
                await self._finish_task("stopped", "用户手动停止")
                return

            await scroll_to_load(page, config.task, max_scrolls=2)

    async def _do_filter(self, candidate: CandidateInfo, mode: TaskMode) -> tuple[bool, str]:
        """
        执行筛选逻辑

        根据模式选择 AI 筛选或关键词筛选，
        AI 模式下 Boss 直聘跳过精筛（信息充足）。

        Args:
            candidate: 候选人信息
            mode: 筛选模式

        Returns:
            tuple[bool, str]: (是否通过, 原因)
        """
        if mode == TaskMode.AI and self._ai_filter:
            result = await self._ai_filter.filter(
                candidate,
                self._job_description,
            )
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
                platform=self._parser.platform_id,
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

    async def _finish_task(self, status: str, error_message: str = "") -> None:
        """
        结束任务，更新状态和日志

        Args:
            status: 最终状态
            error_message: 错误消息（如果有）
        """
        self._status = TaskStatus.COMPLETED if status == "completed" else TaskStatus.FAILED

        if self._task_log_id:
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

        self._log(f"任务结束: status={status}, 扫描={self._total_count}, 打招呼={self._match_count}, 跳过={self._skipped_count}")


task_orchestrator = TaskOrchestrator()
