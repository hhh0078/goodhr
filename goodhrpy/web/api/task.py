"""
GoodHR 自动化工具 - 任务管理 API

支持创建多个独立任务实例。每个任务绑定平台账号/profile，拥有独立状态、
统计和实时日志。旧版 /start /stop /status /realtime_logs 接口保留兼容。
"""

import asyncio
import time
import uuid
from collections import deque
from dataclasses import dataclass, field
from typing import List, Optional

from fastapi import APIRouter, HTTPException, Query
from pydantic import BaseModel, Field
from sqlalchemy import select

from core.task import TaskMode, TaskOrchestrator, TaskStatus
from models.database import async_session
from models.task_log import TaskLog, TaskLogResponse, TaskStartRequest
from utils.logger import get_logger
from web.api.login_api import _account_profile_dir, _get_account

logger = get_logger("task_api")

router = APIRouter()


class TaskCreateRequest(BaseModel):
    """创建任务请求"""

    platform_id: str = Field(default="boss", description="平台标识")
    account_id: str = Field(..., description="平台账号ID")
    position_id: int = Field(..., description="岗位ID")
    mode: str = Field(default="ai", description="筛选模式：ai/keyword")
    match_limit: Optional[int] = Field(default=None, description="匹配上限")


@dataclass
class RuntimeTask:
    """内存中的任务实例"""

    id: str
    platform_id: str
    account_id: str
    account_name: str
    position_id: int
    mode: str
    match_limit: Optional[int]
    orchestrator: TaskOrchestrator = field(default_factory=TaskOrchestrator)
    log_queue: deque = field(default_factory=lambda: deque(maxlen=300))
    log_seq: int = 0
    created_at: float = field(default_factory=time.time)
    started_at: Optional[float] = None

    def on_log(self, message: str, level: str = "info") -> None:
        self.log_seq += 1
        self.log_queue.append({
            "seq": self.log_seq,
            "time": time.strftime("%H:%M:%S"),
            "message": message,
            "level": level,
        })

    def as_dict(self) -> dict:
        return {
            "id": self.id,
            "platform_id": self.platform_id,
            "account_id": self.account_id,
            "account_name": self.account_name,
            "position_id": self.position_id,
            "mode": self.mode,
            "match_limit": self.match_limit,
            "status": self.orchestrator.status.value,
            "total_count": self.orchestrator.total_count,
            "match_count": self.orchestrator.match_count,
            "skipped_count": self.orchestrator.skipped_count,
            "failed_count": self.orchestrator.failed_count,
            "created_at": self.created_at,
            "started_at": self.started_at,
        }


class TaskManager:
    """管理多个运行时任务"""

    def __init__(self):
        self.tasks: dict[str, RuntimeTask] = {}
        self.latest_task_id: Optional[str] = None

    def create(self, request: TaskCreateRequest) -> RuntimeTask:
        account = _get_account(request.account_id)
        if not account:
            raise HTTPException(status_code=404, detail="账号不存在，请先在平台登录中创建/登录账号")
        if account.get("platform") != request.platform_id:
            raise HTTPException(status_code=400, detail="账号与平台不匹配")

        task_id = uuid.uuid4().hex[:10]
        task = RuntimeTask(
            id=task_id,
            platform_id=request.platform_id,
            account_id=request.account_id,
            account_name=account.get("name", request.account_id),
            position_id=request.position_id,
            mode=request.mode,
            match_limit=request.match_limit,
        )
        task.orchestrator.on_log(task.on_log)
        self.tasks[task_id] = task
        self.latest_task_id = task_id
        task.on_log(
            f"任务已创建：平台={request.platform_id}, 账号={task.account_name}, "
            f"岗位ID={request.position_id}, 模式={request.mode}, 上限={request.match_limit or '默认'}"
        )
        return task

    def get(self, task_id: str) -> RuntimeTask:
        task = self.tasks.get(task_id)
        if not task:
            raise HTTPException(status_code=404, detail="任务不存在")
        return task

    def list(self) -> list[dict]:
        return [task.as_dict() for task in sorted(self.tasks.values(), key=lambda item: item.created_at, reverse=True)]

    async def start(self, task_id: str) -> RuntimeTask:
        task = self.get(task_id)
        if task.orchestrator.status != TaskStatus.IDLE:
            raise HTTPException(status_code=409, detail="任务已启动或正在停止")

        for other in self.tasks.values():
            if other.id == task.id:
                continue
            same_account = other.platform_id == task.platform_id and other.account_id == task.account_id
            if same_account and other.orchestrator.status == TaskStatus.RUNNING:
                raise HTTPException(status_code=409, detail="同一账号已有运行中任务")

        mode = TaskMode(task.mode) if task.mode in ("ai", "keyword") else TaskMode.AI
        account = _get_account(task.account_id)
        if not account:
            raise HTTPException(status_code=404, detail="账号不存在")
        profile_dir = str(_account_profile_dir(account))
        task.started_at = time.time()
        task.on_log("任务启动中...")
        asyncio.create_task(
            task.orchestrator.start(
                position_id=task.position_id,
                mode=mode,
                match_limit=task.match_limit,
                platform_id=task.platform_id,
                account_id=task.account_id,
                profile_dir=profile_dir,
            )
        )
        return task

    async def stop(self, task_id: str) -> RuntimeTask:
        task = self.get(task_id)
        if task.orchestrator.status != TaskStatus.RUNNING:
            raise HTTPException(status_code=400, detail="该任务没有运行")
        await task.orchestrator.stop()
        return task

    def latest(self) -> Optional[RuntimeTask]:
        if self.latest_task_id:
            return self.tasks.get(self.latest_task_id)
        return None


