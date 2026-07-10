// Package app 提供本地程序 HTTP 服务中的浏览器窗口激活辅助。
package app

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"
)

// focusCloakBrowserWindow 尝试把 CloakBrowser 窗口拉到系统前台。
// 这是尽力而为的桌面行为，失败时只记录日志，不影响浏览器任务继续执行。
func focusCloakBrowserWindow() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var err error
	switch goruntime.GOOS {
	case "darwin":
		err = focusCloakBrowserWindowDarwin(ctx)
	case "windows":
		err = focusCloakBrowserWindowWindows(ctx)
	default:
		return
	}
	if err != nil {
		log.Printf("[浏览器窗口] 置前失败：%v", err)
	}
}

// shouldFocusBrowserAfterWorkerCall 判断当前 Worker 操作成功后是否需要拉起浏览器窗口。
// path 为 Worker API 路径，只在启动浏览器和打开页面后尝试置前。
func shouldFocusBrowserAfterWorkerCall(path string) bool {
	return path == "/api/v1/browser/start" || path == "/api/v1/page/open"
}

// focusCloakBrowserWindowDarwin 在 macOS 下通过 AppleScript 激活浏览器窗口。
// ctx 为超时上下文，返回最后一次激活失败的错误。
func focusCloakBrowserWindowDarwin(ctx context.Context) error {
	if _, err := exec.LookPath("osascript"); err != nil {
		return err
	}
	script := `
on tryActivate(appName)
  try
    tell application appName to activate
    return true
  end try
  return false
end tryActivate

if tryActivate("CloakBrowser") then return "ok"
if tryActivate("Chromium") then return "ok"
if tryActivate("Google Chrome for Testing") then return "ok"

tell application "System Events"
  repeat with processName in {"CloakBrowser", "Chromium", "Google Chrome for Testing"}
    try
      set frontmost of process processName to true
      return "ok"
    end try
  end repeat
end tell

return "not_found"
`
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w：%s", err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return fmt.Errorf("没有找到可激活的 CloakBrowser 窗口")
	}
	return nil
}

// focusCloakBrowserWindowWindows 在 Windows 下通过 PowerShell 激活浏览器窗口。
// ctx 为超时上下文，返回最后一次激活失败的错误。
func focusCloakBrowserWindowWindows(ctx context.Context) error {
	path, err := exec.LookPath("powershell")
	if err != nil {
		path, err = exec.LookPath("powershell.exe")
		if err != nil {
			return err
		}
	}
	script := `
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Win32Focus {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
}
"@
$names = @("CloakBrowser", "cloakbrowser", "Chromium", "chrome")
foreach ($name in $names) {
  $proc = Get-Process -Name $name -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
  if ($proc) {
    [Win32Focus]::ShowWindow($proc.MainWindowHandle, 9) | Out-Null
    [Win32Focus]::SetForegroundWindow($proc.MainWindowHandle) | Out-Null
    Write-Output "ok"
    exit 0
  }
}
Write-Output "not_found"
`
	out, err := exec.CommandContext(ctx, path, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w：%s", err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return fmt.Errorf("没有找到可激活的 CloakBrowser 窗口")
	}
	return nil
}
