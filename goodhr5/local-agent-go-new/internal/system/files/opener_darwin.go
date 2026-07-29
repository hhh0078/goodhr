//go:build darwin

// Package files 文件作用：使用 macOS 默认程序打开文件并在 Finder 中显示文件。
package files

import (
	"context"
	"os/exec"
)

// openPath 使用 macOS 默认程序打开文件。
func openPath(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "open", path).Run()
}

// revealPath 在 macOS Finder 中选中文件。
func revealPath(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "open", "-R", path).Run()
}