task_manager = TaskManager()


@router.post("/create", response_model=dict)
async def create_task(request: TaskCreateRequest):
    task = task_manager.create(request)
    return task.as_dict()


@router.post("/{task_id}/start", response_model=dict)
async def start_task_instance(task_id: str):
    task = await task_manager.start(task_id)
    return task.as_dict()


@router.post("/{task_id}/stop", response_model=dict)
async def stop_task_instance(task_id: str):
    task = await task_manager.stop(task_id)
    return task.as_dict()


@router.get("/instances", response_model=list[dict])
async def list_task_instances():
    return task_manager.list()


@router.get("/{task_id}/status", response_model=dict)
async def task_instance_status(task_id: str):
    return task_manager.get(task_id).as_dict()


@router.get("/{task_id}/realtime_logs")
async def task_instance_realtime_logs(
    task_id: str,
    since_id: int = Query(default=0, ge=0, description="从哪个序列号之后开始获取"),
):
    task = task_manager.get(task_id)
    logs = list(task.log_queue)
    if since_id > 0:
        logs = [record for record in logs if record["seq"] > since_id]
    return {
        "last_seq": task.log_seq,
        "logs": logs,
    }


@router.post("/start", response_model=dict)
async def start_task(request: TaskStartRequest):
    """旧版兼容：创建并启动一个任务。"""
    account_id = getattr(request, "account_id", None)
    if not account_id:
        raise HTTPException(status_code=400, detail="请选择账号后再启动任务")

    task = task_manager.create(TaskCreateRequest(
        platform_id=request.platform_id,
        account_id=account_id,
        position_id=request.position_id,
        mode=request.mode,
        match_limit=request.match_limit,
    ))
    await task_manager.start(task.id)
    return {"status": "started", **task.as_dict()}


@router.post("/stop", response_model=dict)
async def stop_task():
    """旧版兼容：停止最近创建的运行中任务。"""
    task = task_manager.latest()
    if not task:
        raise HTTPException(status_code=400, detail="当前没有任务")
    await task_manager.stop(task.id)
    return {"status": "stopping", **task.as_dict()}


@router.get("/status", response_model=dict)
async def task_status():
    """旧版兼容：获取最近创建任务状态。"""
    task = task_manager.latest()
    if not task:
        return {"status": "idle", "match_count": 0, "total_count": 0}
    return {
        "status": task.orchestrator.status.value,
        "match_count": task.orchestrator.match_count,
        "total_count": task.orchestrator.total_count,
        "skipped_count": task.orchestrator.skipped_count,
        "failed_count": task.orchestrator.failed_count,
        "task_id": task.id,
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
async def realtime_logs(since_id: int = Query(default=0, ge=0, description="从哪个序列号之后开始获取")):
    """旧版兼容：获取最近创建任务的实时日志。"""
    task = task_manager.latest()
    if not task:
        return {"last_seq": 0, "logs": []}
    logs = list(task.log_queue)
    if since_id > 0:
        logs = [record for record in logs if record["seq"] > since_id]
    return {
        "last_seq": task.log_seq,
        "logs": logs,
    }
