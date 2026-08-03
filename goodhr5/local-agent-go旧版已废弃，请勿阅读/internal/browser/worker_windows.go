//go:build windows

// Package browser 提供 Windows 下的 Node Worker 进程启动辅助。
package browser

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow 在 Windows 下隐藏子进程控制台窗口。
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
