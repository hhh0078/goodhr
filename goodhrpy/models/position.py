"""
GoodHR 自动化工具 - 岗位数据模型

定义招聘岗位的数据库表结构和 Pydantic 请求/响应模型。
"""

from datetime import datetime
from typing import List, Optional

from pydantic import BaseModel, Field
from sqlalchemy import Boolean, DateTime, Integer, String, Text, func
from sqlalchemy.orm import Mapped, mapped_column

from models.database import Base


class Position(Base):
    """招聘岗位表"""

    __tablename__ = "positions"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True, comment="岗位ID")
    name: Mapped[str] = mapped_column(String(100), nullable=False, comment="岗位名称")
    keywords: Mapped[str] = mapped_column(Text, default="", comment="筛选关键词，逗号分隔")
    exclude_keywords: Mapped[str] = mapped_column(Text, default="", comment="排除关键词，逗号分隔")
    description: Mapped[str] = mapped_column(Text, default="", comment="岗位说明/要求")
    greet_message: Mapped[str] = mapped_column(Text, default="", comment="打招呼消息模板")
    is_and_mode: Mapped[bool] = mapped_column(Boolean, default=False, comment="关键词匹配模式：True与模式/False或模式")
    is_active: Mapped[bool] = mapped_column(Boolean, default=True, comment="是否启用")
    created_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now(), comment="创建时间")
    updated_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now(), onupdate=func.now(), comment="更新时间")


class PositionCreate(BaseModel):
    """创建岗位请求模型"""

    name: str = Field(..., min_length=1, max_length=100, description="岗位名称")
    keywords: List[str] = Field(default_factory=list, description="筛选关键词列表")
    exclude_keywords: List[str] = Field(default_factory=list, description="排除关键词列表")
    description: str = Field(default="", description="岗位说明")
    greet_message: str = Field(default="", description="打招呼消息")
    is_and_mode: bool = Field(default=False, description="关键词与模式")


class PositionUpdate(BaseModel):
    """更新岗位请求模型"""

    name: Optional[str] = Field(default=None, description="岗位名称")
    keywords: Optional[List[str]] = Field(default=None, description="筛选关键词列表")
    exclude_keywords: Optional[List[str]] = Field(default=None, description="排除关键词列表")
    description: Optional[str] = Field(default=None, description="岗位说明")
    greet_message: Optional[str] = Field(default=None, description="打招呼消息")
    is_and_mode: Optional[bool] = Field(default=None, description="关键词与模式")
    is_active: Optional[bool] = Field(default=None, description="是否启用")


class PositionResponse(BaseModel):
    """岗位响应模型"""

    id: int
    name: str
    keywords: List[str]
    exclude_keywords: List[str]
    description: str
    greet_message: str
    is_and_mode: bool
    is_active: bool
    created_at: datetime
    updated_at: datetime

    model_config = {"from_attributes": True}

    @classmethod
    def from_orm_with_split(cls, obj: Position) -> "PositionResponse":
        """从 ORM 对象构建响应，自动拆分逗号分隔的关键词字段"""
        return cls(
            id=obj.id,
            name=obj.name,
            keywords=[k.strip() for k in obj.keywords.split(",") if k.strip()],
            exclude_keywords=[k.strip() for k in obj.exclude_keywords.split(",") if k.strip()],
            description=obj.description,
            greet_message=obj.greet_message,
            is_and_mode=obj.is_and_mode,
            is_active=obj.is_active,
            created_at=obj.created_at,
            updated_at=obj.updated_at,
        )
