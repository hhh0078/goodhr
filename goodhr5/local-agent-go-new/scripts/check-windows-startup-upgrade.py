#!/usr/bin/env python3
"""文件作用：用原生 Windows 主程序、正式浏览器归档和模拟云端验证绑定自动换包、启动门闩与隔离登录资料保留。"""
import argparse
import ctypes
from ctypes import wintypes
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path, PurePosixPath, PureWindowsPath
import select
import shutil
import socket
import subprocess
import threading
import time
import traceback
import urllib.error
import urllib.request
import zipfile

TOKEN = "isolated-windows-startup-upgrade-fixture"
OLD_SHA = "ea52a02fb1cfb1813ef6a326bea03fb2b650c9774143d953a94a27bfc8f10072"
NEW_SHA = "c264719312c3344782a4e723511922d8ca872ebb27d8ea73e335ff35b1598bfa"
NEW_URL = "https://oss2.58it.cn/camoufox-152.0.4-beta.30-goodhr-window-r1-win.x86_64.zip"
RESOURCE = "chrome/juggler/content/TargetRegistry.js"


def digest(path):
    """分块计算完整 SHA256，确保实际使用的归档和保留文件与已公开版本一致。"""
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def request(base, route, body=None):
    """仅向本次回环接口发送既有 JSON 协议，保留错误响应供真实门闩断言。"""
    assert base.startswith("http://127.0.0.1:")
    content = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + route, data=content, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            value = json.load(response)
            return response.status, value.get("data", value)
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read())


def free_port():
    """取得独立回环端口，避免复用用户正在运行的本地程序或 Worker。"""
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def wait_for(operation, timeout=90):
    """有界等待真实状态，失败时保留最后错误，不把超时计作通过。"""
    deadline, last = time.monotonic() + timeout, None
    while time.monotonic() < deadline:
        try:
            value = operation()
            if value:
                return value
        except (OSError, ValueError) as error:
            last = str(error)
        time.sleep(0.2)
    raise AssertionError(f"隔离验收超过等待预算：{last}")


def process_snapshot():
    """只读获取 Windows 进程亲缘，不读取命令行或任何用户凭据。"""
    class ProcessEntry(ctypes.Structure):
        """保存 Windows 原生进程枚举结构。"""
        _fields_ = [("size", wintypes.DWORD), ("usage", wintypes.DWORD), ("pid", wintypes.DWORD),
                    ("heap", ctypes.c_size_t), ("module", wintypes.DWORD), ("threads", wintypes.DWORD),
                    ("parent", wintypes.DWORD), ("priority", wintypes.LONG), ("flags", wintypes.DWORD),
                    ("name", wintypes.WCHAR * 260)]
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.CreateToolhelp32Snapshot.argtypes = [wintypes.DWORD, wintypes.DWORD]
    kernel.CreateToolhelp32Snapshot.restype = wintypes.HANDLE
    kernel.Process32FirstW.argtypes = [wintypes.HANDLE, ctypes.POINTER(ProcessEntry)]
    kernel.Process32NextW.argtypes = [wintypes.HANDLE, ctypes.POINTER(ProcessEntry)]
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    handle = kernel.CreateToolhelp32Snapshot(2, 0)
    assert handle != ctypes.c_void_p(-1).value, "无法枚举本次验收进程"
    result, entry = {}, ProcessEntry()
    entry.size = ctypes.sizeof(entry)
    try:
        present = kernel.Process32FirstW(handle, ctypes.byref(entry))
        while present:
            result[entry.pid] = {"pid": entry.pid, "parent": entry.parent, "name": entry.name}
            present = kernel.Process32NextW(handle, ctypes.byref(entry))
    finally:
        kernel.CloseHandle(handle)
    return result


