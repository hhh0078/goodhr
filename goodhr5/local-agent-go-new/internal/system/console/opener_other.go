//go:build !darwin && !windows

// Package console 文件作用：使用桌面系统默认浏览器打开 GoodHR 控制台。
package console

import (
	"context"
	"os/exec"
)

// openBrowserURL 使用 xdg-open 打开控制台地址。
func openBrowserURL(ctx context.Context, target string) error {
	return exec.CommandContext(ctx, "xdg-open", target).Start()
}
