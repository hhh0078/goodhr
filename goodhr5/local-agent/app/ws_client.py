"""本文件负责 Local Agent 主动连接云端 WebSocket 并执行云端下发命令。"""

from __future__ import annotations

import asyncio
import json
import secrets
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from urllib.parse import quote

import websockets
import httpx

from app.humanize import navigate_to_page, scroll_to_load, wait_and_click
from app.paths import data_dir


REPLY_TIMEOUT_SECONDS = 8
MAX_RETRIES = 3


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
        await self._post_cloud_task(cloud_api_base, token, task_id, "stop")
        self._active_tasks.discard(task_id)
        if not self._active_tasks:
            await self.disconnect()
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
                async with websockets.connect(self.state.cloud_ws_url, ping_interval=20, ping_timeout=20) as ws:
                    self._ws = ws
                    self.state.connected = True
                    self.state.status = "已连接"
                    self.state.last_error = ""
                    await self.send_with_reply("agent.status", "", {"status": "online"})
                    async for raw in ws:
                        await self._handle_raw(raw)
                    self.state.connected = False
                    self.state.status = "重连中"
            except asyncio.CancelledError:
                break
            except Exception as exc:
                self.state.connected = False
                self.state.status = "重连中"
                self.state.last_error = str(exc)
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
            return True
        for _ in range(retries):
            await self.connect(cloud_ws_url, token)
            if await self._wait_connected(5):
                return True
            await self.disconnect()
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
            resp = await client.post(url, headers={"Authorization": f"Bearer {token}"})
        data = resp.json() if resp.content else {}
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
            return
        reply_to = str(message.get("reply_to") or "")
        if reply_to:
            future = self._pending.pop(reply_to, None)
            if future and not future.done():
                future.set_result(message)
            return

        message_id = str(message.get("message_id") or "")
        task_id = str(message.get("task_id") or "")
        msg_type = str(message.get("type") or "")
        try:
            payload = message.get("payload") or {}
            result = await self._execute_command(msg_type, task_id, payload)
            await self._send_reply(message_id, msg_type, task_id, True, "", result)
        except Exception as exc:
            await self._send_reply(message_id, msg_type, task_id, False, str(exc), {})

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
        if path == "/api/v1/browser/start":
            user_data_dir = str(body.get("user_data_dir") or "").strip()
            if user_data_dir:
                body["user_data_dir"] = str(_profile_dir(user_data_dir))
            await self.browser_manager.start(
                persistent=bool(body.get("persistent", False)),
                user_data_dir=body.get("user_data_dir"),
                headless=bool(body.get("headless", False)),
                humanize=bool(body.get("humanize", True)),
                proxy=str(body.get("proxy", "")),
            )
            return {"ok": True, "status": "started"}
        if path == "/api/v1/browser/stop":
            await self.browser_manager.stop()
            return {"ok": True, "status": "stopped"}
        if path == "/api/v1/page/open":
            page = await self.browser_manager.new_page("default")
            url = str(body.get("url") or "").strip()
            if not url:
                raise ValueError("url is required")
            ok = await navigate_to_page(page, url, timeout=int(body.get("timeout", 30000)))
            if not ok:
                raise RuntimeError("页面导航失败")
            return {"ok": True, "url": url, "title": await page.title()}
        if path == "/api/v1/page/scroll":
            page = await self._require_page()
            await scroll_to_load(
                page,
                scroll_delay_min=int(body.get("scroll_delay_min", 3)),
                scroll_delay_max=int(body.get("scroll_delay_max", 8)),
                max_scrolls=int(body.get("max_scrolls", 20)),
            )
            return {"ok": True}
        if path == "/api/v1/page/extract":
            return await self._extract_page(body)
        if path == "/api/v1/page/click":
            page = await self._require_page()
            selector = str(body.get("selector") or "").strip()
            if not selector:
                raise ValueError("selector is required")
            clicked = await wait_and_click(
                page,
                selector,
                timeout=int(body.get("timeout", 10000)),
                delay_before=float(body.get("delay_before", 0.5)),
            )
            return {"ok": True, "clicked": clicked}
        raise ValueError(f"unsupported local path: {path}")

    async def _require_page(self):
        """
        读取当前默认页面。

        Returns:
            返回 Playwright Page 实例。
        """
        page = await self.browser_manager.get_page("default")
        if page is None:
            raise RuntimeError("浏览器未启动")
        return page

    async def _extract_page(self, body: dict) -> dict:
        """
        按云端下发的选择器提取页面内容。

        Args:
            body: 包含 selectors、card_selector 和 mode 的请求体。

        Returns:
            返回字段或候选人列表。
        """
        page = await self._require_page()
        selectors = body.get("selectors") or {}
        if not isinstance(selectors, dict) or not selectors:
            raise ValueError("selectors must be a dict")
        mode = str(body.get("mode") or "single")
        card_selector = str(body.get("card_selector") or "").strip()
        if mode == "batch" and card_selector:
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
            candidates = await page.evaluate(js_code, card_selector, selectors)
            if not isinstance(candidates, list):
                candidates = []
            return {"ok": True, "candidates": candidates, "count": len(candidates)}
        fields = {}
        for field_name, selector in selectors.items():
            try:
                locator = page.locator(str(selector)).first
                fields[field_name] = await locator.inner_text(timeout=3000) if await locator.is_visible(timeout=3000) else ""
            except Exception:
                fields[field_name] = ""
        return {"ok": True, "fields": fields}

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
                await self._ws.send(json.dumps(message, ensure_ascii=False))
                reply = await asyncio.wait_for(future, timeout=REPLY_TIMEOUT_SECONDS)
                if not reply.get("ok", False):
                    raise RuntimeError(str(reply.get("error") or "云端返回失败"))
                return reply
            except Exception as exc:
                self._pending.pop(message_id, None)
                last_error = exc
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
        await self._ws.send(json.dumps({
            "message_id": _message_id(),
            "reply_to": reply_to,
            "type": f"{msg_type}.reply",
            "task_id": task_id,
            "ok": ok,
            "error": error,
            "payload": payload,
        }, ensure_ascii=False))

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