def process_identity(pid, terminate=False, expected=None):
    """同一原生句柄内核对创建时间和启动路径；只有精确匹配的本次进程才允许终止。"""
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
    kernel.OpenProcess.restype = wintypes.HANDLE
    kernel.GetProcessTimes.argtypes = [wintypes.HANDLE] + [ctypes.POINTER(wintypes.FILETIME)] * 4
    kernel.GetExitCodeProcess.argtypes = [wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)]
    kernel.QueryFullProcessImageNameW.argtypes = [wintypes.HANDLE, wintypes.DWORD, wintypes.LPWSTR, ctypes.POINTER(wintypes.DWORD)]
    kernel.TerminateProcess.argtypes = [wintypes.HANDLE, wintypes.UINT]
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    handle = kernel.OpenProcess(0x1000 | (1 if terminate else 0), False, pid)
    if not handle:
        return None
    try:
        creation, exit_time, system, user = (wintypes.FILETIME() for _ in range(4))
        exit_code = wintypes.DWORD()
        if not kernel.GetExitCodeProcess(handle, ctypes.byref(exit_code)) or exit_code.value != 259:
            return None
        path, size = ctypes.create_unicode_buffer(32768), wintypes.DWORD(32768)
        if not kernel.GetProcessTimes(handle, *[ctypes.byref(value) for value in (creation, exit_time, system, user)]):
            return None
        if not kernel.QueryFullProcessImageNameW(handle, 0, path, ctypes.byref(size)):
            return None
        value = {"pid": pid, "created": (creation.dwHighDateTime << 32) | creation.dwLowDateTime,
                 "executable": str(Path(path.value).resolve())}
        if terminate:
            assert pid > 4 and expected == value, "进程编号已复用或启动路径改变，拒绝终止"
            assert kernel.TerminateProcess(handle, 1), "无法清理本次残留进程"
        return value
    finally:
        kernel.CloseHandle(handle)


class OwnedProcesses:
    """持续记录本次子进程的创建时间、启动路径和亲缘，失败时也不扩大清理范围。"""
    def __init__(self):
        """启动只读监督线程，进程记录在测试结束后写入独立证据目录。"""
        self.identities, self.stopped = {}, threading.Event()
        self.lock = threading.RLock()
        self.thread = threading.Thread(target=self.watch, daemon=True)
        self.thread.start()

    def add(self, process):
        """登记刚创建的根进程，不把同名用户进程加入清理范围。"""
        value = process_identity(process.pid)
        assert value is not None, "新进程身份无法核对"
        with self.lock:
            self.identities[process.pid] = value

    def capture(self):
        """按已记录父 PID 与不早于父进程的创建时间发现本次子孙进程。"""
        snapshot = process_snapshot()
        with self.lock:
            changed = True
            while changed:
                changed = False
                for pid, entry in snapshot.items():
                    if pid in self.identities or entry["parent"] not in self.identities:
                        continue
                    if process_identity(entry["parent"]) != self.identities[entry["parent"]]:
                        continue
                    value = process_identity(pid)
                    if value and value["created"] >= self.identities[entry["parent"]]["created"]:
                        self.identities[pid] = value
                        changed = True

    def watch(self):
        """定期只读采样，避免浏览器父进程退出后丢失其仍存活的子进程。"""
        while not self.stopped.wait(0.2):
            self.capture()

    def finish(self):
        """最后复查精确身份，只清理本次残留；返回清理前后证据使残留不能被掩盖。"""
        self.stopped.set()
        self.thread.join(timeout=10)
        self.capture()
        with self.lock:
            records = list(self.identities.values())
        remaining = [value for value in records if process_identity(value["pid"]) == value]
        for value in sorted(remaining, key=lambda item: item["created"], reverse=True):
            process_identity(value["pid"], terminate=True, expected=value)
        time.sleep(0.3)
        after = [value["pid"] for value in records if process_identity(value["pid"]) == value]
        return {"captured": records, "remaining_before_forced_cleanup": remaining, "remaining_after_cleanup": after}


