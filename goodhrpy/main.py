"""
GoodHR 自动化工具 - 项目入口

启动 FastAPI Web 服务，提供管理后台和 API 接口。
支持命令行参数控制服务配置。
"""

import argparse
import sys

import uvicorn

from core.settings import config
from utils.logger import get_logger, setup_logger

logger = get_logger("main")


def main():
    """
    应用主入口函数

    解析命令行参数，启动 uvicorn HTTP 服务器。
    默认监听 127.0.0.1:8788，可通过参数或环境变量覆盖。
    """
    parser = argparse.ArgumentParser(description="GoodHR 自动化工具")
    parser.add_argument("--host", default=config.web.host, help="监听地址")
    parser.add_argument("--port", type=int, default=config.web.port, help="监听端口")
    parser.add_argument("--reload", action="store_true", help="开发模式（自动重载）")
    args = parser.parse_args()

    setup_logger(log_dir=config.data_dir / "logs")

    logger.info(f"GoodHR 自动化工具启动: http://{args.host}:{args.port}")
    logger.info(f"API 文档: http://{args.host}:{args.port}/docs")

    uvicorn.run(
        "web.app:app",
        host=args.host,
        port=args.port,
        reload=args.reload,
    )


if __name__ == "__main__":
    main()
