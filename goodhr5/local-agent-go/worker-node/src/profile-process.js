// 本文件负责在 Windows 上精确清理占用指定招聘账号 Profile 的残留浏览器进程。

import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

/**
 * terminateProfileBrowserProcesses 只结束命令行中使用指定 user-data-dir 的 CloakBrowser 进程树。
 * @param {string} userDataDir - 当前招聘账号的浏览器 Profile 目录。
 * @returns {Promise<number[]>} 被清理的浏览器进程 ID。
 */
export async function terminateProfileBrowserProcesses(userDataDir) {
  const target = String(userDataDir || "").trim();
  if (!target || process.platform !== "win32") return [];

  const script = String.raw`
$ErrorActionPreference = 'Stop'
$target = [IO.Path]::GetFullPath($env:GOODHR_TARGET_PROFILE).TrimEnd([char[]]'\/')
$escaped = [regex]::Escape($target)
$pattern = '--user-data-dir=(?:"' + $escaped + '"|' + $escaped + '(?=\s|$))'
$names = @('chrome.exe', 'chromium.exe', 'CloakBrowser.exe')
$pids = @(
  Get-CimInstance Win32_Process | Where-Object {
    $_.CommandLine -and
    $names -contains $_.Name -and
    [regex]::IsMatch($_.CommandLine, $pattern, [Text.RegularExpressions.RegexOptions]::IgnoreCase)
  } | Select-Object -ExpandProperty ProcessId -Unique
)
foreach ($processId in $pids) {
  $previousPreference = $ErrorActionPreference
  $ErrorActionPreference = 'SilentlyContinue'
  & positionkill.exe /PID $processId /T /F 2>$null | Out-Null
  $ErrorActionPreference = $previousPreference
}
Start-Sleep -Milliseconds 300
$remaining = @(
  Get-CimInstance Win32_Process | Where-Object {
    $_.CommandLine -and
    $names -contains $_.Name -and
    [regex]::IsMatch($_.CommandLine, $pattern, [Text.RegularExpressions.RegexOptions]::IgnoreCase)
  } | Select-Object -ExpandProperty ProcessId -Unique
)
if ($remaining.Count -gt 0) {
  throw ('仍有进程占用当前账号 Profile：' + ($remaining -join ','))
}
@($pids) | ConvertTo-Json -Compress
`;
  const { stdout } = await execFileAsync(
    "powershell.exe",
    ["-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script],
    {
      windowsHide: true,
      timeout: 12000,
      maxBuffer: 1024 * 1024,
      env: { ...process.env, GOODHR_TARGET_PROFILE: target },
    },
  );
  const text = String(stdout || "").trim();
  if (!text) return [];
  const parsed = JSON.parse(text);
  return (Array.isArray(parsed) ? parsed : [parsed])
    .map((value) => Number(value || 0))
    .filter((value) => Number.isInteger(value) && value > 0);
}
