"""
本文件负责启动 GoodHR 5 Local Agent FastAPI 服务并注册本地 API。

提供健康检查、云端账号绑定、profile 管理、候选人 JSON 和截图/OCR 文件管理。
后续浏览器控制和任务执行路由也在此注册。
"""

from __future__ import annotations

import json
import os
from collections.abc import Iterable
from pathlib import Path

from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, JSONResponse

from app.browser import BrowserManager
from app.humanize import navigate_to_page, random_delay, scroll_to_load, wait_and_click
from app.machine import load_machine
from app.ocr import is_available as ocr_available, ocr_image_async
from app.profiles import create_profile, delete_profile, list_profiles
from app.screenshot import screenshot_modal
from app.session import load_cloud_account, save_cloud_account
from app.tasks import (
    delete_candidate,
    delete_screenshot,
    init_task,
    list_screenshots,
    load_candidates,
    save_candidate,
    save_ocr_text,
    screenshot_path,
)

HOST = "127.0.0.1"
DEFAULT_PORTS = range(9001, 9010)
MACHINE = load_machine()

# ---------------------------------------------------------------------------
# FastAPI 应用与中间件
# ---------------------------------------------------------------------------

app = FastAPI(
    title="GoodHR 5 Local Agent",
    version="0.4.0",
    docs_url=None,
    redoc_url=None,
)

# 允许云端页面访问 Local Agent 的 CORS 中间件
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["GET", "POST", "PUT", "DELETE", "OPTIONS"],
    allow_headers=["Content-Type", "Authorization", "X-GoodHR-Local-Token"],
    expose_headers=["Content-Length", "Content-Type"],
)

# 全局浏览器管理器实例，用于任务执行期间管理 CloakBrowser 生命周期
_browser_manager = BrowserManager()

# ---------------------------------------------------------------------------
# 路由处理函数
# ---------------------------------------------------------------------------


@app.get("/health")
async def get_health() -> dict:
    """返回 Local Agent 健康状态，包含版本、端口、机器码和云端绑定信息。"""
    account = load_cloud_account()
    return {
        "ok": True,
        "name": "GoodHR 5 Local Agent",
        "version": "0.4.0",
        "port": app.state.port,
        "machine_id": MACHINE["machine_id"],
        "bound_cloud_user_id": account["cloud_user_id"] if account else "",
    }


@app.post("/api/v1/session/bind-cloud-user")
async def bind_cloud_user(payload: dict) -> dict:
    """绑定当前 Local Agent 对应的云端账号。

    绑定信息保存到 agent_data/cloud_account.json，
    用于后续任务操作时验证用户身份。
    """
    cloud_user_id = str(payload.get("cloud_user_id", "")).strip()
    cloud_email = str(payload.get("cloud_email", "")).strip().lower()
    agent_token = str(payload.get("agent_token", "")).strip()

    if not cloud_user_id:
        raise HTTPException(400, "cloud_user_id is required")
    if not cloud_email:
        raise HTTPException(400, "cloud_email is required")
    if not agent_token:
        raise HTTPException(400, "agent_token is required")

    account = save_cloud_account(cloud_user_id, cloud_email, agent_token)
    return {
        "ok": True,
        "machine_id": MACHINE["machine_id"],
        "cloud_user_id": account["cloud_user_id"],
        "cloud_email": account["cloud_email"],
        "bound_at": account["bound_at"],
    }


@app.get("/api/v1/profiles")
async def get_profiles(platform_id: str = "") -> dict:
    """返回本地 profile 元数据列表，可按 platform_id 过滤。

    用于云端页面读取可选平台账号，供任务创建时选择。
    """
    profiles = list_profiles(platform_id)
    return {"ok": True, "profiles": profiles}


@app.post("/api/v1/profiles")
async def post_profile(payload: dict) -> dict:
    """创建本地 profile 元数据。

    真实 cookie 仍由浏览器 profile 保存，此处只管理元数据。
    """
    profile = create_profile(
        str(payload.get("platform_id", "")),
        str(payload.get("display_name", "")),
    )
    return {"ok": True, "profile": profile}


@app.delete("/api/v1/profiles/{profile_id}")
async def delete_profile_route(profile_id: str) -> dict:
    """删除本地 profile 元数据。

    当前只删除元数据记录，浏览器 profile 文件清理后续单独实现。
    """
    deleted = delete_profile(profile_id)
    if not deleted:
        raise HTTPException(404, "profile not found")
    return {"ok": True}


