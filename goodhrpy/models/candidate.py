"""
GoodHR 自动化工具 - 候选人数据模型

定义候选人信息的数据库表结构和 Pydantic 请求/响应模型。
"""

from datetime import datetime
from enum import Enum
from typing import Optional

from pydantic import BaseModel, Field
from sqlalchemy import DateTime, Integer, String, Text, func
from sqlalchemy.orm import Mapped, mapped_column

from models.database import Base


class CandidateStatus(str, Enum):
    """候选人状态枚举"""

    PENDING = "pending"
    GREETED = "greeted"
    SKIPPED = "skipped"
    FAILED = "failed"


class Candidate(Base):
    """候选人信息表"""

    __tablename__ = "candidates"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True, comment="候选人ID")
    position_id: Mapped[int] = mapped_column(Integer, nullable=False, index=True, comment="关联岗位ID")
    name: Mapped[str] = mapped_column(String(50), default="", comment="候选人姓名")
    age: Mapped[str] = mapped_column(String(20), default="", comment="年龄")
    education: Mapped[str] = mapped_column(String(50), default="", comment="学历")
    experience: Mapped[str] = mapped_column(String(100), default="", comment="工作经验")
    skills: Mapped[str] = mapped_column(Text, default="", comment="技能标签")
    salary: Mapped[str] = mapped_column(String(50), default="", comment="期望薪资")
    raw_data: Mapped[str] = mapped_column(Text, default="", comment="原始提取数据（完整信息文本）")
    filter_reason: Mapped[str] = mapped_column(String(200), default="", comment="筛选结果原因")
    status: Mapped[str] = mapped_column(String(20), default=CandidateStatus.PENDING, comment="处理状态：pending/greeted/skipped/failed")
    platform: Mapped[str] = mapped_column(String(50), default="boss", comment="来源平台")
    platform_user_id: Mapped[str] = mapped_column(String(100), default="", comment="平台用户ID")
    created_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now(), comment="创建时间")


class CandidateCreate(BaseModel):
    """创建候选人请求模型"""

    position_id: int = Field(..., description="关联岗位ID")
    name: str = Field(default="", description="姓名")
    age: str = Field(default="", description="年龄")
    education: str = Field(default="", description="学历")
    experience: str = Field(default="", description="工作经验")
    skills: str = Field(default="", description="技能标签")
    salary: str = Field(default="", description="期望薪资")
    raw_data: str = Field(default="", description="原始数据")
    platform: str = Field(default="boss", description="来源平台")
    platform_user_id: str = Field(default="", description="平台用户ID")


class CandidateResponse(BaseModel):
    """候选人响应模型"""

    id: int
    position_id: int
    name: str
    age: str
    education: str
    experience: str
    skills: str
    salary: str
    raw_data: str
    filter_reason: str
    status: str
    platform: str
    platform_user_id: str
    created_at: datetime

    model_config = {"from_attributes": True}


class CandidateListResponse(BaseModel):
    """候选人列表响应模型（含分页）"""

    total: int = Field(description="总数")
    items: list[CandidateResponse] = Field(description="候选人列表")
    page: int = Field(description="当前页码")
    page_size: int = Field(description="每页数量")


class CandidateFilterQuery(BaseModel):
    """候选人筛选查询参数"""

    position_id: Optional[int] = Field(default=None, description="按岗位筛选")
    status: Optional[str] = Field(default=None, description="按状态筛选")
    keyword: Optional[str] = Field(default=None, description="按姓名/技能搜索")
    page: int = Field(default=1, ge=1, description="页码")
    page_size: int = Field(default=20, ge=1, le=100, description="每页数量")
