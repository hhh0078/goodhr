//go:build windows

// Package taskrunner 提供 Windows 下任务运行辅助命令配置。
package taskrunner

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow 在 Windows 下隐藏任务运行器启动的系统命令窗口。
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
