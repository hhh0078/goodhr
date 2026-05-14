"""
GoodHR 自动化工具 - 岗位管理 API

提供岗位的增删改查接口，支持关键词和排除词的列表化管理。
"""

from typing import List

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from models.database import get_session
from models.position import Position, PositionCreate, PositionResponse, PositionUpdate
from utils.logger import get_logger

logger = get_logger("position_api")
router = APIRouter()


@router.get("", response_model=List[PositionResponse])
async def list_positions(session: AsyncSession = Depends(get_session)):
    """获取所有岗位列表"""
    result = await session.execute(select(Position).order_by(Position.created_at.desc()))
    positions = result.scalars().all()
    return [PositionResponse.from_orm_with_split(p) for p in positions]


@router.get("/{position_id}", response_model=PositionResponse)
async def get_position(position_id: int, session: AsyncSession = Depends(get_session)):
    """获取指定岗位详情"""
    result = await session.execute(select(Position).where(Position.id == position_id))
    position = result.scalar_one_or_none()
    if not position:
        raise HTTPException(status_code=404, detail="岗位不存在")
    return PositionResponse.from_orm_with_split(position)


@router.post("", response_model=PositionResponse, status_code=201)
async def create_position(data: PositionCreate, session: AsyncSession = Depends(get_session)):
    """创建新岗位"""
    position = Position(
        name=data.name,
        keywords=",".join(data.keywords),
        exclude_keywords=",".join(data.exclude_keywords),
        description=data.description,
        greet_message=data.greet_message,
        is_and_mode=data.is_and_mode,
    )
    session.add(position)
    await session.commit()
    await session.refresh(position)
    logger.info(f"创建岗位: {position.name}")
    return PositionResponse.from_orm_with_split(position)


@router.put("/{position_id}", response_model=PositionResponse)
async def update_position(
    position_id: int,
    data: PositionUpdate,
    session: AsyncSession = Depends(get_session),
):
    """更新岗位信息"""
    result = await session.execute(select(Position).where(Position.id == position_id))
    position = result.scalar_one_or_none()
    if not position:
        raise HTTPException(status_code=404, detail="岗位不存在")

    update_data = data.model_dump(exclude_unset=True)
    if "keywords" in update_data and isinstance(update_data["keywords"], list):
        update_data["keywords"] = ",".join(update_data["keywords"])
    if "exclude_keywords" in update_data and isinstance(update_data["exclude_keywords"], list):
        update_data["exclude_keywords"] = ",".join(update_data["exclude_keywords"])

    for key, value in update_data.items():
        setattr(position, key, value)

    await session.commit()
    await session.refresh(position)
    logger.info(f"更新岗位: {position.name}")
    return PositionResponse.from_orm_with_split(position)


@router.delete("/{position_id}", status_code=204)
async def delete_position(position_id: int, session: AsyncSession = Depends(get_session)):
    """删除岗位"""
    result = await session.execute(select(Position).where(Position.id == position_id))
    position = result.scalar_one_or_none()
    if not position:
        raise HTTPException(status_code=404, detail="岗位不存在")

    await session.delete(position)
    await session.commit()
    logger.info(f"删除岗位: {position.name}")
