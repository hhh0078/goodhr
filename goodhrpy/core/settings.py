"""
GoodHR 自动化工具 - 集中配置管理

从环境变量和 YAML 配置文件加载应用设置，提供类型安全的配置访问。
优先级：环境变量 > .env 文件 > config.yaml > 默认值
"""

import os
from pathlib import Path
from typing import Optional

import yaml
from pydantic import Field
from pydantic_settings import BaseSettings

_PROJECT_ROOT = Path(__file__).resolve().parent.parent
_DATA_DIR = _PROJECT_ROOT / "data"
_LOCAL_BINARY = _DATA_DIR / "browser" / "Chromium.app" / "Contents" / "MacOS" / "Chromium"

if not os.environ.get("CLOAKBROWSER_BINARY_PATH") and _LOCAL_BINARY.exists():
    os.environ["CLOAKBROWSER_BINARY_PATH"] = str(_LOCAL_BINARY)


def _load_yaml_config() -> dict:
    """加载 config.yaml 配置文件，不存在则返回空字典"""
    config_path = _PROJECT_ROOT / "config.yaml"
    if config_path.exists():
        with open(config_path, "r", encoding="utf-8") as f:
            return yaml.safe_load(f) or {}
    return {}


def _get_yaml_value(key_path: str, default=None):
    """
    从 YAML 配置中按点号路径获取值
    如 _get_yaml_value("ai.api_key") 获取 yaml 中 ai 下的 api_key
    """
    config = _load_yaml_config()
    keys = key_path.split(".")
    current = config
    for key in keys:
        if isinstance(current, dict) and key in current:
            current = current[key]
        else:
            return default
    return current


class AIConfig(BaseSettings):
    """AI 平台配置（ai.58it.cn）"""

    api_key: str = Field(default="", description="AI 平台 API 密钥")
    model: str = Field(default="gpt-5.1-chat", description="AI 模型名称")
    base_url: str = Field(
        default="https://ai.58it.cn/v1/chat/completions",
        description="AI 对话接口地址",
    )
    click_prompt: str = Field(default="", description="粗筛提示词模板")
    contact_prompt: Optional[str] = Field(default=None, description="精筛提示词模板")
    temperature: float = Field(default=0.3, description="AI 生成温度")

    model_config = {"env_prefix": "AI_"}


class BrowserConfig(BaseSettings):
    """CloakBrowser 浏览器配置"""

    headless: bool = Field(default=False, description="是否无头模式运行")
    humanize: bool = Field(default=True, description="是否启用仿真人行为")
    human_preset: str = Field(default="default", description="仿真人行为预设（default/careful）")
    proxy: str = Field(default="", description="代理地址（HTTP/SOCKS5）")
    viewport_width: int = Field(default=1280, description="浏览器视口宽度（像素）")
    viewport_height: int = Field(default=800, description="浏览器视口高度（像素）")

    model_config = {"env_prefix": "BROWSER_"}


class TaskConfig(BaseSettings):
    """任务运行配置"""

    match_limit: int = Field(default=60, description="匹配候选人上限")
    scroll_delay_min: int = Field(default=3, description="滚动最小延迟（秒）")
    scroll_delay_max: int = Field(default=8, description="滚动最大延迟（秒）")
    list_view_delay_min: float = Field(default=1.0, description="候选人列表查看最小延迟（秒）")
    list_view_delay_max: float = Field(default=3.0, description="候选人列表查看最大延迟（秒）")
    detail_view_delay_min: float = Field(default=2.0, description="详情弹框打开后最小延迟（秒）")
    detail_view_delay_max: float = Field(default=5.0, description="详情弹框打开后最大延迟（秒）")
    greet_delay_min: float = Field(default=1.0, description="打招呼前最小延迟（秒）")
    greet_delay_max: float = Field(default=3.0, description="打招呼前最大延迟（秒）")
    rest_after_candidates_min: int = Field(default=1, description="处理多少个候选人后休息：最小值")
    rest_after_candidates_max: int = Field(default=10, description="处理多少个候选人后休息：最大值")
    rest_times_min: int = Field(default=1, description="单次任务随机休息次数：最小值")
    rest_times_max: int = Field(default=3, description="单次任务随机休息次数：最大值")
    rest_duration_min: float = Field(default=2.0, description="每次随机休息最短分钟数")
    rest_duration_max: float = Field(default=8.0, description="每次随机休息最长分钟数")
    keyword_detail_open_probability: int = Field(default=50, description="关键词模式打开详情概率(0-100)")
    click_frequency: int = Field(default=7, description="免费模式点击概率(0-10)")
    detail_mode: str = Field(default="dom", description="详情获取模式：dom=DOM选择器, ocr=截图OCR识别")

    model_config = {"env_prefix": "TASK_"}


class WebConfig(BaseSettings):
    """Web 服务配置"""

    host: str = Field(default="127.0.0.1", description="监听地址")
    port: int = Field(default=8788, description="监听端口")

    model_config = {"env_prefix": "WEB_"}


class DatabaseConfig(BaseSettings):
    """数据库配置"""

    url: str = Field(
        default=f"sqlite+aiosqlite:///{_DATA_DIR / 'goodhr.db'}",
        description="数据库连接地址",
    )

    model_config = {"env_prefix": "DATABASE_"}


class AppConfig(BaseSettings):
    """应用总配置，聚合所有子配置"""

    ai: AIConfig = AIConfig()
    browser: BrowserConfig = BrowserConfig()
    task: TaskConfig = TaskConfig()
    web: WebConfig = WebConfig()
    database: DatabaseConfig = DatabaseConfig()

    project_root: Path = _PROJECT_ROOT
    data_dir: Path = _DATA_DIR


