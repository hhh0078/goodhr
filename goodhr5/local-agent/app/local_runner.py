"""本文件负责管理本地任务运行状态和执行入口。"""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from datetime import datetime, timezone
from pathlib import Path

from app.boss_runtime import (
    extract_visible_candidates,
    fetch_candidate_detail_text,
    greet_candidate_by_index,
    scroll_candidate_list,
)
from app.local_ai_decision import (
    review_candidate_for_greet,
    score_candidate_for_detail,
    score_candidate_for_greet,
    should_review_greet_score,
)
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
KEYWORD_MAX_SCAN_ROUNDS = 20
KEYWORD_MAX_IDLE_ROUNDS = 2


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
            if _task_mode(task) == "ai":
                await self._run_ai_task(task, page, stop_event)
                return
            await self._run_keyword_task(task, page, stop_event)
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

    async def _scan_once(self, task: dict, page, stop_event: asyncio.Event) -> int:
        """
        执行一次候选人提取并保存。

        Args:
            task: 本地任务。
            page: Playwright 页面对象。
            stop_event: 停止信号。

        Returns:
            int: 本轮保存的候选人数量。
        """
        if stop_event.is_set():
            return 0
        task_id = str(task.get("id") or "")
        candidates = await extract_visible_candidates(page)
        candidates, skipped_count = self._apply_keyword_filter(task, candidates)
        for candidate in candidates:
            save_local_candidate(task_id, candidate)
        if candidates:
            increment_local_task_counts(task_id, scanned=len(candidates), skipped=skipped_count)
            add_local_task_log(task_id, "info", f"已提取并保存 {len(candidates)} 个可见候选人")
            if skipped_count > 0:
                add_local_task_log(task_id, "info", f"关键词筛选跳过 {skipped_count} 个候选人")
        else:
            add_local_task_log(task_id, "warning", "当前页面未提取到可见候选人，请确认账号已登录且页面在推荐列表")
        return len(candidates)

    async def _run_keyword_task(self, task: dict, page, stop_event: asyncio.Event) -> None:
        """
        多轮执行 Boss 关键词筛选和打招呼任务。

        Args:
            task: 本地任务。
            page: Playwright 页面对象。
            stop_event: 停止信号。
        """
        task_id = str(task.get("id") or "")
        match_limit = int(task.get("match_limit") or 0)
        greeted_total = 0
        failed_total = 0
        idle_rounds = 0
        seen_ids: set[str] = set()

        for round_index in range(KEYWORD_MAX_SCAN_ROUNDS):
            if stop_event.is_set():
                update_local_task_status(task_id, "stopped")
                add_local_task_log(task_id, "info", "任务收到停止信号，已结束本轮扫描")
                return
            if match_limit > 0 and greeted_total >= match_limit:
                add_local_task_log(task_id, "info", f"已达到本次打招呼上限：{match_limit}")
                update_local_task_status(task_id, "completed")
                return
            candidates = await extract_visible_candidates(page)
            fresh_candidates = [candidate for candidate in candidates if str(candidate.get("id") or "") not in seen_ids]
            for candidate in fresh_candidates:
                seen_ids.add(str(candidate.get("id") or ""))
            if not fresh_candidates:
                idle_rounds += 1
                add_local_task_log(task_id, "info", f"第 {round_index + 1} 轮未发现新候选人")
                if idle_rounds >= KEYWORD_MAX_IDLE_ROUNDS:
                    add_local_task_log(task_id, "info", "连续未发现新候选人，任务已完成")
                    update_local_task_status(task_id, "completed")
                    return
                await scroll_candidate_list(page)
                continue

            idle_rounds = 0
            fresh_candidates, skipped_count = self._apply_keyword_filter(task, fresh_candidates)
            remaining = max(0, match_limit - greeted_total) if match_limit > 0 else 0
            greeted_count, failed_count, limit_skipped_count = await self._greet_keyword_candidates(
                task,
                page,
                fresh_candidates,
                stop_event,
                max_greet=remaining,
            )
            skipped_count += limit_skipped_count
            greeted_total += greeted_count
            failed_total += failed_count
            for candidate in fresh_candidates:
                save_local_candidate(task_id, candidate)
            increment_local_task_counts(
                task_id,
                scanned=len(fresh_candidates),
                greeted=greeted_count,
                skipped=skipped_count,
                failed=failed_count,
            )
            add_local_task_log(task_id, "info", f"第 {round_index + 1} 轮保存 {len(fresh_candidates)} 个新候选人")
            if skipped_count > 0:
                add_local_task_log(task_id, "info", f"本轮关键词筛选跳过 {skipped_count} 个候选人")
            if greeted_count > 0:
                add_local_task_log(task_id, "info", f"本轮打招呼成功 {greeted_count} 个候选人")
            if failed_count > 0:
                add_local_task_log(task_id, "warning", f"本轮打招呼失败 {failed_count} 个候选人")
            if match_limit > 0 and greeted_total >= match_limit:
                add_local_task_log(task_id, "info", f"已达到本次打招呼上限：{match_limit}")
                update_local_task_status(task_id, "completed")
                return
            await scroll_candidate_list(page)

        add_local_task_log(task_id, "info", f"已完成最大扫描轮数：{KEYWORD_MAX_SCAN_ROUNDS}")
        if failed_total > 0:
            add_local_task_log(task_id, "warning", f"本次任务累计打招呼失败 {failed_total} 个候选人")
        update_local_task_status(task_id, "completed")

    async def _run_ai_task(self, task: dict, page, stop_event: asyncio.Event) -> None:
        """
        多轮执行 Boss AI 筛选和打招呼任务。

        Args:
            task: 本地任务。
            page: Playwright 页面对象。
            stop_event: 停止信号。
        """
        task_id = str(task.get("id") or "")
        match_limit = int(task.get("match_limit") or 0)
        greeted_total = 0
        failed_total = 0
        idle_rounds = 0
        seen_ids: set[str] = set()

        for round_index in range(KEYWORD_MAX_SCAN_ROUNDS):
            if stop_event.is_set():
                update_local_task_status(task_id, "stopped")
                add_local_task_log(task_id, "info", "任务收到停止信号，已结束本轮 AI 筛选")
                return
            if match_limit > 0 and greeted_total >= match_limit:
                add_local_task_log(task_id, "info", f"已达到本次打招呼上限：{match_limit}")
                update_local_task_status(task_id, "completed")
                return
            candidates = await extract_visible_candidates(page)
            fresh_candidates = [candidate for candidate in candidates if str(candidate.get("id") or "") not in seen_ids]
            for candidate in fresh_candidates:
                seen_ids.add(str(candidate.get("id") or ""))
            if not fresh_candidates:
                idle_rounds += 1
                add_local_task_log(task_id, "info", f"第 {round_index + 1} 轮未发现新候选人")
                if idle_rounds >= KEYWORD_MAX_IDLE_ROUNDS:
                    add_local_task_log(task_id, "info", "连续未发现新候选人，AI 任务已完成")
                    update_local_task_status(task_id, "completed")
                    return
                await scroll_candidate_list(page)
                continue

            idle_rounds = 0
            greeted_count, skipped_count, failed_count, limit_skipped_count = await self._score_and_greet_ai_candidates(
                task,
                page,
                fresh_candidates,
                stop_event,
                max_greet=max(0, match_limit - greeted_total) if match_limit > 0 else 0,
            )
            skipped_count += limit_skipped_count
            greeted_total += greeted_count
            failed_total += failed_count
            for candidate in fresh_candidates:
                save_local_candidate(task_id, candidate)
            increment_local_task_counts(
                task_id,
                scanned=len(fresh_candidates),
                greeted=greeted_count,
                skipped=skipped_count,
                failed=failed_count,
            )
            add_local_task_log(
                task_id,
                "info",
                f"第 {round_index + 1} 轮 AI 保存 {len(fresh_candidates)} 个新候选人",
            )
            if skipped_count > 0:
                add_local_task_log(task_id, "info", f"本轮 AI 筛选跳过 {skipped_count} 个候选人")
            if greeted_count > 0:
                add_local_task_log(task_id, "info", f"本轮 AI 打招呼成功 {greeted_count} 个候选人")
            if failed_count > 0:
                add_local_task_log(task_id, "warning", f"本轮 AI 处理失败 {failed_count} 个候选人")
            if match_limit > 0 and greeted_total >= match_limit:
                add_local_task_log(task_id, "info", f"已达到本次打招呼上限：{match_limit}")
                update_local_task_status(task_id, "completed")
                return
            await scroll_candidate_list(page)

        add_local_task_log(task_id, "info", f"已完成最大 AI 扫描轮数：{KEYWORD_MAX_SCAN_ROUNDS}")
        if failed_total > 0:
            add_local_task_log(task_id, "warning", f"本次 AI 任务累计失败 {failed_total} 个候选人")
        update_local_task_status(task_id, "completed")

    async def _score_and_greet_ai_candidates(
        self,
        task: dict,
        page,
        candidates: list[dict],
        stop_event: asyncio.Event,
        max_greet: int = 0,
    ) -> tuple[int, int, int, int]:
        """
        对候选人执行 AI 评分并按结果打招呼。

        Args:
            task: 本地任务。
            page: Playwright 页面对象。
            candidates: 候选人列表。
            stop_event: 停止信号。
            max_greet: 本轮最多打招呼数量，0 表示不限制。

        Returns:
            tuple[int, int, int, int]: 成功、跳过、失败和达到上限跳过数量。
        """
        task_id = str(task.get("id") or "")
        greeted = 0
        skipped = 0
        failed = 0
        limit_skipped = 0
        for candidate in candidates:
            if stop_event.is_set():
                break
            name = str(candidate.get("name") or candidate.get("candidate_name") or "候选人")
            if max_greet > 0 and greeted >= max_greet:
                candidate["status"] = "skipped"
                candidate["skip_reason"] = "已达到任务打招呼上限"
                limit_skipped += 1
                continue
            try:
                detail_decision = await self._maybe_fetch_ai_detail(task, page, candidate, name)
                if detail_decision is not None and not detail_decision["should_open_detail"]:
                    candidate["status"] = "skipped"
                    score_text = f"{detail_decision['score']:.1f}/{detail_decision['threshold']:.1f}"
                    candidate["skip_reason"] = f"详情评分低于阈值：{score_text}，{detail_decision['reason']}"
                    skipped += 1
                    add_local_task_log(
                        task_id,
                        "info",
                        f"{name}详情评分跳过：{detail_decision['reason']}"
                        f"（评分={detail_decision['score']:.1f}）",
                    )
                    continue

                add_local_task_log(task_id, "info", f"正在 AI 打招呼评分：{name}")
                decision = await score_candidate_for_greet(task, candidate)
                final_decision = await self._maybe_review_ai_greet(task, candidate, decision, name)
                candidate["ai_greet_score"] = final_decision["score"]
                candidate["ai_greet_reason"] = final_decision["reason"]
                candidate["ai_greet_threshold"] = final_decision["threshold"]
                candidate["ai_usage"] = final_decision.get("usage") or {}
                if not final_decision["should_greet"]:
                    candidate["status"] = "skipped"
                    score_text = f"{final_decision['score']:.1f}/{final_decision['threshold']:.1f}"
                    candidate["skip_reason"] = f"AI评分低于阈值：{score_text}，{final_decision['reason']}"
                    skipped += 1
                    add_local_task_log(
                        task_id,
                        "info",
                        f"{name}AI筛选跳过：{final_decision['reason']}（评分={final_decision['score']:.1f}）",
                    )
                    continue
                add_local_task_log(
                    task_id,
                    "info",
                    f"{name}AI通过：{final_decision['reason']}（评分={final_decision['score']:.1f}）",
                )
                await greet_candidate_by_index(page, int(candidate.get("card_index") or 0))
                candidate["status"] = "greeted"
                candidate["greeted_at"] = _now_iso()
                greeted += 1
                add_local_task_log(task_id, "info", f"{name}打招呼成功")
            except Exception as exc:
                candidate["status"] = "failed"
                candidate["error"] = str(exc)
                failed += 1
                add_local_task_log(task_id, "error", f"{name}AI处理失败：{exc}")
        return greeted, skipped, failed, limit_skipped

    async def _maybe_fetch_ai_detail(self, task: dict, page, candidate: dict, name: str) -> dict | None:
        """
        按 AI 详情评分决定是否打开并提取候选人详情。

        Args:
            task: 本地任务。
            page: Playwright 页面对象。
            candidate: 候选人信息。
            name: 候选人展示名。

        Returns:
            dict | None: 详情评分结果。
        """
        task_id = str(task.get("id") or "")
        add_local_task_log(task_id, "info", f"正在 AI 详情评分：{name}")
        decision = await score_candidate_for_detail(task, candidate)
        candidate["ai_detail_score"] = decision["score"]
        candidate["ai_detail_reason"] = decision["reason"]
        candidate["ai_detail_threshold"] = decision["threshold"]
        if not decision["should_open_detail"]:
            return decision
        try:
            add_local_task_log(task_id, "info", f"{name}详情评分通过，正在打开详情")
            detail_text = await fetch_candidate_detail_text(page, int(candidate.get("card_index") or 0))
        except Exception as exc:
            add_local_task_log(task_id, "warning", f"{name}详情提取失败，沿用基础信息：{exc}")
            return decision
        merged_text = _merge_text(candidate.get("filter_text") or candidate.get("raw_text") or "", detail_text)
        candidate["detail_text"] = detail_text
        candidate["filter_text"] = merged_text
        candidate["raw_text"] = merged_text
        add_local_task_log(task_id, "info", f"{name}详情文本已提取，长度={len(detail_text)}")
        return decision

    async def _maybe_review_ai_greet(
        self,
        task: dict,
        candidate: dict,
        decision: dict,
        name: str,
    ) -> dict:
        """
        对临界打招呼评分执行 AI 复核。

        Args:
            task: 本地任务。
            candidate: 候选人信息。
            decision: 首次打招呼评分。
            name: 候选人展示名。

        Returns:
            dict: 最终评分结果。
        """
        position = task.get("position_snapshot") if isinstance(task.get("position_snapshot"), dict) else {}
        if not should_review_greet_score(position, float(decision.get("score") or 0)):
            return decision
        task_id = str(task.get("id") or "")
        try:
            add_local_task_log(task_id, "info", f"{name}评分接近阈值，开始 AI 复核")
            review = await review_candidate_for_greet(task, candidate)
            candidate["ai_review_score"] = review["score"]
            candidate["ai_review_reason"] = review["reason"]
            add_local_task_log(task_id, "info", f"{name}复核评分：{review['score']:.1f}，{review['reason']}")
            return review
        except Exception as exc:
            add_local_task_log(task_id, "warning", f"{name}AI复核失败，沿用首次评分：{exc}")
            return decision

    def _apply_keyword_filter(self, task: dict, candidates: list[dict]) -> tuple[list[dict], int]:
        """
        对候选人执行本地关键词筛选。

        Args:
            task: 本地任务。
            candidates: 候选人列表。

        Returns:
            tuple[list[dict], int]: 更新后的候选人列表和跳过数量。
        """
        if _task_mode(task) == "ai":
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
        max_greet: int = 0,
    ) -> tuple[int, int, int]:
        """
        对关键词筛选通过的候选人执行打招呼。

        Args:
            task: 本地任务。
            page: Playwright 页面对象。
            candidates: 候选人列表。
            stop_event: 停止信号。
            max_greet: 本轮最多打招呼数量，0 表示不限制。

        Returns:
            tuple[int, int, int]: 打招呼成功数量、失败数量和达到上限跳过数量。
        """
        task_id = str(task.get("id") or "")
        if _task_mode(task) == "ai":
            return 0, 0, 0
        greeted = 0
        failed = 0
        limit_skipped = 0
        for candidate in candidates:
            if stop_event.is_set():
                break
            if candidate.get("status") != "passed":
                continue
            if max_greet > 0 and greeted >= max_greet:
                candidate["status"] = "skipped"
                candidate["skip_reason"] = "已达到任务打招呼上限"
                limit_skipped += 1
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
        return greeted, failed, limit_skipped

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


def _task_mode(task: dict) -> str:
    """
    读取任务模式。

    Args:
        task: 本地任务。

    Returns:
        str: 任务模式。
    """
    return str(task.get("mode") or "").strip().lower()


def _merge_text(base_text: object, detail_text: object) -> str:
    """
    合并候选人基础文本和详情文本。

    Args:
        base_text: 基础文本。
        detail_text: 详情文本。

    Returns:
        str: 合并后的文本。
    """
    parts = [str(base_text or "").strip(), str(detail_text or "").strip()]
    return "\n\n".join(part for part in parts if part)


def _now_iso() -> str:
    """
    返回当前 UTC 时间字符串。

    Returns:
        str: ISO 格式时间。
    """
    return datetime.now(timezone.utc).isoformat()
