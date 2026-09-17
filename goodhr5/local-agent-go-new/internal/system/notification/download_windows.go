//go:build windows

// Package notification 文件作用：显示 Windows 下载完成提示，并返回用户选择的文件动作。
package notification

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const windowsDownloadDialogScript = `
$shell = New-Object -ComObject WScript.Shell
$line = [Environment]::NewLine
$message = "我下载好了，公主请验收：" + $line + $env:GOODHR_DOWNLOAD_FILE_NAME + $line + $line + "是：打开文件  否：打开文件夹  取消：先放着"
$result = $shell.Popup($message, 10, "GoodHR", 67)
if ($result -eq 6) { "open" }
elseif ($result -eq 7) { "reveal" }
else { "dismiss" }
`

// downloadFileNameEnv 是弹窗脚本读取文件名的环境变量。
// PowerShell 的 -Command 会把后面的参数拼进脚本文本一起执行，
// 所以文件名不能走命令行参数，只能走环境变量。
const downloadFileNameEnv = "GOODHR_DOWNLOAD_FILE_NAME"

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
	)
	command.Env = append(os.Environ(), downloadFileNameEnv+"="+filepath.Base(filePath))
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.Output()
	if err != nil {
		return DownloadDismiss, fmt.Errorf("显示下载提示失败：%w", err)
	}
	return parseDownloadAction(string(output)), nil
}
