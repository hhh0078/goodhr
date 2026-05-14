"""
GoodHR 自动化工具 - 任务日志数据模型

定义任务执行日志的数据库表结构和 Pydantic 响应模型。
"""

from datetime import datetime
from typing import Optional

from pydantic import BaseModel, Field
from sqlalchemy import DateTime, Integer, String, Text, func
from sqlalchemy.orm import Mapped, mapped_column

from models.database import Base


class TaskLog(Base):
    """任务执行日志表"""

    __tablename__ = "task_logs"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True, comment="日志ID")
    position_id: Mapped[int] = mapped_column(Integer, nullable=False, index=True, comment="关联岗位ID")
    position_name: Mapped[str] = mapped_column(String(100), default="", comment="岗位名称（冗余，方便查询）")
    status: Mapped[str] = mapped_column(String(20), default="running", comment="任务状态：running/completed/failed/stopped")
    total_count: Mapped[int] = mapped_column(Integer, default=0, comment="扫描候选人总数")
    greeted_count: Mapped[int] = mapped_column(Integer, default=0, comment="打招呼成功数")
    skipped_count: Mapped[int] = mapped_column(Integer, default=0, comment="跳过数")
    error_message: Mapped[str] = mapped_column(Text, default="", comment="错误信息")
    started_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now(), comment="开始时间")
    finished_at: Mapped[Optional[datetime]] = mapped_column(DateTime, nullable=True, comment="结束时间")


class TaskLogResponse(BaseModel):
    """任务日志响应模型"""

    id: int
    position_id: int
    position_name: str
    status: str
    total_count: int
    greeted_count: int
    skipped_count: int
    error_message: str
    started_at: datetime
    finished_at: Optional[datetime]

    model_config = {"from_attributes": True}


class TaskStartRequest(BaseModel):
    """启动任务请求模型"""

    position_id: int = Field(..., description="岗位ID")
    mode: str = Field(default="ai", description="筛选模式：ai/keyword")
    match_limit: Optional[int] = Field(default=None, description="匹配上限，None 使用全局配置")
