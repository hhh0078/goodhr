"""
GoodHR 自动化工具 - 任务管理 API

提供任务的启动、停止、状态查询和日志查看接口。
"""

import asyncio
from typing import List

from fastapi import APIRouter, HTTPException, Query
from sqlalchemy import select

from core.task import TaskMode, TaskStatus, task_orchestrator
from models.database import async_session
from models.task_log import TaskLog, TaskLogResponse, TaskStartRequest
from utils.logger import get_logger

logger = get_logger("task_api")
router = APIRouter()


@router.post("/start", response_model=dict)
async def start_task(request: TaskStartRequest):
    """
    启动候选人筛选任务

    根据岗位 ID 和筛选模式启动后台任务，
    任务在后台异步运行，不阻塞 API 响应。
    """
    if task_orchestrator.status == TaskStatus.RUNNING:
        raise HTTPException(status_code=409, detail="已有任务在运行中")

    mode = TaskMode(request.mode) if request.mode in ("ai", "keyword") else TaskMode.AI

    asyncio.create_task(
        task_orchestrator.start(
            position_id=request.position_id,
            mode=mode,
            match_limit=request.match_limit,
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
    """获取任务日志列表"""
    async with async_session() as session:
        result = await session.execute(
            select(TaskLog).order_by(TaskLog.started_at.desc()).limit(limit)
        )
        logs = result.scalars().all()
        return [TaskLogResponse.model_validate(log) for log in logs]
