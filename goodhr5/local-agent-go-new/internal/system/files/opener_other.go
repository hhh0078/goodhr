//go:build !darwin && !windows

// Package files 文件作用：使用桌面系统默认程序打开文件和文件目录。
package files

import (
	"context"
	"os/exec"
	"path/filepath"
)

// openPath 使用 xdg-open 打开文件。
func openPath(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "xdg-open", path).Run()
}

// revealPath 使用 xdg-open 打开文件所在目录。
func revealPath(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "xdg-open", filepath.Dir(path)).Run()
}
