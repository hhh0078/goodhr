"""
GoodHR 自动化工具 - FastAPI 应用入口

创建和配置 FastAPI 应用实例，注册路由和中间件，
提供 Web 管理后台的 HTTP 服务。
"""

from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles

from core.settings import config
from models.database import init_db
from utils.logger import get_logger, setup_logger
from web.api import candidate, config_api, login_api, position, screenshot_api, task

logger = get_logger("app")


@asynccontextmanager
async def lifespan(application: FastAPI):
    """
    应用生命周期管理

    启动时初始化数据库和日志，关闭时清理资源。
    """
    setup_logger(log_dir=config.data_dir / "logs")
    logger.info("GoodHR 自动化工具启动中...")
    await init_db()
    logger.info("数据库初始化完成")
    yield
    logger.info("GoodHR 自动化工具已关闭")


def create_app() -> FastAPI:
    """
    创建 FastAPI 应用实例

    配置 CORS、注册路由、挂载静态文件。

    Returns:
        FastAPI: 配置好的应用实例
    """
    app = FastAPI(
        title="GoodHR 自动化工具",
        description="基于 CloakBrowser 的候选人自动筛选和沟通工具",
        version="0.1.0",
        lifespan=lifespan,
    )

    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    app.include_router(position.router, prefix="/api/v1/positions", tags=["岗位管理"])
    app.include_router(candidate.router, prefix="/api/v1/candidates", tags=["候选人管理"])
    app.include_router(task.router, prefix="/api/v1/tasks", tags=["任务管理"])
    app.include_router(config_api.router, prefix="/api/v1/config", tags=["系统配置"])
    app.include_router(login_api.router, prefix="/api/v1/login", tags=["平台登录"])
    app.include_router(screenshot_api.router, prefix="/api/v1/screenshots", tags=["截图管理"])

    static_dir = config.project_root / "web" / "static"
    if static_dir.exists():
        app.mount("/static", StaticFiles(directory=str(static_dir)), name="static")

    @app.get("/")
    async def root():
        return {"name": "GoodHR 自动化工具", "version": "0.1.0", "status": "running"}

    @app.get("/health")
    async def health():
        return {"status": "ok"}

    return app


app = create_app()
