"""
GoodHR 自动化工具 - 数据库初始化与连接管理

基于 SQLAlchemy 异步引擎，管理 SQLite 数据库连接和会话。
"""

from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine
from sqlalchemy.orm import DeclarativeBase

from core.settings import config


engine = create_async_engine(config.database.url, echo=False)
async_session = async_sessionmaker(engine, class_=AsyncSession, expire_on_commit=False)


class Base(DeclarativeBase):
    """SQLAlchemy 声明式基类"""
    pass


async def init_db() -> None:
    """
    初始化数据库，创建所有表结构

    在应用启动时调用，确保 data 目录和数据库表已创建。
    """
    config.data_dir.mkdir(parents=True, exist_ok=True)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)


async def get_session() -> AsyncSession:
    """
    获取异步数据库会话

    用于 FastAPI 依赖注入，自动管理会话生命周期。

    Yields:
        AsyncSession: 数据库异步会话
    """
    async with async_session() as session:
        yield session
