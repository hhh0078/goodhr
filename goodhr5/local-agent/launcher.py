"""GoodHR Local Agent 桌面启动器。

本文件提供双击运行时的小窗口，用于启动/停止 Local Agent、查看日志、
清理日志，并打开 GoodHR 官网。打包为 macOS app 或 Windows exe 时，
该文件作为图形界面入口。
"""

from __future__ import annotations

import os
import platform
import subprocess
import sys
import threading
import time
import tkinter as tk
import urllib.error
import urllib.request
import webbrowser
from pathlib import Path
from tkinter import messagebox, scrolledtext


APP_NAME = "GoodHRLocalAgent"
OFFICIAL_SITE_URL = "https://goodhr.58it.cn/"
HOST = "127.0.0.1"
PORTS = range(9001, 9010)


def app_support_dir() -> Path:
    """
    获取 Local Agent 运行数据目录。

    Returns:
        Path: 当前系统对应的应用数据目录。
    """
    system = platform.system().lower()
    if system == "darwin":
        return Path.home() / "Library" / "Application Support" / APP_NAME
    if system == "windows":
        appdata = os.getenv("APPDATA") or str(Path.home() / "AppData" / "Roaming")
        return Path(appdata) / APP_NAME
    return Path.home() / f".{APP_NAME.lower()}"


def bundle_root() -> Path:
    """
    获取程序资源根目录。

    PyInstaller 打包后资源位于 sys._MEIPASS；源码运行时资源位于 local-agent 目录。

    Returns:
        Path: 程序资源根目录。
    """
    frozen_root = getattr(sys, "_MEIPASS", "")
    if frozen_root:
        return Path(frozen_root)
    return Path(__file__).resolve().parent


def ensure_runtime_dirs(base_dir: Path) -> dict[str, Path]:
    """
    创建运行所需目录。

    Args:
        base_dir: 应用数据根目录。

    Returns:
        dict[str, Path]: 常用目录路径。
    """
    dirs = {
        "base": base_dir,
        "agent_data": base_dir / "agent_data",
        "config": base_dir / "config",
        "cookies": base_dir / "cookies",
        "logs": base_dir / "logs",
        "profiles": base_dir / "profiles",
        "tasks": base_dir / "tasks",
        "screenshots": base_dir / "screenshots",
    }
    for path in dirs.values():
        path.mkdir(parents=True, exist_ok=True)
    return dirs


def find_cloakbrowser_binary(root: Path) -> Path | None:
    """
    查找包内 CloakBrowser 浏览器可执行文件。

    Args:
        root: 程序资源根目录。

    Returns:
        Path | None: 找到的浏览器可执行文件；找不到时返回 None。
    """
    vendor_dir = root / "vendor" / "cloakbrowser"
    if not vendor_dir.exists():
        return None

    system = platform.system().lower()
    if system == "darwin":
        candidates = list(vendor_dir.glob("**/Chromium.app/Contents/MacOS/Chromium"))
    elif system == "windows":
        candidates = list(vendor_dir.glob("**/chrome.exe")) + list(vendor_dir.glob("**/chromium.exe"))
    else:
        candidates = list(vendor_dir.glob("**/chrome")) + list(vendor_dir.glob("**/chromium"))

    for candidate in candidates:
        if candidate.exists():
            return candidate
    return None