@app.post("/api/v1/tasks/init")
async def init_task_route(payload: dict) -> dict:
    """初始化本地任务目录和 candidates.json。

    幂等操作，重复调用不会覆盖已有候选人数据。
    同步写入的岗位模板快照会保存在 candidates.json 中。
    """
    task = init_task(
        str(payload.get("task_id", "")),
        str(payload.get("cloud_user_id", "")),
        str(payload.get("platform_id", "")),
        str(payload.get("platform_account_id", "")),
        payload.get("position_snapshot", {}),
    )
    return {"ok": True, "task": task}


@app.get("/api/v1/tasks/{task_id}/candidates")
async def get_candidates(task_id: str) -> dict:
    """读取本地任务候选人 JSON，供云端页面渲染候选人卡片。"""
    data = load_candidates(task_id)
    return {"ok": True, "data": data}


@app.post("/api/v1/tasks/{task_id}/candidates")
async def post_candidate(task_id: str, payload: dict) -> dict:
    """新增或更新本地候选人记录。

    候选人详情只保存在本地 JSON，不进入云端数据库。
    """
    candidate = save_candidate(task_id, payload)
    return {"ok": True, "candidate": candidate}


@app.delete("/api/v1/tasks/{task_id}/candidates/{candidate_id}")
async def delete_candidate_route(task_id: str, candidate_id: str) -> dict:
    """删除本地候选人记录。"""
    deleted = delete_candidate(task_id, candidate_id)
    if not deleted:
        raise HTTPException(404, "candidate not found")
    return {"ok": True}


@app.get("/api/v1/tasks/{task_id}/screenshots")
async def get_screenshots(task_id: str) -> dict:
    """列出本地任务截图文件。"""
    screenshots = list_screenshots(task_id)
    return {"ok": True, "screenshots": screenshots}


@app.get("/api/v1/tasks/{task_id}/screenshots/{filename}")
async def get_screenshot_file(task_id: str, filename: str):
    """读取本地任务截图文件。

    返回 PNG 图片，供云端页面预览候选人详情截图。
    """
    path = screenshot_path(task_id, filename)
    if not path.exists():
        raise HTTPException(404, "screenshot not found")
    return FileResponse(path, media_type="image/png")


@app.delete("/api/v1/tasks/{task_id}/screenshots/{filename}")
async def delete_screenshot_route(task_id: str, filename: str) -> dict:
    """删除本地任务截图文件。

    确保只能删除当前任务 screenshots 目录内的文件。
    """
    deleted = delete_screenshot(task_id, filename)
    if not deleted:
        raise HTTPException(404, "screenshot not found")
    return {"ok": True}


@app.post("/api/v1/tasks/{task_id}/ocr")
async def post_ocr(task_id: str, payload: dict) -> dict:
    """保存本地任务 OCR 文本。

    OCR 原文只保存在本地任务目录，不进入云端数据库。
    文本按 candidate_id 写入 ocr/{candidate_id}.txt。
    """
    candidate_id = str(payload.get("candidate_id", ""))
    text = str(payload.get("text", ""))
    result = save_ocr_text(task_id, candidate_id, text)
    return {"ok": True, "ocr": result}


# ---------------------------------------------------------------------------
# 浏览器控制路由
# ---------------------------------------------------------------------------
# 浏览器管理 API，供云端下发指令到 Local Agent 执行浏览器操作。
# 每个操作均同步等待完成后返回结果，禁止 fire-and-forget。


@app.post("/api/v1/browser/start")
async def browser_start(payload: dict) -> dict:
    """启动 CloakBrowser 浏览器实例。

    请求体参数：
        persistent: 是否使用持久化模式（默认 false）
        user_data_dir: 用户数据目录（持久化模式必填）
        headless: 是否无头模式（默认 false）
        humanize: 是否启用仿真人行为（默认 true）
        proxy: 代理地址（可选）
    """
    if _browser_manager.is_running:
        return {"ok": True, "status": "already_running"}

    await _browser_manager.start(
        persistent=bool(payload.get("persistent", False)),
        user_data_dir=payload.get("user_data_dir"),
        headless=bool(payload.get("headless", False)),
        humanize=bool(payload.get("humanize", True)),
        proxy=str(payload.get("proxy", "")),
    )
    return {"ok": True, "status": "started"}


@app.post("/api/v1/browser/stop")
async def browser_stop() -> dict:
    """关闭浏览器实例，清理所有页面和残留进程。"""
    await _browser_manager.stop()
    return {"ok": True, "status": "stopped"}


