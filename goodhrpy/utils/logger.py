"""
GoodHR 自动化工具 - 日志模块

基于 loguru 封装的统一日志管理，支持控制台和文件输出。
"""

import sys
from pathlib import Path

from loguru import logger


def setup_logger(log_dir: Path | None = None, level: str = "INFO") -> None:
    """
    初始化日志配置

    Args:
        log_dir: 日志文件存储目录，为 None 则仅输出到控制台
        level: 日志级别（DEBUG/INFO/WARNING/ERROR）
    """
    logger.remove()

    logger.add(
        sys.stderr,
        level=level,
        format="<green>{time:YYYY-MM-DD HH:mm:ss}</green> | <level>{level: <8}</level> | <cyan>{name}</cyan>:<cyan>{function}</cyan>:<cyan>{line}</cyan> - <level>{message}</level>",
    )

    if log_dir:
        log_dir.mkdir(parents=True, exist_ok=True)
        logger.add(
            log_dir / "goodhr_{time:YYYY-MM-DD}.log",
            level=level,
            rotation="00:00",
            retention="30 days",
            encoding="utf-8",
            format="{time:YYYY-MM-DD HH:mm:ss} | {level: <8} | {name}:{function}:{line} - {message}",
        )


def get_logger(name: str = "goodhr"):
    """
    获取指定名称的日志器

    Args:
        name: 日志器名称

    Returns:
        绑定名称后的 logger 实例
    """
    return logger.bind(name=name)
