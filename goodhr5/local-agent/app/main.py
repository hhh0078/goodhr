"""
本文件负责启动 GoodHR 5 Local Agent FastAPI 服务并注册本地 API。

提供健康检查、云端账号绑定、profile 管理、候选人 JSON 和截图/OCR 文件管理。
后续浏览器控制和任务执行路由也在此注册。
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import time
from collections.abc import Iterable

import httpx
from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, JSONResponse

from app.browser import BrowserManager
from app.cookie_crypto import decrypt_aes_gcm, decrypt_cookie_payload, decrypt_wrapped_key
from app.element_refs import ELEMENT_REFS
from app.humanize import (
    click_box_random_point,
    find_all_locators_by_spec,
    human_type_focused,
    is_locator_in_viewport,
    locate_element_by_spec,
    move_mouse_to_locator,
    navigate_to_page,
    parse_element_locator_spec,
    random_delay,
    scroll_locator_into_view,
    scroll_to_load,
)
from app.crypto_keys import load_or_generate as load_crypto_keys
from app.machine import cookie_machine_ids, load_machine
from app.ocr import is_available as ocr_available, ocr_image_async, warmup_ocr_async
from app.profiles import create_profile, delete_profile, list_profiles
from app.screenshot import screenshot_locator_full, screenshot_modal
from app.sound import ensure_audio_from_url, play_once, resolve_builtin_audio
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
from app.ws_client import WSAgentClient, _profile_dir

HOST = "127.0.0.1"
DEFAULT_PORTS = range(9001, 9010)
LOCAL_AGENT_VERSION = "5.0.0"
MACHINE = load_machine()
CRYPTO_KEYS = load_crypto_keys()
logger = logging.getLogger("goodhr5.local-agent")
FIELD_FAST_VISIBLE_TIMEOUT_MS = 120
FIELD_FAST_TEXT_TIMEOUT_MS = 300


def _parse_int(raw: object, default: int) -> int:
    """
    将请求参数转换为整数。

    Args:
        raw: 原始参数值
        default: 参数为空或格式错误时使用的默认值

    Returns:
        转换后的整数。
    """
    try:
        return int(raw)
    except (TypeError, ValueError):
        return default


# ---------------------------------------------------------------------------
# FastAPI 应用与中间件
# ---------------------------------------------------------------------------

app = FastAPI(
    title="GoodHR 5 Local Agent",
    version=LOCAL_AGENT_VERSION,
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
_ws_agent = WSAgentClient(_browser_manager)

# ---------------------------------------------------------------------------
# 路由处理函数
# ---------------------------------------------------------------------------


@app.on_event("startup")
async def warmup_ocr_on_startup() -> None:
    """程序启动时预热 OCR 引擎，减少任务执行时首次识别等待。"""
    if not ocr_available():
        logger.warning("OCR 依赖不可用，跳过启动预热")
        return
    logger.info("开始 OCR 启动预热")
    ok = await warmup_ocr_async()
    if not ok:
        logger.warning("OCR 启动预热未成功，后续将按需再次初始化")


@app.get("/health")
async def get_health() -> dict:
    """返回 Local Agent 健康状态，包含版本、端口、机器码和云端绑定信息。"""
    account = load_cloud_account()
    return {
        "ok": True,
        "name": "GoodHR 5 Local Agent",
        "version": LOCAL_AGENT_VERSION,
        "port": app.state.port,
        "machine_id": MACHINE["machine_id"],
        "public_key": CRYPTO_KEYS.get("public_key", ""),
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


@app.post("/api/v1/ws/connect")
async def ws_connect(payload: dict) -> dict:
    """连接云端 WebSocket。

    Args:
        payload: 包含 cloud_ws_url 和 token 的请求体。

    Returns:
        返回当前 WebSocket 连接状态。
    """
    cloud_ws_url = str(payload.get("cloud_ws_url", "")).strip()
    token = str(payload.get("token", "")).strip()
    if not cloud_ws_url:
        raise HTTPException(400, "cloud_ws_url is required")
    if not token:
        raise HTTPException(400, "token is required")
    return await _ws_agent.connect(cloud_ws_url, token)


@app.get("/api/v1/ws/status")
async def ws_status() -> dict:
    """返回 Local Agent 到云端 WebSocket 的连接状态。"""
    return _ws_agent.status()


@app.post("/api/v1/ws/disconnect")
async def ws_disconnect() -> dict:
    """断开 Local Agent 到云端 WebSocket 的连接。"""
    return await _ws_agent.disconnect()


@app.post("/api/v1/tasks/{task_id}/start-ws")
async def start_task_ws(task_id: str, payload: dict) -> dict:
    """通过任务级 WebSocket 启动云端任务。

    Args:
        task_id: 云端任务 ID。
        payload: 包含 cloud_api_base、cloud_ws_url 和 token 的请求体。

    Returns:
        返回任务启动提示和 WebSocket 状态。
    """
    cloud_api_base = str(payload.get("cloud_api_base", "")).strip()
    cloud_ws_url = str(payload.get("cloud_ws_url", "")).strip()
    token = str(payload.get("token", "")).strip()
    if not cloud_api_base:
        raise HTTPException(400, "cloud_api_base is required")
    if not cloud_ws_url:
        raise HTTPException(400, "cloud_ws_url is required")
    if not token:
        raise HTTPException(400, "token is required")
    logger.info("[任务开始] 本地收到开始请求 task=%s api=%s ws=%s", task_id, cloud_api_base, cloud_ws_url)
    try:
        return await _ws_agent.start_task(task_id, cloud_api_base, cloud_ws_url, token)
    except RuntimeError as exc:
        logger.error("[任务开始] 本地开始请求失败 task=%s err=%s", task_id, exc)
        raise HTTPException(502, str(exc))


@app.post("/api/v1/tasks/{task_id}/stop-ws")
async def stop_task_ws(task_id: str, payload: dict) -> dict:
    """停止云端任务并按需断开任务级 WebSocket。

    Args:
        task_id: 云端任务 ID。
        payload: 包含 cloud_api_base 和 token 的请求体。

    Returns:
        返回任务停止提示和 WebSocket 状态。
    """
    cloud_api_base = str(payload.get("cloud_api_base", "")).strip()
    token = str(payload.get("token", "")).strip()
    if not cloud_api_base:
        raise HTTPException(400, "cloud_api_base is required")
    if not token:
        raise HTTPException(400, "token is required")
    logger.info("[任务停止] 本地收到停止请求 task=%s api=%s", task_id, cloud_api_base)
    try:
        return await _ws_agent.stop_task(task_id, cloud_api_base, token)
    except RuntimeError as exc:
        logger.error("[任务停止] 本地停止请求失败 task=%s err=%s", task_id, exc)
        raise HTTPException(502, str(exc))


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
    user_data_dir = str(payload.get("user_data_dir") or "").strip()
    if user_data_dir:
        user_data_dir = str(_profile_dir(user_data_dir))
    persistent = bool(payload.get("persistent", False))
    cookies = payload.get("cookies")

    # 浏览器已运行时，如果目标 profile 不同则先重启，确保切到对应 cookie 目录。
    if _browser_manager.is_running:
        current_dir = str(_browser_manager._last_user_data_dir or "")
        if persistent and user_data_dir and current_dir and current_dir != user_data_dir:
            await _browser_manager.stop()
            ELEMENT_REFS.clear()
        else:
            if isinstance(cookies, list) and cookies:
                try:
                    await _browser_manager.add_cookies(cookies)
                except Exception as exc:
                    raise HTTPException(400, f"cookie 注入失败: {exc}")
            return {"ok": True, "status": "already_running"}

    ELEMENT_REFS.clear()
    await _browser_manager.start(
        persistent=persistent,
        user_data_dir=user_data_dir,
        headless=bool(payload.get("headless", False)),
        humanize=bool(payload.get("humanize", True)),
        proxy=str(payload.get("proxy", "")),
    )
    if isinstance(cookies, list) and cookies:
        try:
            await _browser_manager.add_cookies(cookies)
        except Exception as exc:
            raise HTTPException(400, f"cookie 注入失败: {exc}")
    return {"ok": True, "status": "started"}


@app.post("/api/v1/browser/stop")
async def browser_stop() -> dict:
    """关闭浏览器实例，清理所有页面和残留进程。"""
    ELEMENT_REFS.clear()
    await _browser_manager.stop()
    return {"ok": True, "status": "stopped"}


@app.get("/api/v1/browser/status")
async def browser_status() -> dict:
    """查询浏览器运行状态。"""
    return {"ok": True, "is_running": _browser_manager.is_running}


@app.post("/api/v1/cookie-sync/config")
async def cookie_sync_config(payload: dict) -> dict:
    """兼容旧前端接口；关闭浏览器自动回传 cookie 已停用。"""
    logger.info("[cookie-sync] close sync disabled, ignore config request")
    return {"ok": True, "disabled": True}


async def _require_page():
    """获取当前默认页面，浏览器未启动时返回 400 错误。"""
    page = await _browser_manager.ensure_page("default")
    if page is None:
        raise HTTPException(400, "浏览器未启动，请先调用 POST /api/v1/browser/start")
    return page


def _parse_field_requests(raw: object) -> list[tuple[str, object]]:
    if not isinstance(raw, list) or not raw:
        raise HTTPException(400, "fields must be a non-empty list")
    requests: list[tuple[str, object]] = []
    for item in raw:
        if not isinstance(item, dict) or len(item) != 1:
            raise HTTPException(400, "each field item must contain exactly one field name")
        field_name, spec = next(iter(item.items()))
        field = str(field_name).strip()
        if not field:
            raise HTTPException(400, "field name is required")
        requests.append((field, spec))
    return requests


def _make_fast_field_spec(spec_raw: object):
    """
    生成字段快速提取用的定位配置。

    Args:
        spec_raw: 云端下发的字段定位配置

    Returns:
        已压缩等待时间的元素定位配置。
    """
    spec = parse_element_locator_spec(spec_raw)
    spec.find_attempts = 1
    spec.find_interval_ms = 0
    spec.visible_timeout_ms = min(spec.visible_timeout_ms, FIELD_FAST_VISIBLE_TIMEOUT_MS)
    return spec


async def _find_element_items(page, spec, visible_only: bool = True, field_requests: list[tuple[str, object]] | None = None) -> list[dict[str, object]]:
    """
    查找元素列表，并可选提取每个元素内的字段。

    Args:
        page: 当前页面
        spec: 元素定位配置
        visible_only: 是否只返回当前视口内元素
        field_requests: 可选字段提取配置

    Returns:
        元素引用数组；传入字段配置时每项会包含 fields。
    """
    locators, _matched_parent, _matched_target = await find_all_locators_by_spec(page, spec, "目标元素集合")
    count = await locators.count()
    items: list[dict[str, object]] = []
    for index in range(count):
        locator = locators.nth(index)
        if visible_only:
            if not await is_locator_in_viewport(locator):
                continue
        item = dict(ELEMENT_REFS.register(locator, index))
        if field_requests:
            item["fields"] = await _extract_fields_from_container(locator, field_requests, f"元素[{index}]")
        items.append(item)
    return items


async def _extract_fields_from_container(container, field_requests: list[tuple[str, object]], container_label: str = "元素") -> dict[str, str]:
    """
    在指定元素内快速提取字段文本。

    Args:
        container: 页面或元素定位器
        field_requests: 字段名和定位规则列表
        container_label: 日志中展示的父级元素名称

    Returns:
        字段名到文本内容的映射。
    """
    fields: dict[str, str] = {}
    for field_name, spec_raw in field_requests:
        field_start = time.perf_counter()
        matched_target = ""
        try:
            spec = _make_fast_field_spec(spec_raw)
            if not spec.target_classes:
                fields[field_name] = ""
                continue
            locator, _matched_parent, _matched_target = await locate_element_by_spec(container, spec, f"字段 {field_name}")
            matched_target = _matched_target
            fields[field_name] = (await locator.inner_text(timeout=FIELD_FAST_TEXT_TIMEOUT_MS)).strip()
        except Exception as exc:
            fields[field_name] = ""
            logger.debug("字段快速提取失败 container=%s field=%s err=%s", container_label, field_name, exc)
        finally:
            elapsed_ms = int((time.perf_counter() - field_start) * 1000)
            logger.info(
                "字段快速提取完成 container=%s field=%s matched=%s 耗时=%dms 文本长度=%d",
                container_label,
                field_name,
                matched_target or "-",
                elapsed_ms,
                len(fields.get(field_name, "")),
            )
    return fields


async def _extract_text_from_locator(page, locator, mode: str, delay_before: float, task_id: str = "", label: str = "detail") -> str:
    """按模式从目标元素提取整段文本。"""
    total_start = time.perf_counter()
    if delay_before > 0:
        await asyncio.sleep(delay_before)
    if mode == "ocr":
        screenshot_start = time.perf_counter()
        screenshot_bytes = await screenshot_locator_full(page, locator, "detail-ocr")
        if screenshot_bytes is None:
            screenshot_bytes = await locator.screenshot(type="png")
        screenshot_ms = int((time.perf_counter() - screenshot_start) * 1000)
        logger.info("OCR 文本提取截图完成 bytes=%d 耗时=%dms 保存=未保存", len(screenshot_bytes), screenshot_ms)
        text = (await ocr_image_async(screenshot_bytes)).strip()
        total_ms = int((time.perf_counter() - total_start) * 1000)
        logger.info("OCR 文本提取完成 总耗时=%dms 文本长度=%d", total_ms, len(text))
        return text
    text = (await locator.inner_text(timeout=3000)).strip()
    total_ms = int((time.perf_counter() - total_start) * 1000)
    logger.debug("DOM 文本提取完成 总耗时=%dms 文本长度=%d", total_ms, len(text))
    return text


async def _probe_click_state(locator) -> dict:
    """采样目标元素点击状态，便于区分遮挡与节点变化。"""
    try:
        state = await locator.evaluate(
            """(el) => {
                if (!el) return { ok:false, reason:"no_element" };
                const connected = !!el.isConnected;
                const rect = el.getBoundingClientRect ? el.getBoundingClientRect() : null;
                if (!rect) return { ok:true, connected, reason:"no_rect" };
                const cx = rect.left + rect.width / 2;
                const cy = rect.top + rect.height / 2;
                const top = document.elementFromPoint(cx, cy);
                const centerHit = !!top && (top === el || el.contains(top));
                return {
                    ok: true,
                    connected,
                    center: { x: cx, y: cy },
                    centerHit,
                    topAtCenter: top ? {
                        tag: (top.tagName || "").toLowerCase(),
                        id: top.id || "",
                        className: typeof top.className === "string" ? top.className : ""
                    } : null
                };
            }"""
        )
        if isinstance(state, dict):
            state["inViewport"] = await is_locator_in_viewport(locator)
            return state
    except Exception as exc:
        return {"ok": False, "reason": "probe_exception", "error": repr(exc)}
    return {"ok": False, "reason": "probe_unknown"}


async def _safe_random_click(page, locator, matched_target: str) -> None:
    """使用最新元素位置执行随机点点击，跳过底层 human click 的二次滚动定位。"""
    latest_probe = await _probe_click_state(locator)
    latest_box = await locator.bounding_box()
    logger.info("快速点击前复查 target=%s probe=%s box=%s", matched_target, latest_probe, latest_box)
    if not latest_probe.get("ok"):
        raise HTTPException(400, f"点击目标元素状态检查失败: {matched_target}, probe={latest_probe}")
    if not latest_probe.get("connected", False):
        raise HTTPException(400, f"点击目标元素节点已失效: {matched_target}, probe={latest_probe}")
    if not latest_probe.get("inViewport", False):
        logger.info("点击目标不在视口内，准备滚动后重试 target=%s probe=%s", matched_target, latest_probe)
        if not await scroll_locator_into_view(locator, matched_target):
            latest_probe = await _probe_click_state(locator)
            raise HTTPException(400, f"点击目标元素不在视口内: {matched_target}, probe={latest_probe}")
        latest_probe = await _probe_click_state(locator)
        latest_box = await locator.bounding_box()
        logger.info("点击目标滚动后复查 target=%s probe=%s box=%s", matched_target, latest_probe, latest_box)
        if not latest_probe.get("inViewport", False):
            raise HTTPException(400, f"点击目标元素不在视口内: {matched_target}, probe={latest_probe}")
    if not latest_probe.get("centerHit", False):
        raise HTTPException(400, f"点击目标元素中心点被遮挡: {matched_target}, probe={latest_probe}")
    if not latest_box or latest_box.get("width", 0) <= 0 or latest_box.get("height", 0) <= 0:
        raise HTTPException(400, f"点击目标元素位置无效: {matched_target}, box={latest_box}")
    click_start = time.perf_counter()
    if not await click_box_random_point(page, latest_box, matched_target):
        raise HTTPException(400, f"点击目标元素随机点失败: {matched_target}, box={latest_box}")
    elapsed_ms = int((time.perf_counter() - click_start) * 1000)
    logger.info("点击成功 target=%s phase=safe_random_click elapsed_ms=%d box=%s", matched_target, elapsed_ms, latest_box)


async def _resolve_locator_from_payload(page, payload: dict, label: str):
    """按 element_ref 或 element 解析单个目标元素定位器。"""
    element_ref = str(payload.get("element_ref", "")).strip()
    if element_ref:
        entry = ELEMENT_REFS.get(element_ref)
        if entry is None:
            raise HTTPException(404, "element_ref not found")
        return entry.locator, element_ref

    spec = parse_element_locator_spec(payload.get("element"))
    if not spec.target_classes:
        raise HTTPException(400, "element.target_classes is required")
    locator, _matched_parent, matched_target = await locate_element_by_spec(page, spec, label)
    return locator, matched_target


@app.post("/api/v1/page/open")
async def page_open(payload: dict) -> dict:
    """打开指定 URL 页面，注册为默认页面供后续操作使用。

    请求体参数：
        url: 目标页面 URL（必填）
        timeout: 导航超时毫秒数（默认 30000）
        user_data_dir: 可选，指定账号目录名；传入时会先切换到对应目录再打开页面
    """
    url = str(payload.get("url", "")).strip()
    if not url:
        raise HTTPException(400, "url is required")

    user_data_dir = str(payload.get("user_data_dir") or "").strip()
    if user_data_dir:
        # 允许 open 接口直接按账号目录切换浏览器上下文，避免前端必须先单独调 start。
        await browser_start(
            {
                "persistent": bool(payload.get("persistent", True)),
                "user_data_dir": user_data_dir,
                "headless": bool(payload.get("headless", False)),
                "humanize": bool(payload.get("humanize", True)),
                "proxy": str(payload.get("proxy", "")),
                "cookies": payload.get("cookies"),
            }
        )

    try:
        page = await _browser_manager.new_page("default")
    except RuntimeError as exc:
        raise HTTPException(400, str(exc))
    ELEMENT_REFS.clear()
    timeout = int(payload.get("timeout", 30000))
    cookies = payload.get("cookies")
    if isinstance(cookies, list) and cookies and not user_data_dir:
        try:
            await page.context.add_cookies(cookies)
        except Exception as exc:
            raise HTTPException(400, f"cookie 注入失败: {exc}")

    success = await navigate_to_page(page, url, timeout=timeout)
    if not success:
        raise HTTPException(500, "页面导航失败")

    return {"ok": True, "url": url, "title": await page.title()}


@app.post("/api/v1/page/scroll")
async def page_scroll(payload: dict) -> dict:
    """滚动当前页面，模拟人工浏览加载候选人列表。

    请求体参数：
        scroll_delay_min: 滚动最小延迟秒数（默认 0.1）
        scroll_delay_max: 滚动最大延迟秒数（默认 0.9）
        max_scrolls: 最大滚动次数（默认 20）
        element: 可选统一元素定位对象，支持 parent_classes 和 target_classes
    """
    page = await _require_page()
    element_spec = parse_element_locator_spec(payload.get("element"))
    if "element" in payload and not element_spec.target_classes:
        raise HTTPException(400, "element.target_classes is required")
    await scroll_to_load(
        page,
        scroll_delay_min=float(payload.get("scroll_delay_min", 0.1)),
        scroll_delay_max=float(payload.get("scroll_delay_max", 0.9)),
        max_scrolls=int(payload.get("max_scrolls", 20)),
        element_spec=element_spec,
    )
    return {"ok": True}


@app.post("/api/v1/page/find-elements")
async def page_find_elements(payload: dict) -> dict:
    """查找一组元素，返回元素引用数组，并可选返回每个元素内的字段。"""
    page = await _require_page()
    spec = parse_element_locator_spec(payload.get("element"))
    if not spec.target_classes:
        raise HTTPException(400, "element.target_classes is required")
    visible_only = bool(payload.get("visible_only", True))
    field_requests = _parse_field_requests(payload.get("fields")) if payload.get("fields") else None
    ELEMENT_REFS.clear()
    items = await _find_element_items(page, spec, visible_only=visible_only, field_requests=field_requests)
    return {"ok": True, "items": items, "count": len(items)}


@app.post("/api/v1/page/extract-text")
async def page_extract_text(payload: dict) -> dict:
    """提取目标元素的整段文本，支持 DOM 或 OCR 模式。"""
    page = await _require_page()
    mode = str(payload.get("mode", "dom")).strip().lower() or "dom"
    if mode not in {"dom", "ocr"}:
        raise HTTPException(400, "mode must be dom or ocr")
    delay_before = float(payload.get("delay_before", 0))
    raw_elements = payload.get("elements")
    if not isinstance(raw_elements, list) or not raw_elements:
        raise HTTPException(400, "elements is required and must be a non-empty array")
    texts: list[str] = []
    matched_list: list[str] = []
    task_id = str(payload.get("task_id", "")).strip()
    request_start = time.perf_counter()
    logger.info("开始提取详情文本 mode=%s elements=%d delay_before=%.2fs", mode, len(raw_elements), delay_before)
    for index, element_raw in enumerate(raw_elements):
        if not isinstance(element_raw, dict):
            raise HTTPException(400, f"elements[{index}] must be an object")
        item_start = time.perf_counter()
        locator, matched = await _resolve_locator_from_payload(page, {"element": element_raw}, f"文本提取元素[{index}]")
        text = await _extract_text_from_locator(page, locator, mode, delay_before, task_id, f"element_{index}")
        item_ms = int((time.perf_counter() - item_start) * 1000)
        logger.info(
            "详情文本元素提取完成 index=%d matched=%s 耗时=%dms 文本长度=%d",
            index,
            matched,
            item_ms,
            len(text),
        )
        texts.append(text)
        matched_list.append(matched)
    request_ms = int((time.perf_counter() - request_start) * 1000)
    logger.info("详情文本提取请求完成 mode=%s elements=%d 总耗时=%dms", mode, len(raw_elements), request_ms)
    return {
        "ok": True,
        "text": "\n\n".join([t for t in texts if str(t).strip() != ""]),
        "texts": texts,
        "matched": ",".join(matched_list),
        "matched_list": matched_list,
        "mode": mode,
    }


@app.post("/api/v1/page/in-viewport")
async def page_in_viewport(payload: dict) -> dict:
    """判断目标元素当前是否位于可视区域内。"""
    page = await _require_page()
    locator, matched = await _resolve_locator_from_payload(page, payload, "视口判断元素")
    in_viewport = await is_locator_in_viewport(locator)
    return {"ok": True, "in_viewport": in_viewport, "matched": matched}


@app.post("/api/v1/page/scroll-into-view")
async def page_scroll_into_view(payload: dict) -> dict:
    """将目标元素滚动到可视区域内。"""
    page = await _require_page()
    locator, matched = await _resolve_locator_from_payload(page, payload, "滚动到视口元素")
    in_viewport = await scroll_locator_into_view(locator, str(matched))
    if not in_viewport:
        raise HTTPException(400, f"目标元素未能滚动到视口内: {matched}")
    return {"ok": True, "in_viewport": True, "matched": matched}


@app.post("/api/v1/page/click")
async def page_click(payload: dict) -> dict:
    """点击当前页面中的元素。

    请求体参数：
        element: 统一元素定位对象
        timeout: 等待超时毫秒数（默认 10000）
        delay_before: 点击前延迟秒数（默认 0.5）
    """
    page = await _require_page()
    timeout = int(payload.get("timeout", 10000))
    delay_before = float(payload.get("delay_before", 0.5))
    element_ref = str(payload.get("element_ref", "")).strip()
    if element_ref:
        entry = ELEMENT_REFS.get(element_ref)
        if entry is None:
            raise HTTPException(404, "element_ref not found")
        container = entry.locator
        element_spec = parse_element_locator_spec(payload.get("element"))
        if not element_spec.target_classes:
            raise HTTPException(400, "element.target_classes is required")
        locator, _matched_parent, matched_target = await locate_element_by_spec(container, element_spec, "点击目标元素")
    else:
        element_spec = parse_element_locator_spec(payload.get("element"))
        if not element_spec.target_classes:
            raise HTTPException(400, "element.target_classes is required")
        locator, _matched_parent, matched_target = await locate_element_by_spec(page, element_spec, "点击目标元素")
    if await locator.is_visible(timeout=timeout):
        in_viewport_before = await is_locator_in_viewport(locator)
        box_before = await locator.bounding_box()
        probe_before = await _probe_click_state(locator)
        logger.info(
            "点击前状态 target=%s visible=true in_viewport=%s box=%s probe=%s delay_before=%.2fs timeout=%dms",
            matched_target,
            in_viewport_before,
            box_before,
            probe_before,
            delay_before,
            timeout,
        )
        await move_mouse_to_locator(locator, matched_target)
        # 旧方案会进入底层 human click 的二次滚动定位，异常时可能固定等待约 30 秒。
        # await locator.click(delay=100-300ms)
        await _safe_random_click(page, locator, matched_target)
        success = True
    else:
        raise HTTPException(400, f"点击目标元素不可见: {matched_target}")
    return {"ok": True, "clicked": success}


@app.post("/api/v1/page/press-key")
async def page_press_key(payload: dict) -> dict:
    """在当前页面发送键盘按键。

    请求体参数：
        key: 按键名，例如 Escape、Enter、ArrowDown。
    """
    page = await _require_page()
    key = str(payload.get("key", "")).strip()
    if not key:
        raise HTTPException(400, "key is required")
    await page.keyboard.press(key)
    return {"ok": True, "key": key}


@app.post("/api/v1/page/type-text")
async def page_type_text(payload: dict) -> dict:
    """
    向当前已聚焦输入框分段输入文字。

    请求体参数：
        text: 必填，要输入的文本。
        chunk_min: 可选，每段最少字符数，默认 1。
        chunk_max: 可选，每段最多字符数，默认 2。
        delay_min_ms: 可选，每段输入后的最小等待毫秒数，默认 80。
        delay_max_ms: 可选，每段输入后的最大等待毫秒数，默认 220。
    """
    page = await _require_page()
    text = str(payload.get("text", ""))
    if not text:
        raise HTTPException(400, "text is required")
    result = await human_type_focused(
        page,
        text,
        chunk_min=_parse_int(payload.get("chunk_min"), 1),
        chunk_max=_parse_int(payload.get("chunk_max"), 2),
        delay_min_ms=_parse_int(payload.get("delay_min_ms"), 80),
        delay_max_ms=_parse_int(payload.get("delay_max_ms"), 220),
    )
    return {"ok": True, **result}


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


@app.post("/api/v1/crypto/decrypt")
async def crypto_decrypt(payload: dict) -> dict:
    """用 Agent 私钥解密对称密钥 SK，再用 SK 解密密文。"""
    encrypted_sk_b64 = str(payload.get("encrypted_sk", "")).strip()
    encrypted_data_b64 = str(payload.get("encrypted_data", "")).strip()
    if not encrypted_sk_b64 or not encrypted_data_b64:
        raise HTTPException(400, "encrypted_sk and encrypted_data are required")

    import base64

    sk = decrypt_wrapped_key(CRYPTO_KEYS["private_key"], encrypted_sk_b64)
    plaintext = decrypt_aes_gcm(base64.b64decode(encrypted_data_b64), sk)

    return {"ok": True, "data": base64.b64encode(plaintext).decode()}


@app.post("/api/v1/cookies/decrypt")
async def cookies_decrypt(payload: dict) -> dict:
    """按当前机器密钥解密云端下发的 cookie 数据。"""
    encrypted_data_b64 = str(payload.get("encrypted_data", "")).strip()
    encrypted_keys = payload.get("encrypted_keys")
    if not encrypted_data_b64 or not isinstance(encrypted_keys, dict):
        raise HTTPException(400, "encrypted_data and encrypted_keys are required")

    try:
        cookies = decrypt_cookie_payload(
            CRYPTO_KEYS["private_key"],
            cookie_machine_ids(MACHINE),
            encrypted_data_b64,
            encrypted_keys,
        )
    except ValueError as exc:
        raise HTTPException(400, str(exc))
    except Exception as exc:
        raise HTTPException(500, f"cookie 解密失败: {exc}")

    return {"ok": True, "cookies": cookies, "count": len(cookies)}


@app.get("/api/v1/page/url")
async def page_url() -> dict:
    """返回当前页面 URL。"""
    page = await _require_page()
    return {"ok": True, "url": page.url}


@app.get("/api/v1/page/cookies")
async def page_cookies() -> dict:
    """导出当前浏览器上下文 cookies JSON。"""
    await _require_page()
    logger.info("[cookie-export] request received from /api/v1/page/cookies")
    cookies = await _browser_manager.export_cookies()
    logger.info("[cookie-export] success cookies=%d", len(cookies))
    return {"ok": True, "cookies": cookies}

@app.post("/api/v1/page/load-profile")
async def page_load_profile(payload: dict) -> dict:
    import shutil, tempfile, base64, os
    data = base64.b64decode(str(payload.get("data","")).strip())
    if not payload.get("data"): raise HTTPException(400, "data required")
    tmp = tempfile.mktemp(suffix=".tar.gz")
    with open(tmp,"wb") as f: f.write(data)
    target = str(_profile_dir(str(payload.get("name","default"))))
    os.makedirs(target,exist_ok=True)
    shutil.unpack_archive(tmp, target)
    os.remove(tmp)
    return {"ok":True,"dir":target}

@app.post("/api/v1/page/export-profile")
async def page_export_profile() -> dict:
    """导出浏览器 profile 目录为 tar.gz + Base64。"""
    import shutil, tempfile, base64, os
    page = await _require_page()
    user_data_dir = _browser_manager._last_user_data_dir
    if not user_data_dir: raise HTTPException(400, "未使用持久化模式")
    tmp = tempfile.mktemp(suffix=".tar.gz"); base = tmp.replace(".tar.gz", "")
    shutil.make_archive(base, "gztar", user_data_dir)
    with open(tmp, "rb") as f: data = base64.b64encode(f.read()).decode()
    os.remove(tmp)
    return {"ok": True, "data": data, "size": len(data)}

@app.get("/api/v1/ocr/status")
async def ocr_status() -> dict:
    """检查 OCR 是否可用。"""
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


@app.post("/api/v1/sound/play")
async def sound_play(payload: dict) -> dict:
    """播放提示音。

    请求体支持：
    - kind: success | failed（播放内置音频）
    - url: 网络音频地址（先检查/下载到本地后播放一次）
    """
    kind = str(payload.get("kind", "")).strip().lower()
    url = str(payload.get("url", "")).strip()
    if not kind and not url:
        raise HTTPException(400, "kind or url is required")

    try:
        if url:
            audio = await ensure_audio_from_url(url)
        else:
            audio = resolve_builtin_audio(kind)
        play_once(audio)
        return {"ok": True, "file": str(audio)}
    except FileNotFoundError as exc:
        raise HTTPException(404, str(exc))
    except httpx.HTTPError as exc:
        raise HTTPException(502, f"download audio failed: {exc}")
    except ValueError as exc:
        raise HTTPException(400, str(exc))
    except Exception as exc:
        raise HTTPException(500, f"play audio failed: {exc}")


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

    formatter = logging.Formatter("%(asctime)s %(levelname)s %(name)s: %(message)s")
    root_logger = logging.getLogger()
    root_logger.setLevel(logging.INFO)
    root_logger.handlers.clear()
    if os.getenv("GOODHR_AGENT_LOG_TO_STDOUT", "1") != "0":
        stream_handler = logging.StreamHandler()
        stream_handler.setFormatter(formatter)
        root_logger.addHandler(stream_handler)

    port = find_port()
    app.state.port = port  # 保存到应用状态，供 /health 返回

    logger.info("GoodHR 5 Local Agent starting on http://%s:%s", HOST, port)
    uvicorn.run(app, host=HOST, port=port, log_level="warning", access_log=False)


if __name__ == "__main__":
    main()