@app.get("/api/v1/browser/status")
async def browser_status() -> dict:
    """查询浏览器运行状态。"""
    return {"ok": True, "is_running": _browser_manager.is_running}


async def _require_page():
    """获取当前默认页面，浏览器未启动时返回 400 错误。"""
    page = await _browser_manager.get_page("default")
    if page is None:
        raise HTTPException(400, "浏览器未启动，请先调用 POST /api/v1/browser/start")
    return page


@app.post("/api/v1/page/open")
async def page_open(payload: dict) -> dict:
    """打开指定 URL 页面，注册为默认页面供后续操作使用。

    请求体参数：
        url: 目标页面 URL（必填）
        timeout: 导航超时毫秒数（默认 30000）
    """
    url = str(payload.get("url", "")).strip()
    if not url:
        raise HTTPException(400, "url is required")

    page = await _browser_manager.new_page("default")
    timeout = int(payload.get("timeout", 30000))

    success = await navigate_to_page(page, url, timeout=timeout)
    if not success:
        raise HTTPException(500, "页面导航失败")

    return {"ok": True, "url": url, "title": await page.title()}


@app.post("/api/v1/page/scroll")
async def page_scroll(payload: dict) -> dict:
    """滚动当前页面，模拟人工浏览加载候选人列表。

    请求体参数：
        scroll_delay_min: 滚动最小延迟秒数（默认 3）
        scroll_delay_max: 滚动最大延迟秒数（默认 8）
        max_scrolls: 最大滚动次数（默认 20）
    """
    page = await _require_page()
    await scroll_to_load(
        page,
        scroll_delay_min=int(payload.get("scroll_delay_min", 3)),
        scroll_delay_max=int(payload.get("scroll_delay_max", 8)),
        max_scrolls=int(payload.get("max_scrolls", 20)),
    )
    return {"ok": True}


@app.post("/api/v1/page/extract")
async def page_extract(payload: dict) -> dict:
    """从当前页面按选择器提取文本内容。

    请求体参数：
        selectors: 字段→选择器映射 {"name": ".name", "age": ".age"}
        card_selector: 可选，候选人卡片 CSS 选择器，传入后批量提取每个卡片的字段
        mode: "single"（默认）或 "batch"

    single 返回:
        fields: 单组提取字段值
    batch 返回:
        candidates: 每个卡片的字段值数组
    """
    page = await _require_page()
    selectors = payload.get("selectors", {})
    if not selectors or not isinstance(selectors, dict):
        raise HTTPException(400, "selectors must be a dict of field->selector")

    mode = str(payload.get("mode", "single"))
    card_selector = str(payload.get("card_selector", "")).strip()

    if mode == "batch" and card_selector:
        return await _extract_batch(page, card_selector, selectors)

    return await _extract_single(page, selectors)


async def _extract_single(page, selectors: dict) -> dict:
    """从页面提取单个候选人的字段。"""
    fields = {}
    for field_name, selector in selectors.items():
        try:
            locator = page.locator(selector).first
            if await locator.is_visible(timeout=3000):
                fields[field_name] = await locator.inner_text()
            else:
                fields[field_name] = ""
        except Exception:
            fields[field_name] = ""
    return {"ok": True, "fields": fields}


async def _extract_batch(page, card_selector: str, selectors: dict) -> dict:
    """从页面批量提取多个候选人卡片的字段。"""
    # 使用 JS 在页面内批量提取，避免单次 DOM 往返
    js_code = """
    (selector, fields) => {
        const cards = document.querySelectorAll(selector);
        if (!cards || cards.length === 0) return [];
        const results = [];
        cards.forEach((card, index) => {
            const item = { _index: index };
            for (const [fieldName, fieldSel] of Object.entries(fields)) {
                const el = card.querySelector(fieldSel);
                item[fieldName] = el ? el.innerText.trim() : '';
            }
            results.push(item);
        });
        return results;
    }
    """
    try:
        candidates = await page.evaluate(js_code, card_selector, selectors)
    except Exception as e:
        raise HTTPException(500, f"批量提取失败: {e}")

    if not candidates or not isinstance(candidates, list):
        return {"ok": True, "candidates": []}

    return {"ok": True, "candidates": candidates, "count": len(candidates)}


