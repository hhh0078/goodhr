"""本文件负责管理本地任务运行状态和执行入口。"""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from datetime import datetime, timezone
from pathlib import Path

from app.boss_runtime import extract_visible_candidates, greet_candidate_by_index
from app.local_tasks import (
    add_local_task_log,
    get_local_task,
    increment_local_task_counts,
    save_local_candidate,
    update_local_task_status,
)
from app.paths import data_dir


VerifySubscription = Callable[[], Awaitable[dict]]
BOSS_ENTRY_URL = "https://www.zhipin.com/web/chat/recommend"


class LocalTaskRunner:
    """
    本地任务运行器。

    当前先负责运行锁、会员校验、状态流转和日志入口，后续 Boss 页面主流程会接入这里。
    """

    def __init__(self, browser_manager=None) -> None:
        """
        初始化本地任务运行器。

        Args:
            browser_manager: 浏览器生命周期管理器。
        """
        self._browser_manager = browser_manager
        self._tasks: dict[str, asyncio.Task] = {}
        self._stop_events: dict[str, asyncio.Event] = {}

    async def start(self, task_id: str, verify_subscription: VerifySubscription) -> dict:
        """
        启动本地任务。

        Args:
            task_id: 本地任务 ID。
            verify_subscription: 会员校验回调。

        Returns:
            dict: 启动结果。
        """
        self._cleanup_finished()
        if task_id in self._tasks:
            raise RuntimeError("任务正在运行")
        task = get_local_task(task_id)
        subscription = await verify_subscription()
        if not subscription.get("active"):
            add_local_task_log(task_id, "error", "会员已到期，请先订阅后再开始任务")
            update_local_task_status(task_id, "failed")
            raise PermissionError("会员已到期，请先订阅后再开始任务")

        stop_event = asyncio.Event()
        self._stop_events[task_id] = stop_event
        update_local_task_status(task_id, "running")
        add_local_task_log(task_id, "info", "本地任务运行器已启动")
        running_task = asyncio.create_task(self._run(task, stop_event))
        self._tasks[task_id] = running_task
        return {"ok": True, "message": "本地任务已启动", "subscription": subscription}

    async def stop(self, task_id: str) -> dict:
        """
        停止本地任务。

        Args:
            task_id: 本地任务 ID。

        Returns:
            dict: 停止结果。
        """
        stop_event = self._stop_events.get(task_id)
        if stop_event is not None:
            stop_event.set()
        running_task = self._tasks.get(task_id)
        if running_task is not None:
            running_task.cancel()
            try:
                await running_task
            except asyncio.CancelledError:
                pass
        self._tasks.pop(task_id, None)
        self._stop_events.pop(task_id, None)
        update_local_task_status(task_id, "stopped")
        add_local_task_log(task_id, "info", "本地任务已停止")
        return {"ok": True, "message": "任务已停止"}

    def status(self, task_id: str) -> dict:
        """
        查询本地任务运行状态。

        Args:
            task_id: 本地任务 ID。

        Returns:
            dict: 运行状态。
        """
        self._cleanup_finished()
        return {"ok": True, "running": task_id in self._tasks}

    async def _run(self, task: dict, stop_event: asyncio.Event) -> None:
        """
        执行本地任务主流程。

        Args:
            task: 本地任务。
            stop_event: 停止信号。
        """
        task_id = str(task.get("id") or "")
        try:
            if stop_event.is_set():
                update_local_task_status(task_id, "stopped")
                return
            platform_id = str(task.get("platform_id") or "boss").strip().lower()
            add_local_task_log(task_id, "info", f"本地执行参数已准备：platform={platform_id}")
            if platform_id != "boss":
                add_local_task_log(task_id, "error", f"暂不支持本地执行平台：{platform_id}")
                update_local_task_status(task_id, "failed")
                return
            page = await self._open_boss_entry(task, stop_event)
            if stop_event.is_set():
                update_local_task_status(task_id, "stopped")
                return
            candidates = await extract_visible_candidates(page)
            candidates, skipped_count = self._apply_keyword_filter(task, candidates)
            greeted_count, failed_count = await self._greet_keyword_candidates(task, page, candidates, stop_event)
            for candidate in candidates:
                save_local_candidate(task_id, candidate)
            if candidates:
                increment_local_task_counts(
                    task_id,
                    scanned=len(candidates),
                    greeted=greeted_count,
                    skipped=skipped_count,
                    failed=failed_count,
                )
                add_local_task_log(task_id, "info", f"已提取并保存 {len(candidates)} 个可见候选人")
                if skipped_count > 0:
                    add_local_task_log(task_id, "info", f"关键词筛选跳过 {skipped_count} 个候选人")
                if greeted_count > 0:
                    add_local_task_log(task_id, "info", f"关键词筛选通过并打招呼 {greeted_count} 个候选人")
                if failed_count > 0:
                    add_local_task_log(task_id, "warning", f"打招呼失败 {failed_count} 个候选人")
            else:
                add_local_task_log(task_id, "warning", "当前页面未提取到可见候选人，请确认账号已登录且页面在推荐列表")
            add_local_task_log(task_id, "warning", "Boss AI筛选和打招呼流程正在迁移中，当前版本先完成本地扫描入库")
            update_local_task_status(task_id, "pending")
        except asyncio.CancelledError:
            update_local_task_status(task_id, "stopped")
            raise
        except Exception as exc:
            add_local_task_log(task_id, "error", f"本地任务执行失败：{exc}")
            update_local_task_status(task_id, "failed")
        finally:
            self._tasks.pop(task_id, None)
            self._stop_events.pop(task_id, None)

    def _cleanup_finished(self) -> None:
        """
        清理已经结束的运行任务。
        """
        for task_id, running_task in list(self._tasks.items()):
            if running_task.done():
                self._tasks.pop(task_id, None)
                self._stop_events.pop(task_id, None)

    def _apply_keyword_filter(self, task: dict, candidates: list[dict]) -> tuple[list[dict], int]:
        """
        对候选人执行本地关键词筛选。

        Args:
            task: 本地任务。
            candidates: 候选人列表。

        Returns:
            tuple[list[dict], int]: 更新后的候选人列表和跳过数量。
        """
        if str(task.get("mode") or "").strip().lower() == "ai":
            return candidates, 0
        position = task.get("position_snapshot") or {}
        keywords = _string_list(position.get("keywords"))
        excludes = _string_list(position.get("exclude_keywords") or position.get("exclude"))
        is_and_mode = bool(position.get("is_and_mode"))
        skipped = 0
        result: list[dict] = []
        for candidate in candidates:
            text = str(candidate.get("filter_text") or candidate.get("raw_text") or "").lower()
            matched_excludes = [word for word in excludes if word.lower() in text]
            if matched_excludes:
                candidate["status"] = "skipped"
                candidate["skip_reason"] = "命中排除词：" + "、".join(matched_excludes)
                skipped += 1
                result.append(candidate)
                continue
            if keywords:
                matched_keywords = [word for word in keywords if word.lower() in text]
                passed = len(matched_keywords) == len(keywords) if is_and_mode else len(matched_keywords) > 0
                if not passed:
                    candidate["status"] = "skipped"
                    candidate["skip_reason"] = "未命中关键词"
                    skipped += 1
                    result.append(candidate)
                    continue
                candidate["matched_keywords"] = matched_keywords
            candidate["status"] = "passed"
            result.append(candidate)
        return result, skipped

    async def _greet_keyword_candidates(
        self,
        task: dict,
        page,
        candidates: list[dict],
        stop_event: asyncio.Event,
    ) -> tuple[int, int]:
        """
        对关键词筛选通过的候选人执行打招呼。

        Args:
            task: 本地任务。
            page: Playwright 页面对象。
            candidates: 候选人列表。
            stop_event: 停止信号。

        Returns:
            tuple[int, int]: 打招呼成功数量和失败数量。
        """
        task_id = str(task.get("id") or "")
        if str(task.get("mode") or "").strip().lower() == "ai":
            return 0, 0
        match_limit = int(task.get("match_limit") or 0)
        greeted = 0
        failed = 0
        for candidate in candidates:
            if stop_event.is_set():
                break
            if candidate.get("status") != "passed":
                continue
            if match_limit > 0 and greeted >= match_limit:
                candidate["status"] = "skipped"
                candidate["skip_reason"] = "已达到任务打招呼上限"
                continue
            name = str(candidate.get("name") or candidate.get("candidate_name") or "候选人")
            try:
                add_local_task_log(task_id, "info", f"正在给{name}打招呼")
                await greet_candidate_by_index(page, int(candidate.get("card_index") or 0))
                candidate["status"] = "greeted"
                candidate["greeted_at"] = _now_iso()
                greeted += 1
                add_local_task_log(task_id, "info", f"{name}打招呼成功")
            except Exception as exc:
                candidate["status"] = "failed"
                candidate["error"] = str(exc)
                failed += 1
                add_local_task_log(task_id, "error", f"{name}打招呼失败：{exc}")
        return greeted, failed

    async def _open_boss_entry(self, task: dict, stop_event: asyncio.Event):
        """
        启动浏览器并打开 Boss 推荐页。

        Args:
            task: 本地任务。
            stop_event: 停止信号。
        """
        task_id = str(task.get("id") or "")
        if self._browser_manager is None:
            add_local_task_log(task_id, "warning", "浏览器管理器未初始化，跳过打开 Boss 推荐页")
            return None
        account_id = str(task.get("platform_account_id") or "boss_default")
        user_data_dir = _profile_dir(account_id)
        add_local_task_log(task_id, "info", f"正在启动本地浏览器：profile={account_id}")
        await self._browser_manager.start(
            persistent=True,
            user_data_dir=str(user_data_dir),
            headless=False,
            humanize=True,
        )
        if stop_event.is_set():
            return None
        page = await self._browser_manager.ensure_page("default")
        if page is None:
            raise RuntimeError("浏览器页面创建失败")
        add_local_task_log(task_id, "info", f"正在打开 Boss 推荐页：{BOSS_ENTRY_URL}")
        await page.goto(BOSS_ENTRY_URL, wait_until="domcontentloaded", timeout=60000)
        add_local_task_log(task_id, "info", "Boss 推荐页已打开")
        return page


def _profile_dir(name: str) -> Path:
    """
    计算本地浏览器 profile 目录。

    Args:
        name: profile 名称。

    Returns:
        Path: cookies 下的安全 profile 路径。
    """
    safe_name = "".join(ch if ch.isalnum() or ch in "-_" else "_" for ch in str(name)).strip("_") or "default"
    return data_dir().parent / "cookies" / safe_name


def _string_list(value) -> list[str]:
    """
    将配置值转换为字符串列表。

    Args:
        value: 原始配置值。

    Returns:
        list[str]: 字符串列表。
    """
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    if isinstance(value, str):
        return [item.strip() for item in value.replace(",", " ").split() if item.strip()]
    return []


def _now_iso() -> str:
    """
    返回当前 UTC 时间字符串。

    Returns:
        str: ISO 格式时间。
    """
    return datetime.now(timezone.utc).isoformat()
