"""GoodHR Local Agent 桌面启动器。

本文件提供双击运行时的小窗口，用于启动/停止 Local Agent、查看运行日志、
清理窗口日志，并打开 GoodHR 官网。打包为 macOS app 或 Windows exe 时，
该文件作为图形界面入口。
"""

from __future__ import annotations

import ctypes
import json
import multiprocessing
import os
import platform
import shutil
import subprocess
import sys
import tarfile
import threading
import time
import tkinter as tk
import urllib.error
import urllib.request
import webbrowser
import zipfile
from pathlib import Path
from tkinter import messagebox, scrolledtext


APP_NAME = "GoodHR"
APP_DATA_DIR_NAME = "GoodHR"
OFFICIAL_SITE_URL = "https://goodhr5.58it.cn"
BROWSER_DOWNLOAD_CONFIG_URL = f"{OFFICIAL_SITE_URL}/agent-browser-downloads.json"
DEFAULT_BROWSER_DOWNLOADS = {
    "mac": "https://github.com/CloakHQ/CloakBrowser/releases/download/chromium-v145.0.7632.109.2/cloakbrowser-darwin-arm64.tar.gz",
    "win": "https://github.com/CloakHQ/CloakBrowser/releases/download/chromium-v146.0.7680.177.5/cloakbrowser-windows-x64.zip",
}
WINDOWS_RUNTIME_URL = "https://aka.ms/vs/17/release/vc_redist.x64.exe"
SHORTCUT_MARKER_FILE = "desktop_shortcut_created"
HOST = "127.0.0.1"
PORTS = range(9001, 9010)
THEME_BG = "#0a0a0a"
THEME_PANEL = "#0d0d0d"
THEME_INPUT = "#111111"
THEME_FG = "#00ff00"
THEME_DIM = "#dddddd"
THEME_BORDER = "#333333"
THEME_ACTIVE = "#063f06"
THEME_FONT = ("Courier New", 10)
THEME_TITLE_FONT = ("Courier New", 20, "bold")
THEME_SECTION_FONT = ("Courier New", 12, "bold")


def configure_utf8_stdio() -> None:
    """
    配置当前进程标准输出为 UTF-8。

    Windows 打包后如果没有提前设置，部分三方库日志可能按系统默认编码写出，
    导致桌面日志框看到中文乱码。
    """
    os.environ.setdefault("PYTHONIOENCODING", "utf-8")
    os.environ.setdefault("PYTHONUTF8", "1")
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")


def app_support_dir() -> Path:
    """
    获取 Local Agent 运行数据目录。

    Returns:
        Path: 当前系统对应的应用数据目录。
    """
    system = platform.system().lower()
    if system == "darwin":
        return Path.home() / "Library" / "Application Support" / APP_DATA_DIR_NAME
    if system == "windows":
        appdata = os.getenv("APPDATA") or str(Path.home() / "AppData" / "Roaming")
        return Path(appdata) / APP_DATA_DIR_NAME
    return Path.home() / f".{APP_DATA_DIR_NAME.lower()}"


def bundle_root() -> Path:
    """
    获取程序资源根目录。

    PyInstaller 打包后资源可能位于 sys._MEIPASS，也可能在 macOS .app 的
    Contents/Resources；源码运行时资源位于 local-agent 目录。

    Returns:
        Path: 程序资源根目录。
    """
    frozen_root = getattr(sys, "_MEIPASS", "")
    if frozen_root:
        root = Path(frozen_root)
        exe_path = Path(sys.executable).resolve()
        candidates = resource_root_candidates(root, exe_path)
        archive_name = platform_archive_name()
        for candidate in candidates:
            if (candidate / "vendor" / "downloads" / archive_name).exists():
                return candidate
            if (candidate / "vendor" / "cloakbrowser").exists():
                return candidate
        return root
    return Path(__file__).resolve().parent


def resource_root_candidates(frozen_root: Path, executable_path: Path) -> list[Path]:
    """
    生成打包后可能的资源根目录。

    Args:
        frozen_root: PyInstaller 提供的内部资源目录。
        executable_path: 当前可执行文件路径。

    Returns:
        list[Path]: 去重后的资源候选目录。
    """
    candidates: list[Path] = []

    def add(path: Path) -> None:
        resolved = path.resolve()
        if resolved not in candidates:
            candidates.append(resolved)

    for base in [frozen_root, executable_path.parent, *frozen_root.parents, *executable_path.parents]:
        add(base)
        add(base / "Resources")
        if base.name == "Contents":
            add(base / "Resources")

    return candidates


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
        "profiles": base_dir / "profiles",
        "tasks": base_dir / "tasks",
        "vendor": base_dir / "vendor",
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


