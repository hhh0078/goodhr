// 文件作用说明：在 Windows 上只激活当前 Worker 启动的 Camoufox 窗口，不操作招聘页面。

import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

/** WindowFocusResult 记录桌面窗口是否真的进入前台，避免把一次调用当成成功。 */
export interface WindowFocusResult {
  status: "focused" | "already_active" | "not_found" | "denied" | "unsupported" | "error";
  browser_pid?: number;
  foreground_pid?: number;
  error_type?: string;
}

/** WINDOWS_FOCUS_SCRIPT 通过进程祖先定位本 Worker 的浏览器，激活后再次核对前台窗口。 */
const WINDOWS_FOCUS_SCRIPT = String.raw`
$ErrorActionPreference = 'Stop'
$workerPid = [int]$env:GOODHR_FOCUS_WORKER_PID
$processes = Get-CimInstance Win32_Process
$byId = @{}
foreach ($item in $processes) { $byId[[int]$item.ProcessId] = $item }
$target = $null
foreach ($item in $processes) {
  if ($item.Name -ne 'camoufox.exe') { continue }
  $candidate = Get-Process -Id $item.ProcessId -ErrorAction SilentlyContinue
  if (!$candidate -or $candidate.MainWindowHandle -eq [IntPtr]::Zero) { continue }
  $ancestor = $item
  for ($level = 0; $level -lt 12 -and $ancestor; $level++) {
    if ([int]$ancestor.ProcessId -eq $workerPid) { $target = $candidate; break }
    $ancestor = $byId[[int]$ancestor.ParentProcessId]
  }
  if ($target) { break }
}
if (!$target) { @{ status = 'not_found' } | ConvertTo-Json -Compress; exit 0 }
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class GoodHrBrowserFocusWin32 {
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr window, out uint processId);
  [DllImport("user32.dll")] public static extern bool AttachThreadInput(uint source, uint target, bool attach);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr window);
  [DllImport("user32.dll")] public static extern bool BringWindowToTop(IntPtr window);
  [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr window);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr window, int command);
  [DllImport("kernel32.dll")] public static extern uint GetCurrentThreadId();
}
'@
$handle = $target.MainWindowHandle
$before = [GoodHrBrowserFocusWin32]::GetForegroundWindow()
if ($before -eq $handle) {
  @{ status = 'already_active'; browser_pid = [int]$target.Id; foreground_pid = [int]$target.Id } | ConvertTo-Json -Compress
  exit 0
}
$foregroundPid = [uint32]0
$foregroundThread = [GoodHrBrowserFocusWin32]::GetWindowThreadProcessId($before, [ref]$foregroundPid)
$currentThread = [GoodHrBrowserFocusWin32]::GetCurrentThreadId()
$attached = $foregroundThread -ne 0 -and [GoodHrBrowserFocusWin32]::AttachThreadInput($currentThread, $foregroundThread, $true)
try {
  if ([GoodHrBrowserFocusWin32]::IsIconic($handle)) { [GoodHrBrowserFocusWin32]::ShowWindow($handle, 9) | Out-Null }
  [GoodHrBrowserFocusWin32]::BringWindowToTop($handle) | Out-Null
  [GoodHrBrowserFocusWin32]::SetForegroundWindow($handle) | Out-Null
} finally {
  if ($attached) { [GoodHrBrowserFocusWin32]::AttachThreadInput($currentThread, $foregroundThread, $false) | Out-Null }
}
Start-Sleep -Milliseconds 150
$after = [GoodHrBrowserFocusWin32]::GetForegroundWindow()
@{ status = $(if ($after -eq $handle) { 'focused' } else { 'denied' }); browser_pid = [int]$target.Id; foreground_pid = [int]$foregroundPid } | ConvertTo-Json -Compress
`;

/** focusCurrentBrowserWindow 只在 Windows 上尝试恢复当前 Worker 的浏览器前台状态。 */
export async function focusCurrentBrowserWindow(): Promise<WindowFocusResult> {
  if (process.platform !== "win32") return { status: "unsupported" };
  try {
    const { stdout } = await execFileAsync("powershell.exe", [
      "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", WINDOWS_FOCUS_SCRIPT,
    ], {
      timeout: 5_000,
      windowsHide: true,
      maxBuffer: 16_384,
      env: { ...process.env, GOODHR_FOCUS_WORKER_PID: String(process.pid) },
    });
    const parsed: unknown = JSON.parse(stdout.trim());
    if (!parsed || typeof parsed !== "object" || !("status" in parsed)) return { status: "error", error_type: "InvalidResult" };
    const result = parsed as WindowFocusResult;
    if (!["focused", "already_active", "not_found", "denied"].includes(result.status)) return { status: "error", error_type: "InvalidStatus" };
    return result;
  } catch (error) {
    return { status: "error", error_type: error instanceof Error ? error.name : "UnknownError" };
  }
}
