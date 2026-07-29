//go:build windows

// Package notification 文件作用：显示 Windows 下载完成提示，并返回用户选择的文件动作。
package notification

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const windowsDownloadDialogScript = `
$shell = New-Object -ComObject WScript.Shell
$line = [Environment]::NewLine
$message = "我下载好了，公主请验收：" + $line + $args[0] + $line + $line + "是：打开文件  否：打开文件夹  取消：先放着"
$result = $shell.Popup($message, 10, "GoodHR", 67)
if ($result -eq 6) { "open" }
elseif ($result -eq 7) { "reveal" }
else { "dismiss" }
`

// NotifyDownload 显示 Windows 下载完成提示，十秒未操作时自动关闭。
func (Notifier) NotifyDownload(ctx context.Context, filePath string) (DownloadAction, error) {
	if _, err := os.Stat(filePath); err != nil {
		return DownloadDismiss, fmt.Errorf("下载文件不存在：%w", err)
	}
	player, err := lookPathAny("powershell.exe", "powershell", "pwsh.exe", "pwsh")
	if err != nil {
		return DownloadDismiss, err
	}
	command := exec.CommandContext(
		ctx,
		player,
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		windowsDownloadDialogScript,
		filepath.Base(filePath),
	)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.Output()
	if err != nil {
		return DownloadDismiss, fmt.Errorf("显示下载提示失败：%w", err)
	}
	switch strings.TrimSpace(string(output)) {
	case string(DownloadOpen):
		return DownloadOpen, nil
	case string(DownloadReveal):
		return DownloadReveal, nil
	default:
		return DownloadDismiss, nil
	}
}