def profile_cookie(worker_entry, executable, profile, node, tracker, root):
    """通过标准浏览器上下文在真实旧浏览器 Profile 中设置测试 Cookie，不运行页面脚本。"""
    sdk = Path(worker_entry).resolve().parent.parent / "node_modules/playwright-core/index.mjs"
    source = """
      const {firefox} = await import(process.argv[1]);
      const context = await firefox.launchPersistentContext(process.argv[3], {
        executablePath: process.argv[2], headless: true,
      });
      try {
        await context.addCookies([{name:'goodhr_upgrade_fixture',value:'preserved',domain:'example.com',
          path:'/',secure:true,expires:Math.floor(Date.now()/1000)+86400}]);
        if (!(await context.cookies('https://example.com')).some(x=>x.name==='goodhr_upgrade_fixture'&&x.value==='preserved'))
          throw new Error('隔离测试 Cookie 未写入');
      } finally { await context.close(); }
    """
    with (root / "seed-cookie.log").open("w", encoding="utf-8") as log:
        process = subprocess.Popen([node, "--input-type=module", "-e", source, sdk.as_uri(), str(executable), str(profile)],
                                   stdout=log, stderr=log, creationflags=subprocess.CREATE_NO_WINDOW)
        tracker.add(process)
        assert process.wait(timeout=90) == 0, "旧浏览器未完成隔离 Cookie 准备"


def extract_old(source, destination):
    """将完整校验的官方 ZIP 展开到新隔离目录，拒绝重复、链接和跨目录成员。"""
    destination.mkdir(parents=True)
    with zipfile.ZipFile(source) as archive:
        names = archive.namelist()
        assert len(names) == len({name.casefold() for name in names}), "归档存在重名成员"
        for entry in archive.infolist():
            name = entry.filename
            assert "\\" not in name and not PurePosixPath(name).is_absolute() and not PureWindowsPath(name).drive
            assert ".." not in PurePosixPath(name).parts and (entry.external_attr >> 16) & 0o170000 != 0o120000
        archive.extractall(destination)


def check_gates(base, phase, report):
    """验证四个真实入口都被组件门闩阻止，并确认拒绝任务停在 Worker 预检而没有进入招聘流程。"""
    probes = [("/api/v1/runtime/ensure", {}, 503, "RUNTIME_NOT_READY"),
              ("/api/v1/worker/start", {}, 503, "WORKER_START_FAILED"),
              ("/api/v1/page/open", {"url": "https://example.com", "profile_id": "preserved-login", "mouse_speed": "standard"}, 503, "RUNTIME_NOT_READY"),
              ("/api/v1/tasks/start", {"task_id": "fixture-gate-" + phase, "position_id": "fixture-position",
                "task_type": "greeting", "token": TOKEN, "profile_id": "preserved-login", "headless": True}, 409, "TASK_START_FAILED")]
    results = []
    report[phase + "_gate_checks"] = results
    for route, body, expected_status, expected_code in probes:
        code, response = request(base, route, body)
        results.append({"route": route, "http_status": code, "response": response})
        assert code == expected_status and response["error"]["code"] == expected_code, results[-1]
        assert "修补包" in response["error"]["message"], results[-1]
        if route.endswith("tasks/start"):
            assert response["preflight"][-1]["name"] == "check_worker" and not response["preflight"][-1]["success"]
    assert not request(base, "/api/v1/runtime/status")[1]["camoufox_installed"]
    assert not request(base, "/api/v1/worker/status")[1]["running"]
    report[phase + "_gate_checks"] = results


def completed_status(base):
    """只接受正式 r1 摘要已登记、门闩放行且安装结束的真实状态。"""
    code, status = request(base, "/api/v1/runtime/status")
    installed = status.get("installed_versions", {}).get("camoufox", {})
    return status if code == 200 and installed.get("sha256") == NEW_SHA and status.get("camoufox_installed") and not status.get("install_progress", {}).get("running") else None


