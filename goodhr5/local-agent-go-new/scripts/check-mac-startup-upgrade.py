#!/usr/bin/env python3
"""文件作用：用真实 Mac 主程序、隔离旧浏览器和模拟云端验证绑定后自动换包，不触碰用户目录或招聘网站。"""
import argparse
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import select
import shutil
import signal
import socket
import subprocess
import threading
import time
import urllib.error
import urllib.request

TOKEN = "isolated-startup-upgrade-fixture"


def digest(path):
    """计算真实归档或配置文件的完整摘要，供自动换包及保留断言使用。"""
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def free_port():
    """取得一个本次临时使用的回环端口，不连接用户服务。"""
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def request(base, route, body=None):
    """只向明确的本次回环服务发送 JSON，错误也返回结构化内容供断言。"""
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + route, data=data, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            decoded = json.load(response)
            return response.status, decoded.get("data", decoded)
    except urllib.error.HTTPError as error:
        decoded = json.loads(error.read())
        return error.code, decoded


def wait_for(operation, timeout=90):
    """有界轮询实际状态，未达到断言时保留最后错误，不把超时当成通过。"""
    deadline, last_error = time.monotonic() + timeout, None
    while time.monotonic() < deadline:
        try:
            value = operation()
            if value:
                return value
        except (OSError, ValueError) as error:
            last_error = type(error).__name__
        time.sleep(0.2)
    raise AssertionError(f"本次隔离验收未在预算内完成，最后错误：{last_error}")


def profile_cookie(worker_entry, executable, profile, seed, node):
    """通过标准浏览器上下文写入或复核隔离登录 Cookie，不执行页面脚本。"""
    sdk = Path(worker_entry).resolve().parent.parent / "node_modules/playwright-core/index.mjs"
    script = """
      const {firefox} = await import(process.argv[1]);
      const context = await firefox.launchPersistentContext(process.argv[3], {
        executablePath: process.argv[2], headless: true,
      });
      try {
        if (process.argv[4] === 'seed') await context.addCookies([{
          name: 'goodhr_upgrade_fixture', value: 'preserved', domain: 'example.com',
          path: '/', secure: true, expires: Math.floor(Date.now()/1000)+86400,
        }]);
        const cookies = await context.cookies('https://example.com');
        if (!cookies.some(x => x.name === 'goodhr_upgrade_fixture' && x.value === 'preserved'))
          throw new Error('隔离登录 Cookie 未保留');
      } finally { await context.close(); }
    """
    subprocess.run([node, "--input-type=module", "-e", script,
                    sdk.as_uri(), str(executable), str(profile), "seed" if seed else "check"],
                   check=True, timeout=90, capture_output=True, text=True)


