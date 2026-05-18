"""本文件负责启动 GoodHR 5 Local Agent HTTP 服务并注册本地 API。"""

from __future__ import annotations

import json
import os
from collections.abc import Iterable
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

from app.machine import load_machine
from app.profiles import create_profile, delete_profile, list_profiles
from app.session import load_cloud_account, save_cloud_account
from app.tasks import delete_candidate, init_task, load_candidates, save_candidate


HOST = "127.0.0.1"
DEFAULT_PORTS = range(9001, 9010)
MACHINE = load_machine()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        """处理 Local Agent 的 GET 请求。"""
        if self.path == "/health":
            account = load_cloud_account()
            self._json(
                {
                    "ok": True,
                    "name": "GoodHR 5 Local Agent",
                    "version": "0.1.0",
                    "port": self.server.server_address[1],
                    "machine_id": MACHINE["machine_id"],
                    "bound_cloud_user_id": account["cloud_user_id"] if account else "",
                }
            )
            return

        if self.path.startswith("/api/v1/profiles"):
            self._list_profiles()
            return
        if self.path.startswith("/api/v1/tasks/") and self.path.endswith("/candidates"):
            self._load_candidates()
            return

        self.send_error(404)

    def do_POST(self) -> None:
        """处理 Local Agent 的 POST 请求。"""
        if self.path == "/api/v1/session/bind-cloud-user":
            self._bind_cloud_user()
            return
        if self.path == "/api/v1/profiles":
            self._create_profile()
            return
        if self.path == "/api/v1/tasks/init":
            self._init_task()
            return
        if self.path.startswith("/api/v1/tasks/") and self.path.endswith("/candidates"):
            self._save_candidate()
            return

        self.send_error(404)

    def do_DELETE(self) -> None:
        """处理 Local Agent 的 DELETE 请求。"""
        if self.path.startswith("/api/v1/profiles/"):
            self._delete_profile()
            return
        if self.path.startswith("/api/v1/tasks/") and "/candidates/" in self.path:
            self._delete_candidate()
            return

        self.send_error(404)

    def do_OPTIONS(self) -> None:
        """处理跨域预检请求。"""
        self.send_response(204)
        self._cors_headers()
        self.end_headers()

    def log_message(self, fmt: str, *args: object) -> None:
        """关闭默认访问日志，避免轮询接口刷屏。"""
        return

    def _json(self, payload: dict) -> None:
        """返回 JSON 响应。"""
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(200)
        self._cors_headers()
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _cors_headers(self) -> None:
        """写入允许云端页面访问 Local Agent 的 CORS 响应头。"""
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-GoodHR-Local-Token")
        self.send_header("Access-Control-Allow-Private-Network", "true")

    def _bind_cloud_user(self) -> None:
        """绑定当前 Local Agent 对应的云端账号。"""
        try:
            payload = self._read_json()
        except ValueError as exc:
            self._error(400, str(exc))
            return

        cloud_user_id = str(payload.get("cloud_user_id", "")).strip()
        cloud_email = str(payload.get("cloud_email", "")).strip().lower()
        agent_token = str(payload.get("agent_token", "")).strip()

        if not cloud_user_id:
            self._error(400, "cloud_user_id is required")
            return
        if not cloud_email:
            self._error(400, "cloud_email is required")
            return
        if not agent_token:
            self._error(400, "agent_token is required")
            return

        account = save_cloud_account(cloud_user_id, cloud_email, agent_token)
        self._json(
            {
                "ok": True,
                "machine_id": MACHINE["machine_id"],
                "cloud_user_id": account["cloud_user_id"],
                "cloud_email": account["cloud_email"],
                "bound_at": account["bound_at"],
            }
        )

    def _list_profiles(self) -> None:
        """返回本地 profile 元数据列表。"""
        query = parse_qs(urlparse(self.path).query)
        platform_id = query.get("platform_id", [""])[0]

        # 调用 profiles 模块读取本地 profile 元数据，用于云端选择平台账号。
        profiles = list_profiles(platform_id)
        self._json({"ok": True, "profiles": profiles})

    def _create_profile(self) -> None:
        """创建本地 profile 元数据。"""
        try:
            payload = self._read_json()
            # 调用 profiles 模块创建 profile 元数据，真实 cookie 仍由浏览器 profile 保存。
            profile = create_profile(
                str(payload.get("platform_id", "")),
                str(payload.get("display_name", "")),
            )
        except ValueError as exc:
            self._error(400, str(exc))
            return

        self._json({"ok": True, "profile": profile})

    def _delete_profile(self) -> None:
        """删除本地 profile 元数据。"""
        profile_id = self.path.removeprefix("/api/v1/profiles/").strip()
        if not profile_id:
            self._error(400, "profile id is required")
            return

        # 调用 profiles 模块删除 profile 元数据；是否清理浏览器 profile 文件后续单独实现。
        deleted = delete_profile(profile_id)
        if not deleted:
            self._error(404, "profile not found")
            return

        self._json({"ok": True})

    def _init_task(self) -> None:
        """初始化本地任务目录。"""
        try:
            payload = self._read_json()
            # 调用 tasks 模块初始化任务目录，用于保存本地 candidates.json、截图和 OCR。
            task = init_task(
                str(payload.get("task_id", "")),
                str(payload.get("cloud_user_id", "")),
                str(payload.get("platform_id", "")),
                str(payload.get("platform_account_id", "")),
            )
        except ValueError as exc:
            self._error(400, str(exc))
            return

        self._json({"ok": True, "task": task})

    def _load_candidates(self) -> None:
        """读取本地任务候选人 JSON。"""
        task_id = self._task_id_from_path("/candidates")
        try:
            # 调用 tasks 模块读取 candidates.json，供云端页面渲染候选人卡片。
            data = load_candidates(task_id)
        except FileNotFoundError:
            self._error(404, "task candidates not found")
            return

        self._json({"ok": True, "data": data})

    def _save_candidate(self) -> None:
        """新增或更新本地候选人记录。"""
        task_id = self._task_id_from_path("/candidates")
        try:
            payload = self._read_json()
            # 调用 tasks 模块写入候选人记录，候选人详情只保存在本地 JSON。
            candidate = save_candidate(task_id, payload)
        except FileNotFoundError:
            self._error(404, "task candidates not found")
            return

        self._json({"ok": True, "candidate": candidate})

    def _delete_candidate(self) -> None:
        """删除本地候选人记录。"""
        task_id = self._task_id_from_path("")
        candidate_id = self.path.rsplit("/", 1)[-1]
        try:
            # 调用 tasks 模块删除候选人记录，用于云端页面管理本地 JSON。
            deleted = delete_candidate(task_id, candidate_id)
        except FileNotFoundError:
            self._error(404, "task candidates not found")
            return
        if not deleted:
            self._error(404, "candidate not found")
            return

        self._json({"ok": True})

    def _task_id_from_path(self, suffix: str) -> str:
        """从任务 API 路径中解析 task_id。"""
        value = self.path.removeprefix("/api/v1/tasks/")
        if suffix and value.endswith(suffix):
            value = value[: -len(suffix)]
        if "/candidates/" in value:
            value = value.split("/candidates/", 1)[0]
        return value.strip("/")

    def _read_json(self) -> dict:
        """读取请求体中的 JSON 对象。"""
        length = int(self.headers.get("Content-Length", "0") or "0")
        if length <= 0:
            raise ValueError("json body is required")

        raw = self.rfile.read(length)
        try:
            payload = json.loads(raw.decode("utf-8"))
        except json.JSONDecodeError as exc:
            raise ValueError("invalid json body") from exc

        if not isinstance(payload, dict):
            raise ValueError("json body must be an object")
        return payload

    def _error(self, status: int, message: str) -> None:
        """按统一格式返回错误响应。"""
        body = json.dumps({"ok": False, "error": message}, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self._cors_headers()
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main() -> None:
    """启动 Local Agent HTTP 服务。"""
    server = create_server()
    host, port = server.server_address
    print(f"GoodHR 5 Local Agent listening on http://{host}:{port}")
    server.serve_forever()


def create_server() -> ThreadingHTTPServer:
    """创建 HTTP 服务并自动选择可用端口。"""
    errors: list[str] = []
    for port in candidate_ports():
        try:
            return ThreadingHTTPServer((HOST, port), Handler)
        except OSError as exc:
            errors.append(f"{port}: {exc}")

    detail = "; ".join(errors)
    raise RuntimeError(f"No available GoodHR Local Agent port in 9001-9009. {detail}")


def candidate_ports() -> Iterable[int]:
    """返回 Local Agent 应尝试监听的端口列表。"""
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


if __name__ == "__main__":
    main()
