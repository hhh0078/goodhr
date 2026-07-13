"""
GoodHR 自动化工具 - 截图管理 API

提供候选人详情截图的列表查看、单个查看和一键清理接口。
截图保存在 data/screenshots/ 目录下。
"""

import os
from pathlib import Path
from typing import List

from fastapi import APIRouter, HTTPException
from fastapi.responses import FileResponse
from pydantic import BaseModel

from core.settings import config
from utils.logger import get_logger

logger = get_logger("screenshot_api")
router = APIRouter()


class ScreenshotInfo(BaseModel):
    """截图文件信息模型"""

    filename: str
    filepath: str
    size_kb: float
    created_at: str


class ScreenshotListResponse(BaseModel):
    """截图列表响应模型"""

    total: int
    screenshots: List[ScreenshotInfo]


def _screenshot_dir() -> Path:
    """获取截图目录路径"""
    return config.data_dir / "screenshots"


@router.get("", response_model=ScreenshotListResponse)
async def list_screenshots():
    """
    获取所有截图文件列表

    按修改时间倒序排列，返回文件名、路径、大小和创建时间。
    """
    screenshot_dir = _screenshot_dir()
    if not screenshot_dir.exists():
        return ScreenshotListResponse(total=0, screenshots=[])

    screenshots = []
    for filepath in sorted(screenshot_dir.glob("*.png"), key=lambda f: f.stat().st_mtime, reverse=True):
        try:
            stat = filepath.stat()
            from datetime import datetime
            created_at = datetime.fromtimestamp(stat.st_mtime).strftime("%Y-%m-%d %H:%M:%S")
            screenshots.append(ScreenshotInfo(
                filename=filepath.name,
                filepath=str(filepath),
                size_kb=round(stat.st_size / 1024, 1),
                created_at=created_at,
            ))
        except Exception:
            continue

    return ScreenshotListResponse(total=len(screenshots), screenshots=screenshots)


@router.get("/{filename}")
async def get_screenshot(filename: str):
    """
    获取单个截图文件

    返回 PNG 图片文件，浏览器可直接显示。

    Args:
        filename: 截图文件名
    """
    screenshot_dir = _screenshot_dir()
    filepath = screenshot_dir / filename

    if not filepath.exists():
        raise HTTPException(status_code=404, detail="截图文件不存在")

    if not str(filepath.resolve()).startswith(str(screenshot_dir.resolve())):
        raise HTTPException(status_code=403, detail="非法路径")

    return FileResponse(
        path=str(filepath),
        media_type="image/png",
        filename=filename,
    )


@router.delete("/clear", response_model=dict)
async def clear_screenshots():
    """
    一键清理所有截图文件

    删除 data/screenshots/ 目录下的所有 PNG 文件。
    """
    screenshot_dir = _screenshot_dir()
    if not screenshot_dir.exists():
        return {"ok": True, "deleted": 0}

    deleted = 0
    for filepath in screenshot_dir.glob("*.png"):
        try:
            filepath.unlink()
            deleted += 1
        except Exception as e:
            logger.warning(f"删除截图失败 {filepath}: {e}")

    logger.info(f"已清理 {deleted} 个截图文件")
    return {"ok": True, "deleted": deleted}
