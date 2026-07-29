//go:build windows

// Package console 文件作用：使用 Windows 默认浏览器打开 GoodHR 控制台。
package console

import (
	"context"
	"os/exec"
	"syscall"
)

// openBrowserURL 使用 Windows 系统 URL 处理器打开控制台地址。
func openBrowserURL(ctx context.Context, target string) error {
	command := exec.CommandContext(ctx, "rundll32.exe", "url.dll,FileProtocolHandler", target)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command.Start()
}