def exercise(args, root, report, tracker):
    """模拟已有云端协议，实际暂停公开 HTTPS 下载以验门闩，完成后复核正式 Worker 与两次公开页面重开。"""
    source, candidate = Path(args.old_archive).resolve(), Path(args.new_archive).resolve()
    assert digest(source) == OLD_SHA and source.stat().st_size == 493141328
    assert digest(candidate) == NEW_SHA and candidate.stat().st_size == 508905799
    data, profile = root / "data", root / "data/profiles/preserved-login"
    old = data / "runtime/camoufox"
    extract_old(source, old)
    old_binary = old / "camoufox.exe"
    assert old_binary.is_file()
    (old / "version.json").write_text('{"version":"152.0.4","release":"beta.30"}', encoding="utf-8")
    record = {"camoufox": {"version": "152.0.4-beta.30", "sha256": OLD_SHA, "url": "https://example.invalid/old.zip",
                          "directory_path": "camoufox", "executable_path": "camoufox/camoufox.exe"}}
    (data / "runtime/installed-components.json").write_text(json.dumps(record), encoding="utf-8")
    profile.mkdir(parents=True)
    marker = profile / "login-preservation-fixture.txt"
    marker.write_text("existing-isolated-profile-must-survive", encoding="utf-8")
    profile_cookie(args.worker_entry, old_binary, profile, args.node, tracker, root)
    old_sha, marker_sha = digest(old_binary), digest(marker)
    begun, release = threading.Event(), threading.Event()
    calls = {"config": 0, "download": 0, "bind": 0, "unexpected": []}
    asset = {"version": "152.0.4-beta.30", "sha256": NEW_SHA, "url": NEW_URL, "note": "隔离 Windows 自动换包验收"}

    class Fixture(BaseHTTPRequestHandler):
        """仅提供合成账号和岗位；代理只透传正式 oss2 的 TLS，不解密或关闭证书检查。"""
        def log_message(self, *_arguments):
            """关闭默认请求日志，避免凭据或请求参数落盘。"""

        def reply(self, content, status=200):
            """返回既有接口结构的合成 JSON，不代理真实账号或岗位数据。"""
            encoded = json.dumps(content).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)

        def do_POST(self):
            """模拟设备绑定及故障摘要上报，拒绝一切真正任务开始请求作为额外保险。"""
            content = self.rfile.read(int(self.headers.get("Content-Length", "0")))
            if self.path == "/api/browser-events":
                return self.reply({"ok": True})
            if self.path == "/api/fail-notice":
                return self.reply({"ok": True, "notice_sent": True})
            if self.path == "/api/agents/bind" and self.headers.get("Authorization") == "Bearer " + TOKEN:
                calls["bind"] += 1
                return self.reply({"ok": True, "agent": {"machine_id": json.loads(content)["machine_id"], "bind_status": "bound"}})
            calls["unexpected"].append("POST " + self.path)
            self.reply({"error": "隔离验收拒绝真正启动招聘任务"}, 403)

        def do_PUT(self):
            """只接收本次被门闩拒绝的合成岗位日志，不访问实际云端日志。"""
            self.rfile.read(int(self.headers.get("Content-Length", "0")))
            if self.path == "/api/positions/fixture-position/logs":
                return self.reply({"ok": True})
            calls["unexpected"].append("PUT " + self.path)
            self.reply({"error": "隔离验收没有此接口"}, 404)

        def do_GET(self):
            """为前置登录与岗位检查提供合法合成数据，组件清单始终指向已公开 Windows r1。"""
            if self.headers.get("Authorization") != "Bearer " + TOKEN:
                return self.reply({"error": "仅供隔离测试"}, 401)
            if self.path == "/api/runtime/config":
                calls["config"] += 1
                return self.reply({"ok": True, "config": {"runtime_components": {"camoufox": {"win": asset}}}})
            if self.path == "/api/auth/me":
                return self.reply({"user": {"id": "isolated-fixture-user", "email": "fixture@example.invalid"}})
            if self.path == "/api/config/user-preferences":
                return self.reply({"config": {"mouse_speed": "standard"}})
            if self.path == "/api/resume-library/settings":
                return self.reply({"settings": {"auto_import_downloads": False}})
            if self.path == "/api/positions/fixture-position":
                return self.reply({"position": {"id": "fixture-position", "platform_id": "boss", "profile_id": "preserved-login", "requires_ai": False, "requires_ocr": False,
                                                "common_config": {"mode_default": "keyword", "detail_mode": "none"}}})
            calls["unexpected"].append("GET " + self.path)
            self.reply({"error": "隔离验收没有此接口"}, 404)

        def do_CONNECT(self):
            """只允许公开浏览器归档和公开示例站点，暂停归档连接以验证更新期间无法开始。"""
            if self.path not in ("oss2.58it.cn:443", "example.com:443"):
                return self.send_error(403)
            if self.path == "oss2.58it.cn:443":
                calls["download"] += 1
                begun.set()
                if not release.wait(120):
                    return
            with socket.create_connection((self.path.rsplit(":", 1)[0], 443), timeout=30) as upstream:
                self.send_response(200, "Connection Established")
                self.end_headers()
                self.connection.settimeout(None)
                upstream.settimeout(None)
                streams = [self.connection, upstream]
                while True:
                    ready, _, _ = select.select(streams, [], [], 60)
                    if not ready:
                        return
                    for stream in ready:
                        try:
                            block = stream.recv(1024 * 1024)
                            if not block:
                                return
                            (upstream if stream is self.connection else self.connection).sendall(block)
                        except (ConnectionResetError, BrokenPipeError):
                            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    server.daemon_threads = True
    threading.Thread(target=server.serve_forever, daemon=True).start()
    agent_port, worker_port = free_port(), free_port()
    base, worker_base = f"http://127.0.0.1:{agent_port}", f"http://127.0.0.1:{worker_port}"
    env = dict(os.environ)
    for key in ("CAMOUFOX_BINARY_PATH", "CAMOUFOX_INSTALL_DIR", "GOODHR_NATIVE_INPUT_SAFETY", "https_proxy", "no_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"):
        env.pop(key, None)
    env.update({"GOODHR_AUTO_OPEN_CONSOLE": "false", "GOODHR_WORKER_PORT": str(worker_port),
                "GOODHR_CLOUD_API_BASE": f"http://127.0.0.1:{server.server_port}",
                "HTTPS_PROXY": f"http://127.0.0.1:{server.server_port}", "NO_PROXY": "localhost,127.0.0.1",
                "GOODHR_WORKER_ENTRY": str(Path(args.worker_entry).resolve()), "GOODHR_NODE_PATH": args.node})
    report.update({"old_sha256": OLD_SHA, "new_sha256": NEW_SHA, "download_url": NEW_URL,
                   "agent_port": agent_port, "worker_port": worker_port, "headless": True,
                   "binary_sha256": digest(args.binary), "node": args.node})
    with (root / "agent.log").open("w", encoding="utf-8") as log:
        process = subprocess.Popen([str(Path(args.binary).resolve()), "--data-dir", str(data), "--port", str(agent_port)],
                                   env=env, stdout=log, stderr=log, creationflags=subprocess.CREATE_NO_WINDOW)
        tracker.add(process)
        report["agent_pid"] = process.pid
        try:
            wait_for(lambda: request(base, "/health")[0] == 200)
            assert request(base, "/health")[1]["version"] == args.version
            check_gates(base, "before_bind", report)
            assert request(base, "/api/v1/session/bind", {"token": TOKEN})[0] == 200
            assert begun.wait(60), "已有旧包时没有自动发起公开 r1 下载"
            check_gates(base, "during_download", report)
            release.set()
            status = wait_for(lambda: completed_status(base), timeout=1200)
            installed = status["installed_versions"]["camoufox"]
            assert installed["directory_path"] != "camoufox" and old_binary.exists()
            assert digest(old_binary) == old_sha and digest(marker) == marker_sha
            new_binary = data / "runtime" / installed["executable_path"]
            with zipfile.ZipFile(new_binary.parent / "omni.ja") as archive:
                resource = archive.read(RESOURCE).decode()
                assert "forceAppWindowActive = true" in resource and "GoodHR window rendering backport v1" in resource
            assert request(base, "/api/v1/runtime/ensure", {})[0] == 200
            runtime = request(worker_base, "/api/v1/runtime/status")[1]
            assert Path(runtime["binary_path"]).resolve() == new_binary.resolve()
            pages = []
            for _restart in range(2):
                code, browser = request(worker_base, "/api/v1/browser/start", {"user_data_dir": str(profile),
                    "headless": True, "url": "https://example.com", "wait_until": "domcontentloaded", "timeout_ms": 60000})
                assert code == 200 and browser["running"] and browser["current_url"].startswith("https://example.com"), browser
                cookies = request(worker_base, "/api/v1/cookies")[1]["cookies"]
                assert any(item["name"] == "goodhr_upgrade_fixture" and item["value"] == "preserved" for item in cookies)
                pages.append(browser["current_url"])
                assert request(worker_base, "/api/v1/browser/stop", {})[0] == 200
            assert request(base, "/api/v1/session/bind", {"token": TOKEN})[0] == 200
            time.sleep(0.5)
            report.update({"installed": installed, "runtime_status": runtime,
                           "old_directory_preserved": True, "profile_preserved": True, "login_cookie_preserved_twice": True,
                           "worker_uses_new_binary": True, "prepared_resource_patch_preserved": True, "public_pages": pages,
                           "repeat_bind_no_second_download": True})
            assert calls["config"] == 1 and calls["download"] == 1 and not calls["unexpected"], calls
            report["passed"] = True
        finally:
            release.set()
            report["calls"] = dict(calls)
            for route in ("/api/v1/browser/stop", "/api/v1/worker/stop"):
                try:
                    report["cleanup_" + route.rsplit("/", 2)[1]] = request(base, route, {})[0]
                except (OSError, ValueError) as error:
                    report["cleanup_error"] = str(error)
            tracker.capture()
            if process.poll() is None:
                process.terminate()
            process.wait(timeout=15)
            report["agent_exit_code"] = process.returncode
            report["agent_stopped_by_exact_pid"] = True
            server.shutdown()
            server.server_close()


