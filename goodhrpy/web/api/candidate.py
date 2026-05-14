"""
GoodHR 自动化工具 - 候选人管理 API

提供候选人数据的查询、统计和导出接口。
"""

from typing import List, Optional

from fastapi import APIRouter, Depends, HTTPException, Query
from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession

from models.candidate import Candidate, CandidateResponse, CandidateStatus
from models.database import get_session
from utils.logger import get_logger

logger = get_logger("candidate_api")
router = APIRouter()


@router.get("", response_model=dict)
async def list_candidates(
    position_id: Optional[int] = Query(default=None, description="按岗位筛选"),
    status: Optional[str] = Query(default=None, description="按状态筛选"),
    keyword: Optional[str] = Query(default=None, description="按姓名/技能搜索"),
    page: int = Query(default=1, ge=1, description="页码"),
    page_size: int = Query(default=20, ge=1, le=100, description="每页数量"),
    session: AsyncSession = Depends(get_session),
):
    """获取候选人列表（分页）"""
    query = select(Candidate)

    if position_id:
        query = query.where(Candidate.position_id == position_id)
    if status:
        query = query.where(Candidate.status == status)
    if keyword:
        query = query.where(
            (Candidate.name.contains(keyword)) | (Candidate.skills.contains(keyword))
        )

    count_query = select(func.count()).select_from(query.subquery())
    total_result = await session.execute(count_query)
    total = total_result.scalar() or 0

    query = query.order_by(Candidate.created_at.desc())
    query = query.offset((page - 1) * page_size).limit(page_size)

    result = await session.execute(query)
    candidates = result.scalars().all()

    return {
        "total": total,
        "items": [CandidateResponse.model_validate(c) for c in candidates],
        "page": page,
        "page_size": page_size,
    }


@router.get("/stats", response_model=dict)
async def candidate_stats(
    position_id: Optional[int] = Query(default=None, description="按岗位筛选"),
    session: AsyncSession = Depends(get_session),
):
    """获取候选人统计信息"""
    query = select(Candidate)
    if position_id:
        query = query.where(Candidate.position_id == position_id)

    result = await session.execute(query)
    candidates = result.scalars().all()

    stats = {
        "total": len(candidates),
        "greeted": sum(1 for c in candidates if c.status == CandidateStatus.GREETED),
        "skipped": sum(1 for c in candidates if c.status == CandidateStatus.SKIPPED),
        "pending": sum(1 for c in candidates if c.status == CandidateStatus.PENDING),
        "failed": sum(1 for c in candidates if c.status == CandidateStatus.FAILED),
    }
    return stats


@router.get("/{candidate_id}", response_model=CandidateResponse)
async def get_candidate(candidate_id: int, session: AsyncSession = Depends(get_session)):
    """获取候选人详情"""
    result = await session.execute(select(Candidate).where(Candidate.id == candidate_id))
    candidate = result.scalar_one_or_none()
    if not candidate:
        raise HTTPException(status_code=404, detail="候选人不存在")
    return CandidateResponse.model_validate(candidate)


@router.delete("/{candidate_id}", status_code=204)
async def delete_candidate(candidate_id: int, session: AsyncSession = Depends(get_session)):
    """删除候选人记录"""
    result = await session.execute(select(Candidate).where(Candidate.id == candidate_id))
    candidate = result.scalar_one_or_none()
    if not candidate:
        raise HTTPException(status_code=404, detail="候选人不存在")
    await session.delete(candidate)
    await session.commit()