def ensure_executable_permission(binary: Path | None) -> Path | None:
    """
    确保浏览器文件具备可执行权限。

    Args:
        binary: 浏览器可执行文件路径。

    Returns:
        Path | None: 原始浏览器路径；为空时返回 None。
    """
    if binary is None:
        return None
    if platform.system().lower() != "windows":
        ensure_macos_app_permissions(binary)
    return binary


def ensure_macos_app_permissions(binary: Path) -> None:
    """
    修复 macOS Chromium.app 内部可执行文件权限。

    zip 解压后可能丢失 Chromium Helper、GPU、Network 等子进程文件的执行权限，
    导致浏览器启动后创建页面时立刻关闭。

    Args:
        binary: Chromium 主程序路径。
    """
    app_dir = find_parent_app_dir(binary)
    targets = [binary]
    if app_dir is not None:
        targets.extend(path for path in app_dir.rglob("*") if should_chmod_executable(path))

    for target in targets:
        try:
            target.chmod(target.stat().st_mode | 0o755)
        except OSError:
            continue


def find_parent_app_dir(path: Path) -> Path | None:
    """
    查找路径所属的 macOS .app 目录。

    Args:
        path: 任意文件路径。

    Returns:
        Path | None: 找到的 .app 目录；找不到时返回 None。
    """
    for parent in [path, *path.parents]:
        if parent.suffix == ".app":
            return parent
    return None


def should_chmod_executable(path: Path) -> bool:
    """
    判断文件是否应补充执行权限。

    Args:
        path: 待检查文件路径。

    Returns:
        bool: 是否应添加执行权限。
    """
    if not path.is_file():
        return False
    if path.parent.name in {"MacOS", "Helpers"}:
        return True
    try:
        header = path.read_bytes()[:4]
    except OSError:
        return False
    return header in {
        b"\xcf\xfa\xed\xfe",
        b"\xca\xfe\xba\xbe",
        b"\xca\xfe\xba\xbf",
        b"\xfe\xed\xfa\xcf",
        b"\xfe\xed\xfa\xce",
        b"\xce\xfa\xed\xfe",
    } or header.startswith(b"#!")


def platform_archive_name() -> str:
    """
    获取当前系统对应的 CloakBrowser 压缩包名称。

    Returns:
        str: 当前平台压缩包文件名。
    """
    system = platform.system().lower()
    if system == "darwin":
        return "cloakbrowser_mac.tar.gz"
    if system == "windows":
        return "cloakbrowser_win.zip"
    return "cloakbrowser_linux.zip"


def browser_download_key() -> str:
    """
    获取当前系统在浏览器下载配置中的键名。

    Returns:
        str: mac、win 或 linux。
    """
    system = platform.system().lower()
    if system == "darwin":
        return "mac"
    if system == "windows":
        return "win"
    return "linux"


def load_browser_downloads() -> dict[str, str]:
    """
    读取浏览器下载地址配置。

    优先读取官网公开 JSON；失败时使用程序内置默认地址，避免配置文件临时不可用时无法启动。

    Returns:
        dict[str, str]: 平台到下载地址的映射。
    """
    downloads = dict(DEFAULT_BROWSER_DOWNLOADS)
    try:
        request = urllib.request.Request(BROWSER_DOWNLOAD_CONFIG_URL, headers={"User-Agent": "GoodHRLocalAgent"})
        with urllib.request.urlopen(request, timeout=10) as response:
            data = json.loads(response.read().decode("utf-8"))
        if isinstance(data, dict):
            for key, value in data.items():
                url = str(value or "").strip()
                if url:
                    downloads[str(key)] = url
    except Exception:
        pass
    return downloads


def browser_archive_path(download_dir: Path, url: str) -> Path:
    """
    根据下载地址生成本地浏览器压缩包路径。

    Args:
        download_dir: 压缩包保存目录。
        url: 浏览器下载地址。

    Returns:
        Path: 本地压缩包路径。
    """
    name = Path(urllib.request.url2pathname(url.split("?", 1)[0])).name
    if not name:
        name = platform_archive_name()
    return download_dir / name