@app.post("/api/v1/page/click")
async def page_click(payload: dict) -> dict:
    """点击当前页面中的元素。

    请求体参数：
        selector: CSS 选择器（必填）
        timeout: 等待超时毫秒数（默认 10000）
        delay_before: 点击前延迟秒数（默认 0.5）
    """
    page = await _require_page()
    selector = str(payload.get("selector", "")).strip()
    if not selector:
        raise HTTPException(400, "selector is required")

    timeout = int(payload.get("timeout", 10000))
    delay_before = float(payload.get("delay_before", 0.5))

    success = await wait_and_click(page, selector, timeout=timeout, delay_before=delay_before)
    return {"ok": True, "clicked": success}


@app.post("/api/v1/page/screenshot")
async def page_screenshot(payload: dict) -> dict:
    """截取当前页面弹框区域，支持滚动拼接。

    请求体参数：
        modal_selectors: 弹框 CSS 选择器列表（按优先级尝试）
    """
    page = await _require_page()
    modal_selectors = payload.get("modal_selectors", [])
    if not isinstance(modal_selectors, list) or not modal_selectors:
        raise HTTPException(400, "modal_selectors must be a non-empty list")

    screenshot_bytes = await screenshot_modal(page, modal_selectors)
    if screenshot_bytes is None:
        raise HTTPException(500, "截图失败")

    return {"ok": True, "size": len(screenshot_bytes)}


@app.get("/api/v1/ocr/status")
async def ocr_status() -> dict:
    """检查 PaddleOCR 是否可用。"""
    return {"ok": True, "available": ocr_available()}


@app.post("/api/v1/ocr/recognize")
async def ocr_recognize(payload: dict) -> dict:
    """对图片字节数据进行 OCR 识别。

    请求体参数：
        image_b64: Base64 编码的图片数据（必填）
    """
    import base64

    image_b64 = str(payload.get("image_b64", "")).strip()
    if not image_b64:
        raise HTTPException(400, "image_b64 is required")

    try:
        image_bytes = base64.b64decode(image_b64)
    except Exception:
        raise HTTPException(400, "invalid base64 data")

    text = await ocr_image_async(image_bytes)
    return {"ok": True, "text": text}


# ---------------------------------------------------------------------------
# 异常处理
# ---------------------------------------------------------------------------


@app.exception_handler(FileNotFoundError)
async def handle_not_found(_request: Request, exc: FileNotFoundError) -> JSONResponse:
    """统一的文件未找到异常处理，返回 404 响应。"""
    return JSONResponse(
        {"ok": False, "error": str(exc) or "task candidates not found"},
        status_code=404,
    )


@app.exception_handler(ValueError)
async def handle_value_error(_request: Request, exc: ValueError) -> JSONResponse:
    """统一的参数校验异常处理，返回 400 响应。"""
    return JSONResponse({"ok": False, "error": str(exc)}, status_code=400)


# ---------------------------------------------------------------------------
# 端口自动选择与启动
# ---------------------------------------------------------------------------


def create_app() -> FastAPI:
    """创建并配置 FastAPI 应用实例。"""
    return app


def candidate_ports() -> Iterable[int]:
    """返回 Local Agent 应尝试监听的端口列表。

    优先尝试 GOODHR_AGENT_PORT 环境变量指定的端口，
    然后按 9001-9009 依次尝试。
    """
    configured = os.getenv("GOODHR_AGENT_PORT")
    yielded: set[int] = set()

    if configured:
        try:
            port = int(configured)
        except ValueError as exc:
            raise RuntimeError("GOODHR_AGENT_PORT must be a number") from exc
        yielded.add(port)
        yield port

    for port in DEFAULT_PORTS:
        if port in yielded:
            continue
        yield port


def find_port() -> int:
    """自动寻找可用端口并返回。

    按 candidate_ports() 顺序逐个尝试绑定 socket，
    找到可用端口后立即释放并返回端口号。
    """
    import socket

    errors: list[str] = []
    for port in candidate_ports():
        try:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
                sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                sock.bind((HOST, port))
                return port
        except OSError as exc:
            errors.append(f"{port}: {exc}")

    detail = "; ".join(errors)
    raise RuntimeError(f"No available GoodHR Local Agent port in 9001-9009. {detail}")


def main() -> None:
    """启动 Local Agent FastAPI 服务。

    自动选择可用端口，启动后在 print 中给出实际监听地址。
    """
    import uvicorn

    port = find_port()
    app.state.port = port  # 保存到应用状态，供 /health 返回

    print(f"GoodHR 5 Local Agent starting on http://{HOST}:{port}")
    uvicorn.run(app, host=HOST, port=port, log_level="warning", access_log=False)


if __name__ == "__main__":
    main()