def main():
    """要求全新 camoufox-upgrade 隔离目录，失败也保存现场和精确进程清理结果。"""
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "worker-entry", "old-archive", "new-archive", "root"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--node", default=shutil.which("node"))
    parser.add_argument("--version", default="6.2.12.1")
    parser.add_argument("--expect-old-behavior", action="store_true")
    args = parser.parse_args()
    root = Path(args.root).resolve()
    assert os.name == "nt" and "camoufox-upgrade" in [part.lower() for part in root.parts] and not root.exists()
    assert not any(part in root.as_posix().lower() for part in ("/appdata/local/goodhr/", "/appdata/roaming/goodhr/"))
    assert args.node and Path(args.node).is_file()
    root.mkdir(parents=True)
    report, tracker = {"passed": False, "platform": "windows/amd64", "version": args.version, "root": str(root)}, OwnedProcesses()
    try:
        exercise(args, root, report, tracker)
    except BaseException as error:
        report["passed"] = False
        report["error"] = str(error).replace(TOKEN, "[隔离凭据]")
        (root / "failure.log").write_text(traceback.format_exc().replace(TOKEN, "[隔离凭据]"), encoding="utf-8")
    finally:
        report["process_cleanup"] = tracker.finish()
        if report["process_cleanup"]["remaining_before_forced_cleanup"] or report["process_cleanup"]["remaining_after_cleanup"]:
            report["passed"] = False
        if args.expect_old_behavior:
            first = report.get("before_bind_gate_checks", [{}])[0]
            report["expected_old_behavior_reproduced"] = (not report["passed"] and first.get("route") == "/api/v1/runtime/ensure"
                and first.get("http_status") == 200 and first.get("response", {}).get("ready") is True
                and not report["process_cleanup"]["remaining_after_cleanup"])
        (root / "result.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False))
    assert report.get("expected_old_behavior_reproduced") if args.expect_old_behavior else report["passed"], report.get("error", "本次验收有进程残留")


if __name__ == "__main__":
    main()