def download_browser_archive(url: str, target: Path, progress_callback: object | None = None) -> None:
    """
    下载浏览器压缩包并上报进度。

    Args:
        url: 下载地址。
        target: 保存路径。
        progress_callback: 进度回调，参数为 downloaded 和 total。
    """
    target.parent.mkdir(parents=True, exist_ok=True)
    temp_target = target.with_suffix(target.suffix + ".part")
    if temp_target.exists():
        temp_target.unlink()

    if platform.system().lower() == "windows":
        try:
            download_browser_archive_with_powershell(url, temp_target, target, progress_callback)
            return
        except Exception:
            if temp_target.exists():
                temp_target.unlink()

    request = urllib.request.Request(url, headers={"User-Agent": "GoodHRLocalAgent"})
    with urllib.request.urlopen(request, timeout=120) as response:
        total = int(response.headers.get("Content-Length") or 0)
        downloaded = 0
        with temp_target.open("wb") as file:
            while True:
                chunk = response.read(1024 * 1024)
                if not chunk:
                    break
                file.write(chunk)
                downloaded += len(chunk)
                if progress_callback:
                    progress_callback(downloaded, total)
    temp_target.replace(target)


def download_browser_archive_with_powershell(
    url: str,
    temp_target: Path,
    target: Path,
    progress_callback: object | None = None,
) -> None:
    """
    在 Windows 上使用 PowerShell/.NET 下载浏览器压缩包。

    Python urllib 在部分 Windows 环境会遇到根证书校验失败；PowerShell/.NET
    使用系统证书存储，成功率更高。

    Args:
        url: 下载地址。
        temp_target: 临时保存路径。
        target: 最终保存路径。
        progress_callback: 进度回调，参数为 downloaded 和 total。
    """
    script_path = target.parent / "goodhr_download_browser.ps1"
    script_path.write_text(
        r'''
param(
    [Parameter(Mandatory=$true)][string]$Url,
    [Parameter(Mandatory=$true)][string]$TempPath,
    [Parameter(Mandatory=$true)][string]$TargetPath
)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = `
    [Net.SecurityProtocolType]::Tls12 -bor `
    [Net.SecurityProtocolType]::Tls11 -bor `
    [Net.SecurityProtocolType]::Tls

if (Test-Path $TempPath) {
    Remove-Item -Force $TempPath
}

$request = [System.Net.HttpWebRequest]::Create($Url)
$request.UserAgent = "GoodHRLocalAgent"
$request.AllowAutoRedirect = $true
$response = $request.GetResponse()
$total = [int64]$response.ContentLength
$stream = $response.GetResponseStream()
$file = [System.IO.File]::Open($TempPath, [System.IO.FileMode]::Create, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
$buffer = New-Object byte[] 1048576
$downloaded = [int64]0

try {
    while (($read = $stream.Read($buffer, 0, $buffer.Length)) -gt 0) {
        $file.Write($buffer, 0, $read)
        $downloaded += $read
        Write-Output "GOODHR_PROGRESS $downloaded $total"
    }
}
finally {
    $file.Close()
    $stream.Close()
    $response.Close()
}

Move-Item -Force $TempPath $TargetPath
'''.strip(),
        encoding="utf-8",
    )
    try:
        result = subprocess.Popen(
            [
                windows_powershell_path(),
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                str(script_path),
                url,
                str(temp_target),
                str(target),
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
        output_lines: list[str] = []
        if result.stdout:
            for line in result.stdout:
                line = line.strip()
                if line.startswith("GOODHR_PROGRESS "):
                    parts = line.split()
                    if len(parts) >= 3 and progress_callback:
                        progress_callback(int(parts[1]), int(parts[2]))
                elif line:
                    output_lines.append(line)
        exit_code = result.wait()
        if exit_code != 0:
            raise RuntimeError("\n".join(output_lines) or f"PowerShell 下载失败，退出码={exit_code}")
    finally:
        try:
            script_path.unlink()
        except OSError:
            pass


def extract_browser_archive(archive: Path, runtime_vendor_dir: Path) -> None:
    """
    解压浏览器压缩包到运行目录。

    Args:
        archive: 浏览器压缩包路径。
        runtime_vendor_dir: 解压目标目录。
    """
    if runtime_vendor_dir.exists():
        shutil.rmtree(runtime_vendor_dir)
    runtime_vendor_dir.mkdir(parents=True, exist_ok=True)
    suffix = archive.name.lower()
    if suffix.endswith(".zip"):
        with zipfile.ZipFile(archive) as zip_file:
            zip_file.extractall(runtime_vendor_dir)
        return
    if suffix.endswith(".tar.gz") or suffix.endswith(".tgz"):
        with tarfile.open(archive, "r:gz") as tar:
            tar.extractall(runtime_vendor_dir)
        return
    raise RuntimeError(f"不支持的浏览器压缩包格式：{archive.name}")


def ensure_cloakbrowser_binary(
    root: Path,
    runtime_vendor_dir: Path,
    progress_callback: object | None = None,
    log_callback: object | None = None,
) -> Path | None:
    """
    确保 CloakBrowser 可执行文件存在。

    优先使用运行数据目录中已经解压的浏览器；如果不存在，则读取官网公开 JSON
    获取当前平台浏览器下载地址，下载到运行目录后解压。

    Args:
        root: 程序资源根目录。
        runtime_vendor_dir: 运行时浏览器目录。
        progress_callback: 下载进度回调。
        log_callback: 日志回调。

    Returns:
        Path | None: CloakBrowser 可执行文件路径。
    """
    runtime_root = runtime_vendor_dir.parent.parent
    existing = find_cloakbrowser_binary(runtime_root)
    if existing is not None:
        return ensure_executable_permission(existing)

    downloads = load_browser_downloads()
    url = downloads.get(browser_download_key(), "").strip()
    if not url:
        return ensure_executable_permission(find_cloakbrowser_binary(root))

    download_dir = runtime_vendor_dir.parent / "downloads"
    archive = browser_archive_path(download_dir, url)
    if not archive.exists():
        if log_callback:
            log_callback(f"开始下载浏览器组件：{url}\n")
        download_browser_archive(url, archive, progress_callback)

    if log_callback:
        log_callback(f"正在解压浏览器组件：{archive}\n")
    try:
        extract_browser_archive(archive, runtime_vendor_dir)
    except Exception:
        if archive.exists():
            archive.unlink()
        raise
    return ensure_executable_permission(find_cloakbrowser_binary(runtime_root))


def packaged_app_target() -> Path | None:
    """
    获取打包程序本体路径。

    Returns:
        Path | None: 打包后的 exe 或 app 路径；源码运行时返回 None。
    """
    if not getattr(sys, "frozen", False):
        return None
    executable = Path(sys.executable).resolve()
    if platform.system().lower() == "darwin":
        app_dir = find_parent_app_dir(executable)
        return app_dir or executable
    return executable


def desktop_dir() -> Path:
    """
    获取当前用户桌面目录。

    Returns:
        Path: 当前系统桌面路径。
    """
    if platform.system().lower() == "windows":
        windows_desktop = windows_known_desktop_dir()
        if windows_desktop is not None:
            return windows_desktop
        user_profile = os.environ.get("USERPROFILE")
        if user_profile:
            return Path(user_profile) / "Desktop"
    return Path.home() / "Desktop"


def windows_known_desktop_dir() -> Path | None:
    """
    读取 Windows 系统登记的真实桌面目录。

    Returns:
        Path | None: 读取成功时返回桌面路径，失败时返回 None。
    """
    if platform.system().lower() != "windows":
        return None
    class GUID(ctypes.Structure):
        _fields_ = [
            ("Data1", ctypes.c_uint32),
            ("Data2", ctypes.c_ushort),
            ("Data3", ctypes.c_ushort),
            ("Data4", ctypes.c_ubyte * 8),
        ]

    folder_id_desktop = GUID(
        0xB4BFCC3A,
        0xDB2C,
        0x424C,
        (ctypes.c_ubyte * 8)(0xB0, 0x29, 0x7F, 0xE9, 0x9A, 0x87, 0xC6, 0x41),
    )
    path_ptr = ctypes.c_wchar_p()
    try:
        result = ctypes.windll.shell32.SHGetKnownFolderPath(  # type: ignore[attr-defined]
            ctypes.byref(folder_id_desktop),
            0,
            None,
            ctypes.byref(path_ptr),
        )
        if result != 0 or not path_ptr.value:
            return None
        return Path(path_ptr.value)
    except Exception:
        return None
    finally:
        if path_ptr.value:
            ctypes.windll.ole32.CoTaskMemFree(path_ptr)  # type: ignore[attr-defined]


def ensure_desktop_shortcut(base_dir: Path, force: bool = False) -> str:
    """
    首次运行时创建 GoodHR 桌面快捷方式。

    Args:
        base_dir: 应用数据根目录。
        force: 是否强制重新创建快捷方式。

    Returns:
        str: 创建结果说明。
    """
    target = packaged_app_target()
    if target is None:
        return "源码运行，跳过桌面快捷方式创建。"

    shortcut_path = desktop_shortcut_path(target)
    marker = base_dir / "config" / SHORTCUT_MARKER_FILE
    if not force and marker.exists() and shortcut_path.exists():
        return "桌面快捷方式已存在。"

    shortcut_path.parent.mkdir(parents=True, exist_ok=True)
    if platform.system().lower() == "windows":
        create_windows_desktop_shortcut(shortcut_path, target)
    else:
        create_unix_desktop_shortcut(shortcut_path, target)

    marker.parent.mkdir(parents=True, exist_ok=True)
    marker.write_text(str(shortcut_path), encoding="utf-8")
    return f"已创建桌面快捷方式：{shortcut_path}"


def desktop_shortcut_path(target: Path) -> Path:
    """
    计算桌面快捷方式路径。

    Args:
        target: 打包程序本体路径。

    Returns:
        Path: 桌面快捷方式路径。
    """
    if platform.system().lower() == "windows":
        return desktop_dir() / f"{APP_NAME}.lnk"
    suffix = ".app" if target.suffix == ".app" else ""
    return desktop_dir() / f"{APP_NAME}{suffix}"


def create_windows_desktop_shortcut(shortcut_path: Path, target: Path) -> None:
    """
    创建 Windows 桌面 lnk 快捷方式。

    Args:
        shortcut_path: 快捷方式保存路径。
        target: 快捷方式指向的 exe 路径。
    """
    script = (
        "$Shell = New-Object -ComObject WScript.Shell; "
        f"$Shortcut = $Shell.CreateShortcut('{escape_powershell(shortcut_path)}'); "
        f"$Shortcut.TargetPath = '{escape_powershell(target)}'; "
        f"$Shortcut.WorkingDirectory = '{escape_powershell(target.parent)}'; "
        f"$Shortcut.IconLocation = '{escape_powershell(target)},0'; "
        "$Shortcut.Save()"
    )
    powershell = windows_powershell_path()
    result = subprocess.run(
        [powershell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        raise RuntimeError(detail or f"PowerShell 创建快捷方式失败，退出码={result.returncode}")


def windows_powershell_path() -> str:
    """
    获取 Windows PowerShell 可执行文件路径。

    Returns:
        str: PowerShell 可执行文件路径或命令名。
    """
    found = shutil.which("powershell.exe") or shutil.which("powershell")
    if found:
        return found
    system_root = os.environ.get("SystemRoot", r"C:\Windows")
    candidate = Path(system_root) / "System32" / "WindowsPowerShell" / "v1.0" / "powershell.exe"
    return str(candidate)


def create_unix_desktop_shortcut(shortcut_path: Path, target: Path) -> None:
    """
    创建 macOS/Linux 桌面快捷入口。

    Args:
        shortcut_path: 快捷入口路径。
        target: 快捷入口指向的 app 或可执行文件。
    """
    if shortcut_path.exists() or shortcut_path.is_symlink():
        if not shortcut_path.is_symlink():
            return
        shortcut_path.unlink()
    shortcut_path.symlink_to(target)


def escape_powershell(value: Path) -> str:
    """
    转义 PowerShell 单引号字符串。

    Args:
        value: 需要放入 PowerShell 字符串的路径。

    Returns:
        str: 已转义路径。
    """
    return str(value).replace("'", "''")


class GoodHRLauncher:
    """GoodHR Local Agent 图形启动器。"""

    def __init__(self) -> None:
        """初始化窗口、运行目录和状态。"""
        self.root = tk.Tk()
        self.root.title(APP_NAME)
        self.root.configure(bg=THEME_BG)
        self.icon_image: tk.PhotoImage | None = None
        self._apply_window_icon()
        sw = self.root.winfo_screenwidth()
        sh = self.root.winfo_screenheight()
        w = max(680, int(sw * 0.55))
        h = max(460, int(sh * 0.65))
        self.root.geometry(f"{w}x{h}")
        self.root.minsize(680, 460)

        self.base_dir = app_support_dir()
        self.dirs = ensure_runtime_dirs(self.base_dir)
        self.process: subprocess.Popen[str] | None = None
        self.running_port: int | None = None
        self.agent_starting = False

        self.status_var = tk.StringVar(value="准备启动")
        self.detail_var = tk.StringVar(value=f"数据目录：{self.base_dir}")

        self._build_ui()
        self._ensure_desktop_shortcut()
        self.root.after(100, self._start_agent)
        self._schedule_refresh()
        self.root.protocol("WM_DELETE_WINDOW", self._on_close)

    def _build_ui(self) -> None:
        """创建窗口组件。"""
        wrapper = tk.Frame(
            self.root,
            padx=16,
            pady=14,
            bg=THEME_PANEL,
            highlightbackground=THEME_BORDER,
            highlightthickness=1,
        )
        wrapper.pack(fill=tk.BOTH, expand=True)

        title = self._make_label(wrapper, text=APP_NAME, font=THEME_TITLE_FONT, fg=THEME_FG)
        title.pack(anchor="w")

        desc = self._make_label(
            wrapper,
            text="本程序负责启动本地浏览器、执行平台页面操作、截图 OCR 和任务数据保存。",
            anchor="w",
            fg=THEME_DIM,
        )
        desc.pack(anchor="w", pady=(6, 10))

        status_row = tk.Frame(wrapper, bg=THEME_PANEL)
        status_row.pack(fill=tk.X)
        self._make_label(status_row, text="当前状态：", font=THEME_SECTION_FONT).pack(side=tk.LEFT)
        self._make_label(status_row, textvariable=self.status_var, fg=THEME_FG, font=THEME_SECTION_FONT).pack(
            side=tk.LEFT
        )

        self._make_label(wrapper, textvariable=self.detail_var, anchor="w", fg=THEME_DIM).pack(fill=tk.X, pady=(6, 10))

        button_row = tk.Frame(wrapper, bg=THEME_PANEL)
        button_row.pack(fill=tk.X, pady=(0, 10))
        self._make_button(button_row, text="打开官网", command=self._open_site).pack(side=tk.LEFT, padx=(0, 8))
        if platform.system().lower() == "windows":
            self._make_button(button_row, text="下载安装环境", command=self._open_windows_runtime, width=14).pack(
                side=tk.LEFT,
                padx=(0, 8),
            )
            self._make_button(button_row, text="创建快捷方式", command=self._create_desktop_shortcut, width=14).pack(
                side=tk.LEFT,
                padx=(0, 8),
            )
        self._make_button(button_row, text="停止服务", command=self._stop_agent).pack(side=tk.LEFT, padx=(0, 8))
        self._make_button(button_row, text="清除日志", command=self._clear_logs).pack(side=tk.LEFT, padx=(0, 8))
        self._make_button(button_row, text="重新启动", command=self._restart_agent).pack(side=tk.LEFT, padx=(0, 8))
        self._make_button(button_row, text="重下浏览器", command=self._redownload_browser, width=14).pack(side=tk.LEFT)

        if platform.system().lower() == "windows":
            self._make_label(
                wrapper,
                text="程序无法启动？请先下载安装环境包，安装后重启电脑。",
                anchor="w",
                fg=THEME_DIM,
            ).pack(fill=tk.X, pady=(0, 10))

        self._make_label(wrapper, text="运行日志", font=THEME_SECTION_FONT).pack(anchor="w")
        self.log_view = scrolledtext.ScrolledText(
            wrapper,
            height=20,
            wrap=tk.WORD,
            state=tk.DISABLED,
            bg=THEME_INPUT,
            fg=THEME_FG,
            insertbackground=THEME_FG,
            selectbackground=THEME_ACTIVE,
            selectforeground=THEME_FG,
            relief=tk.FLAT,
            borderwidth=0,
            highlightthickness=1,
            highlightbackground=THEME_BORDER,
            highlightcolor=THEME_FG,
            font=THEME_FONT,
        )
        self.log_view.pack(fill=tk.BOTH, expand=True, pady=(6, 0))

    def _make_label(self, parent: tk.Widget, **kwargs: object) -> tk.Label:
        """
        创建统一主题的文本标签。

        Args:
            parent: 标签所属的父组件。
            **kwargs: 传给 Tk Label 的额外参数。

        Returns:
            tk.Label: 已应用 GoodHR 主题的标签组件。
        """
        options = {
            "bg": THEME_PANEL,
            "fg": THEME_FG,
            "font": THEME_FONT,
        }
        options.update(kwargs)
        return tk.Label(parent, **options)

    def _make_button(self, parent: tk.Widget, text: str, command: object, width: int = 12) -> tk.Button:
        """
        创建统一主题的操作按钮。

        Args:
            parent: 按钮所属的父组件。
            text: 按钮显示文字。
            command: 点击按钮时执行的回调。
            width: 按钮宽度。

        Returns:
            tk.Button: 已应用 GoodHR 主题的按钮组件。
        """
        return tk.Button(
            parent,
            text=text,
            command=command,
            width=width,
            bg=THEME_INPUT,
            fg=THEME_FG,
            activebackground=THEME_ACTIVE,
            activeforeground=THEME_FG,
            disabledforeground=THEME_BORDER,
            relief=tk.FLAT,
            borderwidth=0,
            highlightthickness=1,
            highlightbackground=THEME_BORDER,
            highlightcolor=THEME_FG,
            cursor="hand2",
            font=THEME_FONT,
        )

    def _apply_window_icon(self) -> None:
        """给桌面窗口设置 GoodHR 图标。"""
        icon_path = bundle_root() / "assets" / "icons" / "goodhr-logo.png"
        if not icon_path.exists():
            return

        try:
            self.icon_image = tk.PhotoImage(file=str(icon_path))
            self.root.iconphoto(True, self.icon_image)
        except tk.TclError:
            self.icon_image = None

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

    def _append_log_threadsafe(self, text: str) -> None:
        """
        从后台线程安全地追加日志。

        Args:
            text: 要追加的日志内容。
        """
        self.root.after(0, self._append_log, text)

    def _set_status_threadsafe(self, status: str, detail: str = "") -> None:
        """
        从后台线程安全地更新状态文案。

        Args:
            status: 当前状态。
            detail: 详情文案，为空时不更新详情。
        """
        def update() -> None:
            self.status_var.set(status)
            if detail:
                self.detail_var.set(detail)

        self.root.after(0, update)

    def _ensure_desktop_shortcut(self) -> None:
        """创建桌面快捷方式并把结果写入窗口日志。"""
        try:
            message = ensure_desktop_shortcut(self.base_dir)
            if "已创建" in message:
                self._append_log(f"{message}\n")
        except Exception as exc:
            self._append_log(f"创建桌面快捷方式失败：{exc}\n")

    def _create_desktop_shortcut(self) -> None:
        """手动创建桌面快捷方式并显示结果。"""
        try:
            message = ensure_desktop_shortcut(self.base_dir, force=True)
            self._append_log(f"{message}\n")
            messagebox.showinfo("GoodHR", "桌面快捷方式已创建")
        except Exception as exc:
            self._append_log(f"创建桌面快捷方式失败：{exc}\n")
            messagebox.showerror("GoodHR", f"创建桌面快捷方式失败：{exc}")

    def _redownload_browser(self) -> None:
        """清理浏览器组件并重新下载启动。"""
        if self.agent_starting:
            messagebox.showinfo("GoodHR", "浏览器组件正在准备中，请稍后再试。")
            return
        if not messagebox.askyesno("重新下载浏览器组件", "将停止服务并删除本地浏览器组件，然后重新下载。确认继续吗？"):
            return
        self._stop_agent()
        try:
            self._clear_browser_components()
        except Exception as exc:
            self.status_var.set("清理失败")
            self.detail_var.set(str(exc))
            self._append_log(f"清理浏览器组件失败：{exc}\n")
            messagebox.showerror("GoodHR", f"清理浏览器组件失败：{exc}")
            return
        self._append_log("已清理浏览器组件，准备重新下载。\n")
        self._start_agent()

    def _clear_browser_components(self) -> None:
        """
        删除已下载和已解压的浏览器组件。

        只清理 vendor 下的浏览器运行文件和压缩包，不影响账号、任务、cookie 等数据。
        """
        for path in [self.dirs["vendor"] / "cloakbrowser", self.dirs["vendor"] / "downloads"]:
            if path.exists():
                shutil.rmtree(path)
            path.mkdir(parents=True, exist_ok=True)

    def _start_agent(self) -> None:
        """启动 Local Agent 子进程。"""
        if self.process and self.process.poll() is None:
            self.status_var.set("运行中")
            return
        if self.agent_starting:
            return

        self.agent_starting = True
        self.status_var.set("准备浏览器")
        threading.Thread(target=self._start_agent_worker, daemon=True).start()

    def _start_agent_worker(self) -> None:
        """在后台线程中准备浏览器并启动 Local Agent。"""
        try:
            browser_binary = ensure_cloakbrowser_binary(
                bundle_root(),
                self.dirs["vendor"] / "cloakbrowser",
                progress_callback=self._on_browser_download_progress,
                log_callback=self._append_log_threadsafe,
            )
        except Exception as exc:
            self.agent_starting = False
            self._set_status_threadsafe("浏览器准备失败", str(exc))
            self._append_log_threadsafe(f"浏览器组件准备失败：{exc}\n")
            return

        if browser_binary is None:
            self.agent_starting = False
            self._set_status_threadsafe("缺少 CloakBrowser", "未找到浏览器组件下载地址。")
            self._append_log_threadsafe("未找到浏览器组件下载地址，Local Agent 未启动。\n")
            return

        env = os.environ.copy()
        env["GOODHR_AGENT_DATA_DIR"] = str(self.dirs["agent_data"])
        env["GOODHR_AGENT_LOG_TO_STDOUT"] = "1"
        env["CLOAKBROWSER_BINARY_PATH"] = str(browser_binary)
        env["PYTHONIOENCODING"] = "utf-8"
        env["PYTHONUTF8"] = "1"

        if getattr(sys, "frozen", False):
            command = [sys.executable, "--agent-server"]
        else:
            command = [sys.executable, str(Path(__file__).resolve()), "--agent-server"]
        self._set_status_threadsafe("启动中", f"CloakBrowser：{browser_binary}")
        self._append_log_threadsafe(f"正在启动 Local Agent...\n数据目录：{self.base_dir}\n浏览器：{browser_binary}\n")

        env["PYTHONUNBUFFERED"] = "1"
        creationflags = 0
        if platform.system().lower() == "windows" and hasattr(subprocess, "CREATE_NO_WINDOW"):
            creationflags = subprocess.CREATE_NO_WINDOW
        try:
            self.process = subprocess.Popen(
                command,
                cwd=str(bundle_root()),
                env=env,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                encoding="utf-8",
                errors="replace",
                creationflags=creationflags,
            )
        except Exception as exc:
            self.agent_starting = False
            self._set_status_threadsafe("启动失败", str(exc))
            self._append_log_threadsafe(f"Local Agent 启动失败：{exc}\n")
            return
        self.agent_starting = False
        self._start_stdout_reader()

    def _on_browser_download_progress(self, downloaded: int, total: int) -> None:
        """
        显示浏览器组件下载进度。

        Args:
            downloaded: 已下载字节数。
            total: 总字节数，为 0 时表示未知。
        """
        downloaded_mb = downloaded / 1024 / 1024
        if total > 0:
            total_mb = total / 1024 / 1024
            percent = downloaded * 100 / total
            detail = f"浏览器组件下载中：{percent:.1f}% ({downloaded_mb:.1f}/{total_mb:.1f} MB)"
        else:
            detail = f"浏览器组件下载中：{downloaded_mb:.1f} MB"
        self._set_status_threadsafe("下载浏览器", detail)

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
        """清空日志窗口。"""
        self.log_view.configure(state=tk.NORMAL)
        self.log_view.delete("1.0", tk.END)
        self.log_view.configure(state=tk.DISABLED)

    def _open_site(self) -> None:
        """使用默认浏览器打开 GoodHR 官网。"""
        webbrowser.open(OFFICIAL_SITE_URL)

    def _open_windows_runtime(self) -> None:
        """使用默认浏览器打开 Windows 运行环境下载地址。"""
        webbrowser.open(WINDOWS_RUNTIME_URL)

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

    def _start_stdout_reader(self) -> None:
        """启动后台线程读取子进程输出并显示到窗口。"""
        process = self.process
        if process is None or process.stdout is None:
            return

        def reader() -> None:
            for line in process.stdout:
                self.root.after(0, self._append_log, line)

        threading.Thread(target=reader, daemon=True).start()

    def _schedule_refresh(self) -> None:
        """定时刷新状态。"""
        self._refresh_status()
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
    """启动桌面窗口或 Local Agent 服务。"""
    configure_utf8_stdio()
    multiprocessing.freeze_support()
    if "--agent-server" in sys.argv:
        from app.main import main as run_agent

        run_agent()
        return
    GoodHRLauncher().run()


if __name__ == "__main__":
    main()
