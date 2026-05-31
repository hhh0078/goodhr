"""本文件负责 Local Agent 主动连接云端 WebSocket 并执行云端下发命令。"""

from __future__ import annotations

import asyncio
import json
import logging
import secrets
import time
import traceback
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from urllib.parse import quote

import websockets
import httpx

from app.crypto_keys import load_or_generate as load_crypto_keys
from app.cookie_crypto import decrypt_cookie_payload
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
    scroll_locator_into_view,
    scroll_to_load,
)
from app.machine import cookie_machine_ids, load_machine
from app.ocr import ocr_image_async
from app.paths import data_dir
from app.screenshot import screenshot_locator_full
from app.sound import ensure_audio_from_url, play_once, resolve_builtin_audio
from app.tasks import screenshot_path


REPLY_TIMEOUT_SECONDS = 8
MAX_RETRIES = 3
MACHINE = load_machine()
CRYPTO_KEYS = load_crypto_keys()
logger = logging.getLogger("goodhr5.local-agent.ws")
FIELD_FAST_VISIBLE_TIMEOUT_MS = 120
FIELD_FAST_TEXT_TIMEOUT_MS = 300


def _payload_summary(payload: Any) -> str:
    """生成适合日志查看的消息摘要，避免整包 payload 过大。"""
    if not isinstance(payload, dict):
        return str(payload)
    if "fields" in payload and isinstance(payload.get("fields"), dict):
        field_parts: list[str] = []
        fields = payload.get("fields") or {}
        non_empty = 0
        for key, value in fields.items():
            text = str(value or "").strip()
            if text:
                non_empty += 1
                preview = text.replace("\n", " ")
                if len(preview) > 18:
                    preview = preview[:18] + "..."
                field_parts.append(f"{key}={preview}")
            else:
                field_parts.append(f"{key}=空")
        return f"fields命中={non_empty}/{len(fields)}, " + ", ".join(field_parts)
    if "items" in payload and isinstance(payload.get("items"), list):
        count = payload.get("count")
        items = payload.get("items") or []
        refs = [str(item.get("ref")) for item in items[:3] if isinstance(item, dict) and item.get("ref")]
        if refs:
            return f"items={count or len(items)}个, refs={refs}"
        return f"items={count or len(items)}个"
    if "text" in payload:
        text = str(payload.get("text") or "").strip().replace("\n", " ")
        preview = text[:48] + "..." if len(text) > 48 else text
        return f"text_len={len(text)}, text={preview or '空'}"
    if "cookies" in payload and isinstance(payload.get("cookies"), list):
        return f"cookies={payload.get('count') or len(payload.get('cookies') or [])}条"
    path = str(payload.get("path") or "")
    body = payload.get("body") or {}
    if not isinstance(body, dict):
        body = {}
    parts: list[str] = []
    if path:
        parts.append(f"path={path}")
    if "url" in body:
        parts.append(f"url={body.get('url')}")
    if "user_data_dir" in body:
        parts.append(f"user_data_dir={body.get('user_data_dir')}")
    element = body.get("element")
    if isinstance(element, dict):
        parent_classes = element.get("parent_classes")
        target_classes = element.get("target_classes")
        if isinstance(parent_classes, list) and parent_classes:
            parts.append(f"parent_classes={parent_classes}")
        if isinstance(target_classes, list) and target_classes:
            parts.append(f"target_classes={target_classes}")
    cookies = body.get("cookies")
    if isinstance(cookies, list):
        parts.append(f"cookies={len(cookies)}条")
    if "encrypted_data" in body:
        parts.append("encrypted_data=已传")
    encrypted_keys = body.get("encrypted_keys")
    if isinstance(encrypted_keys, dict):
        parts.append(f"encrypted_keys={len(encrypted_keys)}个机器")
    selectors = body.get("selectors")
    if isinstance(selectors, dict):
        parts.append(f"selectors={list(selectors.keys())}")
    fields = body.get("fields")
    if isinstance(fields, list):
        names: list[str] = []
        for item in fields:
            if isinstance(item, dict) and item:
                names.extend([str(key) for key in item.keys()])
        if names:
            parts.append(f"fields={names}")
    if "element_ref" in body:
        parts.append(f"element_ref={body.get('element_ref')}")
    if "in_viewport" in payload:
        parts.append(f"in_viewport={payload.get('in_viewport')}")
    if "matched" in payload:
        parts.append(f"matched={payload.get('matched')}")
    if "max_scrolls" in body:
        parts.append(f"max_scrolls={body.get('max_scrolls')}")
    return ", ".join(parts) if parts else str(sorted(body.keys()))


