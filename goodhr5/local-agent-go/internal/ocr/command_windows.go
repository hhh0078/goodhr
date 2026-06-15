//go:build windows

// Package ocr 提供 Windows 下 OCR 子进程启动辅助。
package ocr

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow 在 Windows 下隐藏 OCR 子进程控制台窗口。
// cmd 为即将启动的命令对象。
func hideCommandWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
}
