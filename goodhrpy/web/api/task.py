"""
GoodHR 自动化工具 - 任务管理 API

提供任务的启动、停止、状态查询和日志查看接口。
任务日志通过内存队列实时推送给前端轮询接口。
状态机保证任务不会重复启动，API 层只需简单判断即可。
"""

import asyncio
from collections import deque
from typing import List

from fastapi import APIRouter, HTTPException, Query
from sqlalchemy import select

from core.task import TaskMode, TaskStatus, task_orchestrator
from models.database import async_session
from models.task_log import TaskLog, TaskLogResponse, TaskStartRequest
from utils.logger import get_logger

logger = get_logger("task_api")

router = APIRouter()

_log_queue: deque = deque(maxlen=200)


def _on_task_log(message: str, level: str = "info") -> None:
    """
    任务日志回调，将日志推入内存队列供前端轮询

    Args:
        message: 日志消息
        level: 日志级别
    """
    import time

    _log_queue.append(
        {
            "time": time.strftime("%H:%M:%S"),
            "message": message,
            "level": level,
        }
    )


task_orchestrator.on_log(_on_task_log)


@router.post("/start", response_model=dict)
async def start_task(request: TaskStartRequest):
    """
    启动候选人筛选任务

    状态机保证同一时间只有一个任务运行。
    任务在后台异步运行，不阻塞 API 响应。
    """
    if task_orchestrator.status != TaskStatus.IDLE:
        status_msg = {
            TaskStatus.RUNNING: "已有任务在运行中",
            TaskStatus.STOPPING: "任务正在停止中，请稍候",
        }
        raise HTTPException(
            status_code=409,
            detail=status_msg.get(task_orchestrator.status, "当前无法启动任务"),
        )

    mode = TaskMode(request.mode) if request.mode in ("ai", "keyword") else TaskMode.AI

    asyncio.create_task(
        task_orchestrator.start(
            position_id=request.position_id,
            mode=mode,
            match_limit=request.match_limit,
            platform_id=request.platform_id,
        )
    )

    return {"status": "started", "position_id": request.position_id, "mode": request.mode}


@router.post("/stop", response_model=dict)
async def stop_task():
    """停止当前运行的任务"""
    if task_orchestrator.status != TaskStatus.RUNNING:
        raise HTTPException(status_code=400, detail="当前没有运行中的任务")
    await task_orchestrator.stop()
    return {"status": "stopping"}


@router.get("/status", response_model=dict)
async def task_status():
    """获取当前任务状态"""
    return {
        "status": task_orchestrator.status.value,
        "match_count": task_orchestrator.match_count,
        "total_count": task_orchestrator.total_count,
    }


@router.get("/logs", response_model=List[TaskLogResponse])
async def list_task_logs(limit: int = Query(default=20, ge=1, le=100)):
    """获取任务日志列表（数据库持久化记录）"""
    async with async_session() as session:
        result = await session.execute(
            select(TaskLog).order_by(TaskLog.started_at.desc()).limit(limit)
        )
        logs = result.scalars().all()
        return [TaskLogResponse.model_validate(log) for log in logs]


@router.get("/realtime_logs")
async def realtime_logs(since: int = Query(default=0, ge=0, description="从第几条开始获取")):
    """
    获取实时任务日志（内存队列）

    前端轮询此接口获取任务运行时的实时日志输出。

    Args:
        since: 从第几条开始获取（用于增量拉取）

    Returns:
        dict: 包含日志列表和总数
    """
    logs = list(_log_queue)
    if since > 0 and since < len(logs):
        logs = logs[since:]
    return {
        "total": len(_log_queue),
        "logs": logs,
    }