class GoodHRLauncher:
    """GoodHR Local Agent 图形启动器。"""

    def __init__(self) -> None:
        """初始化窗口、运行目录和状态。"""
        self.root = tk.Tk()
        self.root.title("GoodHR Local Agent")
        self.root.geometry("780x560")
        self.root.minsize(680, 460)

        self.base_dir = app_support_dir()
        self.dirs = ensure_runtime_dirs(self.base_dir)
        self.log_file = self.dirs["logs"] / "agent.log"
        self.process: subprocess.Popen[str] | None = None
        self.log_offset = 0
        self.running_port: int | None = None

        self.status_var = tk.StringVar(value="准备启动")
        self.detail_var = tk.StringVar(value=f"数据目录：{self.base_dir}")

        self._build_ui()
        self._clear_log_file()
        self._start_agent()
        self._schedule_refresh()
        self.root.protocol("WM_DELETE_WINDOW", self._on_close)

    def _build_ui(self) -> None:
        """创建窗口组件。"""
        wrapper = tk.Frame(self.root, padx=14, pady=12)
        wrapper.pack(fill=tk.BOTH, expand=True)

        title = tk.Label(wrapper, text="GoodHR 本地执行器", font=("Arial", 18, "bold"))
        title.pack(anchor="w")

        desc = tk.Label(
            wrapper,
            text="本程序负责启动本地浏览器、执行平台页面操作、截图 OCR 和任务数据保存。",
            anchor="w",
        )
        desc.pack(anchor="w", pady=(6, 10))

        status_row = tk.Frame(wrapper)
        status_row.pack(fill=tk.X)
        tk.Label(status_row, text="当前状态：", font=("Arial", 12, "bold")).pack(side=tk.LEFT)
        tk.Label(status_row, textvariable=self.status_var, fg="#166534", font=("Arial", 12, "bold")).pack(side=tk.LEFT)

        tk.Label(wrapper, textvariable=self.detail_var, anchor="w", fg="#555").pack(fill=tk.X, pady=(6, 10))

        button_row = tk.Frame(wrapper)
        button_row.pack(fill=tk.X, pady=(0, 10))
        tk.Button(button_row, text="打开官网", command=self._open_site, width=12).pack(side=tk.LEFT, padx=(0, 8))
        tk.Button(button_row, text="停止服务", command=self._stop_agent, width=12).pack(side=tk.LEFT, padx=(0, 8))
        tk.Button(button_row, text="清除日志", command=self._clear_logs, width=12).pack(side=tk.LEFT, padx=(0, 8))
        tk.Button(button_row, text="重新启动", command=self._restart_agent, width=12).pack(side=tk.LEFT)

        tk.Label(wrapper, text="运行日志", font=("Arial", 12, "bold")).pack(anchor="w")
        self.log_view = scrolledtext.ScrolledText(wrapper, height=20, wrap=tk.WORD, state=tk.DISABLED)
        self.log_view.pack(fill=tk.BOTH, expand=True, pady=(6, 0))

    def _clear_log_file(self) -> None:
        """清空本次运行日志文件。"""
        self.log_file.parent.mkdir(parents=True, exist_ok=True)
        self.log_file.write_text("", encoding="utf-8")
        self.log_offset = 0

    def _append_log(self, text: str) -> None:
        """
        向日志窗口追加文本。

        Args:
            text: 要追加的日志内容。
        """
        if not text:
            return
        self.log_view.configure(state=tk.NORMAL)
        self.log_view.insert(tk.END, text)
        self.log_view.see(tk.END)
        self.log_view.configure(state=tk.DISABLED)

    def _start_agent(self) -> None:
        """启动 Local Agent 子进程。"""
        if self.process and self.process.poll() is None:
            self.status_var.set("运行中")
            return

        browser_binary = find_cloakbrowser_binary(bundle_root())
        if browser_binary is None:
            self.status_var.set("缺少 CloakBrowser")
            self.detail_var.set("未找到包内 CloakBrowser，请确认打包时已包含 vendor/cloakbrowser。")
            self._append_log("未找到包内 CloakBrowser，Local Agent 未启动。\n")
            return

        env = os.environ.copy()
        env["GOODHR_AGENT_DATA_DIR"] = str(self.dirs["agent_data"])
        env["GOODHR_AGENT_LOG_FILE"] = str(self.log_file)
        env["CLOAKBROWSER_BINARY_PATH"] = str(browser_binary)

        command = [sys.executable, str(Path(__file__).resolve()), "--agent-server"]
        self.status_var.set("启动中")
        self.detail_var.set(f"CloakBrowser：{browser_binary}")
        self._append_log(f"正在启动 Local Agent...\n数据目录：{self.base_dir}\n浏览器：{browser_binary}\n")

        self.process = subprocess.Popen(
            command,
            cwd=str(bundle_root()),
            env=env,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            text=True,
        )

    def _stop_agent(self) -> None:
        """停止 Local Agent 子进程。"""
        if not self.process or self.process.poll() is not None:
            self.status_var.set("已停止")
            return

        self.status_var.set("正在停止")
        self.process.terminate()
        try:
            self.process.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait(timeout=5)
        self.status_var.set("已停止")
        self._append_log("Local Agent 已停止。\n")

    def _restart_agent(self) -> None:
        """重新启动 Local Agent。"""
        self._stop_agent()
        time.sleep(0.2)
        self._start_agent()

    def _clear_logs(self) -> None:
        """清空日志窗口和日志文件。"""
        self._clear_log_file()
        self.log_view.configure(state=tk.NORMAL)
        self.log_view.delete("1.0", tk.END)
        self.log_view.configure(state=tk.DISABLED)

    def _open_site(self) -> None:
        """使用默认浏览器打开 GoodHR 官网。"""
        webbrowser.open(OFFICIAL_SITE_URL)

    def _refresh_status(self) -> None:
        """刷新子进程状态和健康检查状态。"""
        if self.process and self.process.poll() is not None:
            self.status_var.set("已停止")
            return

        port = self._detect_running_port()
        if port:
            self.running_port = port
            self.status_var.set("运行中")
            self.detail_var.set(f"服务地址：http://{HOST}:{port}    数据目录：{self.base_dir}")
        elif self.process and self.process.poll() is None:
            self.status_var.set("启动中")

    def _detect_running_port(self) -> int | None:
        """
        检测 Local Agent 当前监听端口。

        Returns:
            int | None: 正常响应的端口；未响应时返回 None。
        """
        for port in PORTS:
            try:
                with urllib.request.urlopen(f"http://{HOST}:{port}/health", timeout=0.2) as response:
                    if response.status == 200:
                        return port
            except (urllib.error.URLError, TimeoutError, OSError):
                continue
        return None

    def _refresh_log_view(self) -> None:
        """从日志文件读取新增内容并显示到窗口。"""
        if not self.log_file.exists():
            return
        try:
            with self.log_file.open("r", encoding="utf-8", errors="ignore") as handle:
                handle.seek(self.log_offset)
                content = handle.read()
                self.log_offset = handle.tell()
        except OSError:
            return
        self._append_log(content)

    def _schedule_refresh(self) -> None:
        """定时刷新状态和日志。"""
        self._refresh_status()
        self._refresh_log_view()
        self.root.after(800, self._schedule_refresh)

    def _on_close(self) -> None:
        """关闭窗口前停止 Local Agent。"""
        if self.process and self.process.poll() is None:
            if not messagebox.askyesno("退出", "退出会停止 GoodHR 本地执行器，确认退出吗？"):
                return
            self._stop_agent()
        self.root.destroy()

    def run(self) -> None:
        """运行桌面启动器。"""
        self.root.mainloop()


def main() -> None:
    """启动桌面窗口。"""
    if "--agent-server" in sys.argv:
        from app.main import main as run_agent

        run_agent()
        return
    GoodHRLauncher().run()


if __name__ == "__main__":
    main()