def main():
    """准备真实旧安装，暂停实际新包下载以检查门闩，再确认新 Worker 路径及重绑幂等。"""
    parser = argparse.ArgumentParser()
    for name in ("binary", "worker-entry", "old-archive", "new-archive", "root"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--intel", action="store_true")
    parser.add_argument("--node", default=shutil.which("node"))
    args = parser.parse_args()
    root = Path(args.root).resolve()
    assert "camoufox-upgrade" in root.parts and not root.exists()
    assert "/library/application support/goodhr/" not in (root.as_posix().lower() + "/")
    root.mkdir(parents=True)
    old_archive, new_archive = Path(args.old_archive).resolve(), Path(args.new_archive).resolve()
    old_sha, new_sha = digest(old_archive), digest(new_archive)
    assert old_sha != new_sha
    data = root / "data"
    old = data / "runtime/camoufox"
    old.mkdir(parents=True)
    subprocess.run(["/usr/bin/ditto", "-x", "-k", str(old_archive), str(old)], check=True)
    old_binary = old / "Camoufox.app/Contents/MacOS/camoufox"
    assert old_binary.is_file()
    (old / "version.json").write_text('{"version":"152.0.4","release":"beta.30"}')
    record = {"camoufox": {"version": "152.0.4-beta.30", "sha256": old_sha,
                          "url": "https://example.invalid/old.zip", "directory_path": "camoufox",
                          "executable_path": "camoufox/Camoufox.app/Contents/MacOS/camoufox"}}
    (data / "runtime/installed-components.json").write_text(json.dumps(record))
    profile = data / "profiles/preserved-login"
    profile.mkdir(parents=True)
    marker = profile / "login-preservation-fixture.txt"
    marker.write_text("existing-profile-must-survive")
    profile_cookie(args.worker_entry, old_binary, profile, True, args.node)
    marker_sha, executable_sha = digest(marker), digest(old_binary)
    begun, release = threading.Event(), threading.Event()
    calls = {"config": 0, "download": 0, "bind": 0}
    asset = {"version": "152.0.4-beta.30", "sha256": new_sha, "note": "隔离自动更新验收"}

    class Fixture(BaseHTTPRequestHandler):
        """只模拟已有云端协议和固定受测 ZIP，不代理外部请求或保存登录凭证。"""
        def log_message(self, *_arguments):
            """禁止默认日志记录请求参数。"""

        def reply(self, value):
            """返回既有协议 JSON。"""
            content = json.dumps(value).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(content)))
            self.end_headers()
            self.wfile.write(content)

        def do_POST(self):
            """确认绑定凭证仅用于当前请求，再返回当前设备。"""
            if self.path == "/api/browser-events":
                self.rfile.read(int(self.headers["Content-Length"]))
                self.reply({"ok": True})
                return
            assert self.path == "/api/agents/bind" and self.headers.get("Authorization") == "Bearer " + TOKEN
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            calls["bind"] += 1
            self.reply({"ok": True, "agent": {"machine_id": body["machine_id"], "bind_status": "bound"}})

        def do_GET(self):
            """按实际鉴权接口提供清单，真实归档始终从已发布的 HTTPS 地址下载。"""
            assert self.headers.get("Authorization") == "Bearer " + TOKEN
            if self.path == "/api/runtime/config":
                calls["config"] += 1
                self.reply({"ok": True, "config": {"runtime_components": {"camoufox": {
                    "darwin-x64" if args.intel else "mac": asset}}}})
            elif self.path == "/api/auth/me":
                self.reply({"user": {"id": "isolated-fixture-user", "email": "fixture@example.invalid"}})
            else:
                self.reply({"ok": True})

        def do_CONNECT(self):
            """仅透传两个公开站点的 TLS，暂停 oss2 下载，不解密或关闭证书检查。"""
            if self.path not in ("oss2.58it.cn:443", "example.com:443"):
                self.send_error(403)
                return
            if self.path == "oss2.58it.cn:443":
                calls["download"] += 1
                begun.set()
                if not release.wait(60):
                    return
            host = self.path.rsplit(":", 1)[0]
            with socket.create_connection((host, 443), timeout=30) as upstream:
                self.send_response(200, "Connection Established")
                self.end_headers()
                self.connection.settimeout(None)
                upstream.settimeout(None)
                streams = [self.connection, upstream]
                while True:
                    ready, _, _ = select.select(streams, [], [], 60)
                    if not ready:
                        return
                    for source in ready:
                        block = source.recv(1024 * 1024)
                        if not block:
                            return
                        (upstream if source is self.connection else self.connection).sendall(block)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    server.daemon_threads = True
    asset["url"] = "https://oss2.58it.cn/" + new_archive.name
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    agent_port, worker_port = free_port(), free_port()
    base = f"http://127.0.0.1:{agent_port}"
    env = dict(os.environ)
    for key in ("CAMOUFOX_BINARY_PATH", "CAMOUFOX_INSTALL_DIR", "GOODHR_NATIVE_INPUT_SAFETY", "https_proxy", "no_proxy"):
        env.pop(key, None)
    env.update({"GOODHR_AUTO_OPEN_CONSOLE": "false", "GOODHR_WORKER_PORT": str(worker_port),
                "GOODHR_CLOUD_API_BASE": f"http://127.0.0.1:{server.server_port}",
                "HTTPS_PROXY": f"http://127.0.0.1:{server.server_port}",
                "NO_PROXY": "localhost,127.0.0.1",
                "GOODHR_WORKER_ENTRY": str(Path(args.worker_entry).resolve()), "GOODHR_NODE_PATH": args.node})
    command = [str(Path(args.binary).resolve()), "--data-dir", str(data), "--port", str(agent_port)]
    if args.intel:
        command = ["/usr/bin/arch", "-x86_64", *command]
    report = {"passed": False, "intel_via_rosetta": args.intel, "old_sha256": old_sha, "new_sha256": new_sha,
              "node_arch": subprocess.check_output([args.node, "-p", "process.arch"], text=True).strip()}
    with (root / "agent.log").open("w") as log:
        process = subprocess.Popen(command, env=env, stdout=log, stderr=log, start_new_session=True)
        try:
            wait_for(lambda: request(base, "/health")[0] == 200)
            assert request(base, "/health")[1]["version"] == "6.2.12.0"
            code, blocked = request(base, "/api/v1/runtime/ensure", {})
            assert code == 503 and blocked["error"]["code"] == "RUNTIME_NOT_READY" and "修补包" in blocked["error"]["message"]
            assert not request(base, "/api/v1/runtime/status")[1]["camoufox_installed"]
            assert request(base, "/api/v1/session/bind", {"token": TOKEN})[0] == 200
            assert begun.wait(60), "旧包存在时没有自动发起真实下载"
            code, blocked = request(base, "/api/v1/runtime/ensure", {})
            assert code == 503 and blocked["error"]["code"] == "RUNTIME_NOT_READY"
            assert not request(base, "/api/v1/runtime/status")[1]["camoufox_installed"]
            release.set()
            status = wait_for(lambda: completed_status(base, new_sha), timeout=900)
            installed = status["installed_versions"]["camoufox"]
            assert installed["directory_path"] != "camoufox" and old_binary.exists()
            assert digest(old_binary) == executable_sha and digest(marker) == marker_sha
            new_binary = data / "runtime" / installed["executable_path"]
            profile_cookie(args.worker_entry, new_binary, profile, False, args.node)
            assert request(base, "/api/v1/runtime/ensure", {})[0] == 200
            worker_base = f"http://127.0.0.1:{worker_port}"
            runtime_status = request(worker_base, "/api/v1/runtime/status")[1]
            assert Path(runtime_status["binary_path"]).resolve() == new_binary.resolve()
            assert request(base, "/api/v1/worker/status")[1]["running"]
            for _restart in range(2):
                code, browser = request(worker_base, "/api/v1/browser/start", {
                    "user_data_dir": str(profile), "headless": False, "url": "https://example.com",
                    "wait_until": "domcontentloaded", "timeout_ms": 30000})
                assert code == 200 and browser["running"] and browser["current_url"].startswith("https://example.com"), browser
                cookies = request(worker_base, "/api/v1/cookies")[1]["cookies"]
                assert any(item["name"] == "goodhr_upgrade_fixture" and item["value"] == "preserved" for item in cookies)
                assert request(worker_base, "/api/v1/browser/stop", {})[0] == 200
            assert request(base, "/api/v1/session/bind", {"token": TOKEN})[0] == 200
            time.sleep(0.5)
            assert calls["download"] == 1 and calls["config"] == 1, "绑定重试不应重复下载或打断就绪会话"
            report.update({"passed": True, "old_directory_preserved": True, "profile_preserved": True,
                           "startup_gate_checked": True, "download_gate_checked": True,
                           "login_cookie_preserved": True, "worker_uses_new_binary": True,
                           "worker_cookie_preserved_after_restart": True,
                           "public_page_opened": browser["current_url"],
                           "installed": installed, "runtime_status": runtime_status, "calls": dict(calls)})
        finally:
            release.set()
            process.send_signal(signal.SIGTERM)
            try:
                process.wait(timeout=90)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=10)
                report["passed"] = False
                report["forced_cleanup"] = True
            server.shutdown()
            report["agent_exit_code"] = process.returncode
            # 只检查带本次唯一隔离路径的进程，不碰用户已有浏览器。
            remaining = []
            for _attempt in range(20):
                listing = subprocess.check_output(["ps", "-axo", "pid=,command="], text=True)
                remaining = [int(line.strip().split(None, 1)[0]) for line in listing.splitlines()
                             if str(root) in line and int(line.strip().split(None, 1)[0]) != os.getpid()]
                if not remaining:
                    break
                time.sleep(0.2)
            report["remaining_owned_pids"] = remaining
            if remaining:
                report["passed"] = False
                for pid in remaining:
                    try:
                        os.kill(pid, signal.SIGTERM)
                    except ProcessLookupError:
                        pass
            (root / "result.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))
    assert report["passed"] and report["agent_exit_code"] == 0
    print(json.dumps(report, ensure_ascii=False))


def completed_status(base, expected_sha):
    """只接受新包已登记、门闩已打开且安装结束的真实本地状态。"""
    code, status = request(base, "/api/v1/runtime/status")
    installed = status.get("installed_versions", {}).get("camoufox", {})
    return status if code == 200 and installed.get("sha256") == expected_sha and status.get("camoufox_installed") and not status.get("install_progress", {}).get("running") else None


if __name__ == "__main__":
    main()
