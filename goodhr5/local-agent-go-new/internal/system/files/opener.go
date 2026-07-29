// Package files 提供 macOS 打开文件和在 Finder 中显示文件的系统能力。
package files

import (
	"context"
	"fmt"
	"os"
)

// Open 使用 macOS 默认程序打开文件。
func Open(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("文件不存在：%w", err)
	}
	return openPath(ctx, path)
}

// Reveal 在 Finder 中显示指定文件。
func Reveal(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("文件不存在：%w", err)
	}
	return revealPath(ctx, path)
}