def _message_id() -> str:
    """
    生成 WebSocket 消息 ID。

    Returns:
        返回带 msg_ 前缀的随机消息 ID。
    """
    return f"msg_{secrets.token_hex(16)}"


@dataclass
class WSAgentState:
    """记录 Local Agent 到云端 WebSocket 的连接状态。"""

    status: str = "未连接"
    connected: bool = False
    cloud_ws_url: str = ""
    last_error: str = ""
    last_message: str = ""


@dataclass
class WSAgentClient:
    """维护 Local Agent 到云端的唯一 WebSocket 连接。"""

    browser_manager: Any
    state: WSAgentState = field(default_factory=WSAgentState)
    _task: asyncio.Task | None = None
    _ws: Any = None
    _pending: dict[str, asyncio.Future] = field(default_factory=dict)
    _active_tasks: set[str] = field(default_factory=set)

    async def connect(self, cloud_ws_url: str, token: str) -> dict:
        """
        启动或替换当前云端 WebSocket 连接。

        Args:
            cloud_ws_url: 云端 WebSocket 地址。
            token: 当前云端登录 access_token。

        Returns:
            返回当前连接状态。
        """
        logger.info("[任务WS] 收到连接请求 cloud_ws_url=%s", cloud_ws_url)
        await self.disconnect()
        self.state.cloud_ws_url = self._url_with_token(cloud_ws_url, token)
        self.state.status = "连接中"
        self.state.last_error = ""
        self._task = asyncio.create_task(self._run_forever())
        return self.status()

    async def start_task(self, task_id: str, cloud_api_base: str, cloud_ws_url: str, token: str) -> dict:
        """
        建立任务级 WebSocket 并通知云端开始任务。

        Args:
            task_id: 云端任务 ID。
            cloud_api_base: 云端 HTTP API 基础地址。
            cloud_ws_url: 云端 WebSocket 地址。
            token: 当前云端登录 access_token。

        Returns:
            返回任务启动提示。
        """
        if not task_id:
            raise ValueError("task_id is required")
        logger.info("[任务WS] 收到开始任务请求 task=%s api=%s ws=%s", task_id, cloud_api_base, cloud_ws_url)
        connected = await self._connect_with_retries(cloud_ws_url, token, MAX_RETRIES)
        if not connected:
            raise RuntimeError("本地程序无法与服务器建立连接")
        try:
            await self._post_cloud_task(cloud_api_base, token, task_id, "run")
        except Exception:
            if not self._active_tasks:
                await self.disconnect()
            raise
        self._active_tasks.add(task_id)
        self.state.last_message = "任务开始，请关注日志"
        logger.info("[任务WS] 开始任务成功 task=%s active_tasks=%s", task_id, sorted(self._active_tasks))
        return {"ok": True, "message": "任务开始，请关注日志", "status": self.status()}

    async def stop_task(self, task_id: str, cloud_api_base: str, token: str) -> dict:
        """
        停止指定任务，并在没有活跃任务时断开 WebSocket。

        Args:
            task_id: 云端任务 ID。
            cloud_api_base: 云端 HTTP API 基础地址。
            token: 当前云端登录 access_token。

        Returns:
            返回任务停止状态。
        """
        if not task_id:
            raise ValueError("task_id is required")
        logger.info("[任务WS] 收到停止任务请求 task=%s api=%s", task_id, cloud_api_base)
        await self._post_cloud_task(cloud_api_base, token, task_id, "stop")
        self._active_tasks.discard(task_id)
        if not self._active_tasks:
            await self.disconnect()
        logger.info("[任务WS] 停止任务成功 task=%s active_tasks=%s", task_id, sorted(self._active_tasks))
        return {"ok": True, "message": "任务已停止", "status": self.status()}

    async def disconnect(self) -> dict:
        """
        关闭当前 WebSocket 连接。

        Returns:
            返回关闭后的连接状态。
        """
        if self._task:
            self._task.cancel()
            self._task = None
        if self._ws:
            await self._ws.close()
            self._ws = None
        self._active_tasks.clear()
        self.state.connected = False
        self.state.status = "未连接"
        logger.info("[任务WS] 已手动断开连接")
        return self.status()

    def status(self) -> dict:
        """
        读取当前 WebSocket 状态。

        Returns:
            返回给前端展示的状态字典。
        """
        return {
            "ok": True,
            "connected": self.state.connected,
            "status": self.state.status,
            "last_error": self.state.last_error,
            "last_message": self.state.last_message,
        }

    async def _run_forever(self) -> None:
        """
        持续连接云端 WebSocket，断开后自动重连。
        """
        while True:
            try:
                logger.info("[任务WS] 正在连接云端WS url=%s", self.state.cloud_ws_url)
                async with websockets.connect(self.state.cloud_ws_url, ping_interval=20, ping_timeout=20) as ws:
                    self._ws = ws
                    self.state.connected = True
                    self.state.status = "已连接"
                    self.state.last_error = ""
                    logger.info("[任务WS] 云端WS已连接")
                    await self._send_nowait("agent.status", "", {"status": "online"})
                    async for raw in ws:
                        # logger.info("[任务WS] 收到原始消息长度=%d", len(raw))
                        await self._handle_raw(raw)
                    self.state.connected = False
                    self.state.status = "重连中"
                    logger.warning("[任务WS] 连接已关闭，准备重连")
            except asyncio.CancelledError:
                logger.info("[任务WS] 连接循环已取消")
                break
            except Exception as exc:
                self.state.connected = False
                self.state.status = "重连中"
                self.state.last_error = str(exc)
                logger.exception("[任务WS] 连接循环异常: %s", exc)
                await asyncio.sleep(3)

    async def _connect_with_retries(self, cloud_ws_url: str, token: str, retries: int) -> bool:
        """
        按次数尝试建立云端 WebSocket。

        Args:
            cloud_ws_url: 云端 WebSocket 地址。
            token: 当前云端登录 access_token。
            retries: 最大尝试次数。

        Returns:
            返回是否连接成功。
        """
        if self.state.connected:
            logger.info("[任务WS] 已连接，跳过重试")
            return True
        for attempt in range(1, retries + 1):
            logger.info("[任务WS] 第 %d/%d 次尝试建立连接", attempt, retries)
            await self.connect(cloud_ws_url, token)
            if await self._wait_connected(5):
                logger.info("[任务WS] 第 %d 次连接成功", attempt)
                return True
            await self.disconnect()
            logger.warning("[任务WS] 第 %d 次连接失败", attempt)
        return False

    async def _wait_connected(self, timeout_seconds: int) -> bool:
        """
        等待 WebSocket 进入已连接状态。

        Args:
            timeout_seconds: 最长等待秒数。

        Returns:
            返回是否在超时前连接成功。
        """
        deadline = asyncio.get_running_loop().time() + timeout_seconds
        while asyncio.get_running_loop().time() < deadline:
            if self.state.connected:
                return True
            await asyncio.sleep(0.2)
        return False

    async def _post_cloud_task(self, cloud_api_base: str, token: str, task_id: str, action: str) -> dict:
        """
        调用云端任务运行或停止接口。

        Args:
            cloud_api_base: 云端 HTTP API 基础地址。
            token: 当前云端登录 access_token。
            task_id: 云端任务 ID。
            action: run 或 stop。

        Returns:
            返回云端响应 JSON。
        """
        base = cloud_api_base.rstrip("/")
        url = f"{base}/api/tasks/{quote(task_id)}/{action}"
        async with httpx.AsyncClient(timeout=30) as client:
            logger.info("[任务WS] 正在调用云端任务接口 action=%s task=%s url=%s", action, task_id, url)
            resp = await client.post(url, headers={"Authorization": f"Bearer {token}"})
        data = resp.json() if resp.content else {}
        logger.info("[任务WS] 云端任务接口返回 action=%s task=%s status=%s body=%s", action, task_id, resp.status_code, data)
        if resp.status_code >= 400 or not data.get("ok", False):
            raise RuntimeError(str(data.get("error") or f"云端任务{action}失败"))
        return data

    async def _handle_raw(self, raw: str) -> None:
        """
        处理云端发来的原始 WebSocket 消息。

        Args:
            raw: 云端发送的 JSON 字符串。
        """
        try:
            message = json.loads(raw)
        except json.JSONDecodeError:
            logger.warning("[任务WS] 收到非法JSON raw=%s", raw)
            return
        reply_to = str(message.get("reply_to") or "")
        if reply_to:
            logger.info("[任务WS] 收到回复 reply_to=%s ok=%s type=%s task=%s", reply_to, message.get("ok"), message.get("type"), message.get("task_id"))
            future = self._pending.pop(reply_to, None)
            if future and not future.done():
                future.set_result(message)
            return

        message_id = str(message.get("message_id") or "")
        task_id = str(message.get("task_id") or "")
        msg_type = str(message.get("type") or "")
        # logger.info("[任务WS] 开始处理命令 type=%s task=%s message_id=%s", msg_type, task_id, message_id)
        try:
            payload = message.get("payload") or {}
            result = await self._execute_command(msg_type, task_id, payload)
            await self._send_reply(message_id, msg_type, task_id, True, "", result)
        except Exception as exc:
            logger.exception("[任务WS] 命令执行失败 type=%s task=%s: %s", msg_type, task_id, exc)
            await self._send_reply(
                message_id,
                msg_type,
                task_id,
                False,
                str(exc),
                {
                    "detail": str(exc),
                    "traceback": traceback.format_exc(),
                },
            )

    async def _execute_command(self, msg_type: str, task_id: str, payload: dict) -> dict:
        """
        执行云端下发的命令。

        Args:
            msg_type: 消息类型。
            task_id: 任务 ID 或 cookie ID。
            payload: 命令参数。

        Returns:
            返回命令执行结果。
        """
        if msg_type == "cookie.capture.start":
            return await self._capture_cookie(task_id, payload)
        if msg_type == "local.http.post":
            return await self._execute_local_post(payload)
        raise ValueError(f"unsupported message type: {msg_type}")

    async def _execute_local_post(self, payload: dict) -> dict:
        """
        执行云端任务编排下发的本地浏览器操作。

        Args:
            payload: 包含 path 和 body 的操作请求。

        Returns:
            返回与原本 HTTP API 兼容的结果字典。
        """
        path = str(payload.get("path") or "")
        body = payload.get("body") or {}
        if not isinstance(body, dict):
            body = {}
        logger.info("[任务WS] 执行本地命令 %s", _payload_summary(payload))
        if path == "/api/v1/browser/start":
            user_data_dir = str(body.get("user_data_dir") or "").strip()
            if user_data_dir:
                body["user_data_dir"] = str(_profile_dir(user_data_dir))
            ELEMENT_REFS.clear()
            await self.browser_manager.start(
                persistent=bool(body.get("persistent", False)),
                user_data_dir=body.get("user_data_dir"),
                headless=bool(body.get("headless", False)),
                humanize=bool(body.get("humanize", True)),
                proxy=str(body.get("proxy", "")),
            )
            cookies = body.get("cookies")
            if isinstance(cookies, list) and cookies:
                await self.browser_manager.add_cookies(cookies)
            return {"ok": True, "status": "started"}
        if path == "/api/v1/browser/stop":
            ELEMENT_REFS.clear()
            await self.browser_manager.stop()
            return {"ok": True, "status": "stopped"}
        if path == "/api/v1/page/open":
            user_data_dir = str(body.get("user_data_dir") or "").strip()
            if user_data_dir:
                body["user_data_dir"] = str(_profile_dir(user_data_dir))
                await self.browser_manager.start(
                    persistent=bool(body.get("persistent", True)),
                    user_data_dir=body.get("user_data_dir"),
                    headless=bool(body.get("headless", False)),
                    humanize=bool(body.get("humanize", True)),
                    proxy=str(body.get("proxy", "")),
                )
            page = await self.browser_manager.new_page("default")
            ELEMENT_REFS.clear()
            url = str(body.get("url") or "").strip()
            if not url:
                raise ValueError("url is required")
            cookies = body.get("cookies")
            if isinstance(cookies, list) and cookies:
                await self.browser_manager.add_cookies(cookies)
            ok = await navigate_to_page(page, url, timeout=int(body.get("timeout", 30000)))
            if not ok:
                raise RuntimeError("页面导航失败")
            return {"ok": True, "url": url, "title": await page.title()}
        if path == "/api/v1/page/scroll":
            page = await self._require_page()
            element_spec = parse_element_locator_spec(body.get("element"))
            if "element" in body and not element_spec.target_classes:
                raise ValueError("element.target_classes is required")
            await scroll_to_load(
                page,
                scroll_delay_min=float(body.get("scroll_delay_min", 0.1)),
                scroll_delay_max=float(body.get("scroll_delay_max", 0.9)),
                max_scrolls=int(body.get("max_scrolls", 20)),
                element_spec=element_spec,
            )
            return {"ok": True}
        if path == "/api/v1/page/find-elements":
            page = await self._require_page()
            element_spec = parse_element_locator_spec(body.get("element"))
            if not element_spec.target_classes:
                raise ValueError("element.target_classes is required")
            visible_only = bool(body.get("visible_only", True))
            field_requests = self._parse_field_requests(body.get("fields")) if body.get("fields") else None
            ELEMENT_REFS.clear()
            items = await self._find_element_items(page, element_spec, visible_only=visible_only, field_requests=field_requests)
            return {"ok": True, "items": items, "count": len(items)}
        if path == "/api/v1/page/extract-text":
            page = await self._require_page()
            mode = str(body.get("mode", "dom") or "dom").strip().lower()
            if mode not in {"dom", "ocr"}:
                raise ValueError("mode must be dom or ocr")
            delay_before = float(body.get("delay_before", 0))
            raw_elements = body.get("elements")
            if not isinstance(raw_elements, list) or not raw_elements:
                raise ValueError("elements is required and must be a non-empty array")
            texts: list[str] = []
            matched_list: list[str] = []
            task_id_for_screenshot = str(body.get("task_id") or "").strip()
            request_start = time.perf_counter()
            logger.info("开始提取详情文本 mode=%s elements=%d delay_before=%.2fs", mode, len(raw_elements), delay_before)
            for index, element_raw in enumerate(raw_elements):
                if not isinstance(element_raw, dict):
                    raise ValueError(f"elements[{index}] must be an object")
                item_start = time.perf_counter()
                locator, matched = await self._resolve_locator_from_payload(page, {"element": element_raw}, f"文本提取元素[{index}]")
                text = await self._extract_text_from_locator(page, locator, mode, delay_before, task_id_for_screenshot, f"element_{index}")
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
        if path == "/api/v1/page/in-viewport":
            page = await self._require_page()
            locator, matched = await self._resolve_locator_from_payload(page, body, "视口判断元素")
            in_viewport = await is_locator_in_viewport(locator)
            return {"ok": True, "in_viewport": in_viewport, "matched": matched}
        if path == "/api/v1/page/scroll-into-view":
            page = await self._require_page()
            locator, matched = await self._resolve_locator_from_payload(page, body, "滚动到视口元素")
            in_viewport = await scroll_locator_into_view(locator, str(matched))
            if not in_viewport:
                raise ValueError(f"目标元素未能滚动到视口内: {matched}")
            return {"ok": True, "in_viewport": True, "matched": matched}
        if path == "/api/v1/page/click":
            page = await self._require_page()
            timeout = int(body.get("timeout", 10000))
            delay_before = float(body.get("delay_before", 0.5))
            element_ref = str(body.get("element_ref") or "").strip()
            if element_ref:
                entry = ELEMENT_REFS.get(element_ref)
                if entry is None:
                    raise ValueError("element_ref not found")
                container = entry.locator
                element_spec = parse_element_locator_spec(body.get("element"))
                if not element_spec.target_classes:
                    raise ValueError("element.target_classes is required")
                locator, _matched_parent, matched_target = await locate_element_by_spec(container, element_spec, "点击目标元素")
            else:
                element_spec = parse_element_locator_spec(body.get("element"))
                if not element_spec.target_classes:
                    raise ValueError("element.target_classes is required")
                locator, _matched_parent, matched_target = await locate_element_by_spec(page, element_spec, "点击目标元素")
            if not await locator.is_visible(timeout=timeout):
                raise ValueError(f"点击目标元素不可见: {matched_target}")
            in_viewport_before = await is_locator_in_viewport(locator)
            box_before = await locator.bounding_box()
            probe_before = await self._probe_click_state(locator)
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
            await self._safe_random_click(page, locator, matched_target)
            clicked = True
            return {"ok": True, "clicked": clicked}
        if path == "/api/v1/page/press-key":
            page = await self._require_page()
            key = str(body.get("key") or "").strip()
            if not key:
                raise ValueError("key is required")
            await page.keyboard.press(key)
            return {"ok": True, "key": key}
        if path == "/api/v1/page/type-text":
            page = await self._require_page()
            text = str(body.get("text") or "")
            if not text:
                raise ValueError("text is required")
            result = await human_type_focused(
                page,
                text,
                chunk_min=self._parse_int(body.get("chunk_min"), 1),
                chunk_max=self._parse_int(body.get("chunk_max"), 2),
                delay_min_ms=self._parse_int(body.get("delay_min_ms"), 80),
                delay_max_ms=self._parse_int(body.get("delay_max_ms"), 220),
            )
            return {"ok": True, **result}
        if path == "/api/v1/sound/play":
            kind = str(body.get("kind") or "").strip().lower()
            url = str(body.get("url") or "").strip()
            if not kind and not url:
                raise ValueError("kind or url is required")
            audio = await ensure_audio_from_url(url) if url else resolve_builtin_audio(kind)
            play_once(audio)
            return {"ok": True, "file": str(audio)}
        if path == "/api/v1/cookies/decrypt":
            encrypted_data = str(body.get("encrypted_data") or "").strip()
            encrypted_keys = body.get("encrypted_keys")
            if not encrypted_data or not isinstance(encrypted_keys, dict):
                raise ValueError("encrypted_data and encrypted_keys are required")
            cookies = decrypt_cookie_payload(
                CRYPTO_KEYS["private_key"],
                cookie_machine_ids(MACHINE),
                encrypted_data,
                encrypted_keys,
            )
            return {"ok": True, "cookies": cookies, "count": len(cookies)}
        raise ValueError(f"unsupported local path: {path}")

    async def _require_page(self):
        """
        读取当前默认页面。

        Returns:
            返回 Playwright Page 实例。
        """
        page = await self.browser_manager.ensure_page("default")
        if page is None:
            raise RuntimeError("浏览器未启动")
        return page

    def _parse_field_requests(self, raw: Any) -> list[tuple[str, Any]]:
        if not isinstance(raw, list) or not raw:
            raise ValueError("fields must be a non-empty list")
        requests: list[tuple[str, Any]] = []
        for item in raw:
            if not isinstance(item, dict) or len(item) != 1:
                raise ValueError("each field item must contain exactly one field name")
            field_name, spec = next(iter(item.items()))
            field = str(field_name).strip()
            if not field:
                raise ValueError("field name is required")
            requests.append((field, spec))
        return requests

    def _parse_int(self, raw: Any, default: int) -> int:
        """
        将云端下发的数字参数转换为整数。

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

    def _make_fast_field_spec(self, spec_raw: Any) -> Any:
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

    async def _find_element_items(self, page: Any, spec: Any, visible_only: bool = True, field_requests: list[tuple[str, Any]] | None = None) -> list[dict[str, Any]]:
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
        items: list[dict[str, Any]] = []
        for index in range(count):
            locator = locators.nth(index)
            if visible_only:
                if not await is_locator_in_viewport(locator):
                    continue
            item = dict(ELEMENT_REFS.register(locator, index))
            if field_requests:
                item["fields"] = await self._extract_fields_from_container(locator, field_requests, f"元素[{index}]")
            items.append(item)
        return items

    async def _extract_fields_from_container(self, container: Any, field_requests: list[tuple[str, Any]], container_label: str = "元素") -> dict[str, str]:
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
        for field_name, selector in field_requests:
            field_start = time.perf_counter()
            matched_target = ""
            try:
                spec = self._make_fast_field_spec(selector)
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

    def _save_ocr_debug_screenshot(self, task_id: str, screenshot_bytes: bytes, label: str) -> str:
        """
        保存 OCR 合并截图到本地任务截图目录。

        Args:
            task_id: 云端任务 ID
            screenshot_bytes: PNG 图片字节
            label: 文件名标签

        Returns:
            保存后的相对路径；未保存时返回空字符串。
        """
        safe_task_id = str(task_id or "").strip()
        if not safe_task_id or not screenshot_bytes:
            return ""
        safe_label = "".join(ch if ch.isalnum() else "_" for ch in str(label or "detail"))[:40] or "detail"
        filename = f"ocr_detail_{safe_label}_{int(time.time() * 1000)}.png"
        path = screenshot_path(safe_task_id, filename)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(screenshot_bytes)
        return "screenshots/" + path.name

    async def _extract_text_from_locator(self, page: Any, locator: Any, mode: str, delay_before: float, task_id: str = "", label: str = "detail") -> str:
        """按模式从目标元素提取整段文本。"""
        total_start = time.perf_counter()
        if delay_before > 0:
            await asyncio.sleep(delay_before)
        if mode == "ocr":
            screenshot_start = time.perf_counter()
            screenshot_bytes = await screenshot_locator_full(page, locator, "detail-ocr")
            if screenshot_bytes is None:
                screenshot_bytes = await locator.screenshot(type="png")
            saved_path = self._save_ocr_debug_screenshot(task_id, screenshot_bytes, label)
            screenshot_ms = int((time.perf_counter() - screenshot_start) * 1000)
            logger.info("OCR 文本提取截图完成 bytes=%d 耗时=%dms 保存=%s", len(screenshot_bytes), screenshot_ms, saved_path or "未保存")
            text = (await ocr_image_async(screenshot_bytes)).strip()
            total_ms = int((time.perf_counter() - total_start) * 1000)
            logger.info("OCR 文本提取完成 总耗时=%dms 文本长度=%d", total_ms, len(text))
            return text
        text = (await locator.inner_text(timeout=3000)).strip()
        total_ms = int((time.perf_counter() - total_start) * 1000)
        logger.debug("DOM 文本提取完成 总耗时=%dms 文本长度=%d", total_ms, len(text))
        return text

    async def _probe_click_state(self, locator: Any) -> dict[str, Any]:
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

    async def _safe_random_click(self, page: Any, locator: Any, matched_target: str) -> None:
        """使用最新元素位置执行随机点点击，跳过底层 human click 的二次滚动定位。"""
        latest_probe = await self._probe_click_state(locator)
        latest_box = await locator.bounding_box()
        logger.info("快速点击前复查 target=%s probe=%s box=%s", matched_target, latest_probe, latest_box)
        if not latest_probe.get("ok"):
            raise ValueError(f"点击目标元素状态检查失败: {matched_target}, probe={latest_probe}")
        if not latest_probe.get("connected", False):
            raise ValueError(f"点击目标元素节点已失效: {matched_target}, probe={latest_probe}")
        if not latest_probe.get("inViewport", False):
            logger.info("点击目标不在视口内，准备滚动后重试 target=%s probe=%s", matched_target, latest_probe)
            if not await scroll_locator_into_view(locator, matched_target):
                latest_probe = await self._probe_click_state(locator)
                raise ValueError(f"点击目标元素不在视口内: {matched_target}, probe={latest_probe}")
            latest_probe = await self._probe_click_state(locator)
            latest_box = await locator.bounding_box()
            logger.info("点击目标滚动后复查 target=%s probe=%s box=%s", matched_target, latest_probe, latest_box)
            if not latest_probe.get("inViewport", False):
                raise ValueError(f"点击目标元素不在视口内: {matched_target}, probe={latest_probe}")
        if not latest_probe.get("centerHit", False):
            raise ValueError(f"点击目标元素中心点被遮挡: {matched_target}, probe={latest_probe}")
        if not latest_box or latest_box.get("width", 0) <= 0 or latest_box.get("height", 0) <= 0:
            raise ValueError(f"点击目标元素位置无效: {matched_target}, box={latest_box}")
        click_start = time.perf_counter()
        if not await click_box_random_point(page, latest_box, matched_target):
            raise ValueError(f"点击目标元素随机点失败: {matched_target}, box={latest_box}")
        elapsed_ms = int((time.perf_counter() - click_start) * 1000)
        logger.info("点击成功 target=%s phase=safe_random_click elapsed_ms=%d box=%s", matched_target, elapsed_ms, latest_box)

    async def _resolve_locator_from_payload(self, page: Any, body: dict, label: str):
        """按 element_ref 或 element 解析单个目标元素定位器。"""
        element_ref = str(body.get("element_ref") or "").strip()
        if element_ref:
            entry = ELEMENT_REFS.get(element_ref)
            if entry is None:
                raise ValueError("element_ref not found")
            return entry.locator, element_ref

        spec = parse_element_locator_spec(body.get("element"))
        if not spec.target_classes:
            raise ValueError("element.target_classes is required")
        locator, _matched_parent, matched_target = await locate_element_by_spec(page, spec, label)
        return locator, matched_target

    async def _capture_cookie(self, task_id: str, payload: dict) -> dict:
        """
        启动浏览器并打开招聘平台页面，供用户扫码登录。

        Args:
            task_id: cookie 捕获记录 ID。
            payload: 包含 platform_id 和 user_data_dir 的命令参数。

        Returns:
            返回打开页面的 URL 和浏览器状态。
        """
        platform_id = str(payload.get("platform_id") or "")
        user_data_dir = str(payload.get("user_data_dir") or task_id).strip()
        if not platform_id:
            raise ValueError("platform_id is required")
        url = _platform_entry(platform_id)
        if not url:
            raise ValueError(f"unsupported platform: {platform_id}")
        profile_dir = _profile_dir(user_data_dir)
        ELEMENT_REFS.clear()
        await self.browser_manager.start(persistent=True, user_data_dir=str(profile_dir), headless=False, humanize=True)
        page = await self.browser_manager.new_page("default")
        ok = await navigate_to_page(page, url, timeout=30000)
        if not ok:
            raise RuntimeError("页面导航失败")
        self.state.last_message = f"已打开 {platform_id} 登录页面"
        await self.send_with_reply("cookie.capture.status", task_id, {"status": "opened", "url": url})
        return {"status": "opened", "url": url}

    async def send_with_reply(self, msg_type: str, task_id: str, payload: dict) -> dict:
        """
        向云端发送消息并等待回复，失败时自动重试。

        Args:
            msg_type: 消息类型。
            task_id: 任务 ID。
            payload: 消息载荷。

        Returns:
            返回云端回复消息。
        """
        if not self._ws:
            raise RuntimeError("WebSocket 未连接")
        message_id = _message_id()
        message = {
            "message_id": message_id,
            "type": msg_type,
            "task_id": task_id,
            "ok": True,
            "payload": payload,
        }
        last_error: Exception | None = None
        for attempt in range(1, MAX_RETRIES + 1):
            message["attempt"] = attempt
            future = asyncio.get_running_loop().create_future()
            self._pending[message_id] = future
            try:
                logger.info("[任务WS] 发送消息 type=%s task=%s attempt=%d message_id=%s 摘要=%s", msg_type, task_id, attempt, message_id, _payload_summary(payload))
                await self._ws.send(json.dumps(message, ensure_ascii=False))
                reply = await asyncio.wait_for(future, timeout=REPLY_TIMEOUT_SECONDS)
                logger.info("[任务WS] 收到消息回复 type=%s task=%s attempt=%d message_id=%s ok=%s error=%s", msg_type, task_id, attempt, message_id, reply.get("ok"), reply.get("error", ""))
                if not reply.get("ok", False):
                    raise RuntimeError(str(reply.get("error") or "云端返回失败"))
                return reply
            except Exception as exc:
                self._pending.pop(message_id, None)
                last_error = exc
                logger.warning("[任务WS] 发送消息失败 type=%s task=%s attempt=%d message_id=%s err=%r", msg_type, task_id, attempt, message_id, exc)
        raise RuntimeError(str(last_error) if last_error else "消息发送失败")

    async def _send_reply(self, reply_to: str, msg_type: str, task_id: str, ok: bool, error: str, payload: dict) -> None:
        """
        回复云端下发的消息。

        Args:
            reply_to: 被回复的消息 ID。
            msg_type: 原消息类型。
            task_id: 任务 ID。
            ok: 是否执行成功。
            error: 错误信息。
            payload: 回复载荷。
        """
        if not reply_to or not self._ws:
            return
        reply_message = {
            "message_id": _message_id(),
            "reply_to": reply_to,
            "type": f"{msg_type}.reply",
            "task_id": task_id,
            "ok": ok,
            "error": error,
            "payload": payload,
        }
        logger.info("[任务WS] 发送回复 type=%s task=%s reply_to=%s ok=%s error=%s 摘要=%s", msg_type, task_id, reply_to, ok, error, _payload_summary(payload))
        await self._ws.send(json.dumps(reply_message, ensure_ascii=False))

    async def _send_nowait(self, msg_type: str, task_id: str, payload: dict) -> None:
        """发送无需等待回复的云端消息。"""
        if not self._ws:
            raise RuntimeError("WebSocket 未连接")
        message = {
            "message_id": _message_id(),
            "type": msg_type,
            "task_id": task_id,
            "ok": True,
            "payload": payload,
        }
        logger.info("[任务WS] 发送无需等待回复的消息 type=%s task=%s message_id=%s payload=%s", msg_type, task_id, message["message_id"], payload)
        await self._ws.send(json.dumps(message, ensure_ascii=False))

    def _url_with_token(self, cloud_ws_url: str, token: str) -> str:
        """
        将 token 追加到云端 WebSocket 地址。

        Args:
            cloud_ws_url: 原始 WebSocket 地址。
            token: 云端访问令牌。

        Returns:
            返回可直接连接的 WebSocket 地址。
        """
        sep = "&" if "?" in cloud_ws_url else "?"
        return f"{cloud_ws_url}{sep}token={quote(token)}"


def _profile_dir(name: str) -> Path:
    """
    计算本地浏览器 profile 目录。

    Args:
        name: profile 名称。

    Returns:
        返回 local-agent/cookies 下的安全路径。
    """
    safe_name = "".join(ch if ch.isalnum() or ch in "-_" else "_" for ch in name).strip("_") or "default"
    return data_dir().parent / "cookies" / safe_name


def _platform_entry(platform_id: str) -> str:
    """
    根据平台 ID 返回登录入口页面。

    Args:
        platform_id: 招聘平台 ID。

    Returns:
        返回平台入口 URL。
    """
    if platform_id == "boss":
        return "https://www.zhipin.com/web/chat/recommend"
    if platform_id == "zhaopin":
        return "https://rd6.zhaopin.com/app/recommend"
    return ""
