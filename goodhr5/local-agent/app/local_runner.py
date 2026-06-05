"""本文件负责管理本地任务运行状态和执行入口。"""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable

from app.local_tasks import add_local_task_log, get_local_task, update_local_task_status


VerifySubscription = Callable[[], Awaitable[dict]]


class LocalTaskRunner:
    """
    本地任务运行器。

    当前先负责运行锁、会员校验、状态流转和日志入口，后续 Boss 页面主流程会接入这里。
    """

    def __init__(self) -> None:
        """初始化本地任务运行器。"""
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
            add_local_task_log(task_id, "warning", "Boss 本地主流程正在迁移中，当前版本先完成本地运行器和数据闭环")
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