def load_config() -> AppConfig:
    """
    加载应用配置，合并环境变量和 YAML 文件
    环境变量优先级高于 YAML 配置
    """
    yaml_data = _load_yaml_config()

    ai_defaults = yaml_data.get("ai", {})
    browser_defaults = yaml_data.get("browser", {})
    task_defaults = yaml_data.get("task", {})
    web_defaults = yaml_data.get("web", {})
    db_defaults = yaml_data.get("database", {})

    ai_config = AIConfig(
        api_key=os.getenv("AI_API_KEY", "") or ai_defaults.get("api_key", ""),
        model=os.getenv("AI_MODEL", "") or ai_defaults.get("model", "gpt-5.1-chat"),
        base_url=os.getenv("AI_BASE_URL", "") or ai_defaults.get("base_url", "https://ai.58it.cn/v1/chat/completions"),
        click_prompt=ai_defaults.get("click_prompt", ""),
        contact_prompt=ai_defaults.get("contact_prompt"),
        temperature=float(ai_defaults.get("temperature", 0.3)),
    )

    browser_config = BrowserConfig(
        headless=os.getenv("BROWSER_HEADLESS", "").lower() in ("true", "1") or browser_defaults.get("headless", False),
        humanize=(
            os.getenv("BROWSER_HUMANIZE", "true").lower() in ("true", "1")
            or browser_defaults.get("humanize", True)
        ),
        human_preset=os.getenv("BROWSER_HUMAN_PRESET", "") or browser_defaults.get("human_preset", "default"),
        proxy=os.getenv("PROXY", "") or os.getenv("PROXY_SOCKS5", "") or browser_defaults.get("proxy", ""),
        viewport_width=int(os.getenv("BROWSER_VIEWPORT_WIDTH", "") or browser_defaults.get("viewport_width", 1280)),
        viewport_height=int(os.getenv("BROWSER_VIEWPORT_HEIGHT", "") or browser_defaults.get("viewport_height", 800)),
    )

    task_config = TaskConfig(
        match_limit=int(os.getenv("TASK_MATCH_LIMIT", "") or task_defaults.get("match_limit", 60)),
        scroll_delay_min=int(os.getenv("TASK_SCROLL_DELAY_MIN", "") or task_defaults.get("scroll_delay_min", 3)),
        scroll_delay_max=int(os.getenv("TASK_SCROLL_DELAY_MAX", "") or task_defaults.get("scroll_delay_max", 8)),
        list_view_delay_min=float(
            os.getenv("TASK_LIST_VIEW_DELAY_MIN", "") or task_defaults.get("list_view_delay_min", 1.0)
        ),
        list_view_delay_max=float(
            os.getenv("TASK_LIST_VIEW_DELAY_MAX", "") or task_defaults.get("list_view_delay_max", 3.0)
        ),
        detail_view_delay_min=float(
            os.getenv("TASK_DETAIL_VIEW_DELAY_MIN", "") or task_defaults.get("detail_view_delay_min", 2.0)
        ),
        detail_view_delay_max=float(
            os.getenv("TASK_DETAIL_VIEW_DELAY_MAX", "") or task_defaults.get("detail_view_delay_max", 5.0)
        ),
        greet_delay_min=float(os.getenv("TASK_GREET_DELAY_MIN", "") or task_defaults.get("greet_delay_min", 1.0)),
        greet_delay_max=float(os.getenv("TASK_GREET_DELAY_MAX", "") or task_defaults.get("greet_delay_max", 3.0)),
        rest_after_candidates_min=int(
            os.getenv("TASK_REST_AFTER_CANDIDATES_MIN", "")
            or task_defaults.get("rest_after_candidates_min", 1)
        ),
        rest_after_candidates_max=int(
            os.getenv("TASK_REST_AFTER_CANDIDATES_MAX", "")
            or task_defaults.get("rest_after_candidates_max", 10)
        ),
        rest_times_min=int(os.getenv("TASK_REST_TIMES_MIN", "") or task_defaults.get("rest_times_min", 1)),
        rest_times_max=int(os.getenv("TASK_REST_TIMES_MAX", "") or task_defaults.get("rest_times_max", 3)),
        rest_duration_min=float(os.getenv("TASK_REST_DURATION_MIN", "") or task_defaults.get("rest_duration_min", 2.0)),
        rest_duration_max=float(os.getenv("TASK_REST_DURATION_MAX", "") or task_defaults.get("rest_duration_max", 8.0)),
        keyword_detail_open_probability=int(
            os.getenv("TASK_KEYWORD_DETAIL_OPEN_PROBABILITY", "")
            or task_defaults.get("keyword_detail_open_probability", 50)
        ),
        click_frequency=int(os.getenv("TASK_CLICK_FREQUENCY", "") or task_defaults.get("click_frequency", 7)),
        detail_mode=os.getenv("TASK_DETAIL_MODE", "") or task_defaults.get("detail_mode", "dom"),
    )

    web_config = WebConfig(
        host=os.getenv("WEB_HOST", "") or web_defaults.get("host", "127.0.0.1"),
        port=int(os.getenv("WEB_PORT", "") or web_defaults.get("port", 8788)),
    )

    db_config = DatabaseConfig(
        url=os.getenv("DATABASE_URL", "") or db_defaults.get("url", f"sqlite+aiosqlite:///{_DATA_DIR / 'goodhr.db'}"),
    )

    return AppConfig(
        ai=ai_config,
        browser=browser_config,
        task=task_config,
        web=web_config,
        database=db_config,
    )


config = load_config()
