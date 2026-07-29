//go:build darwin

// Package console 文件作用：使用 macOS 默认浏览器打开 GoodHR 控制台。
package console

import (
	"context"
	"os/exec"
)

// openBrowserURL 使用 macOS open 命令打开控制台地址。
func openBrowserURL(ctx context.Context, target string) error {
	return exec.CommandContext(ctx, "open", target).Start()
}
