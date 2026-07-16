//go:build windows

// Package positionrunner 提供 Windows 下岗位运行运行辅助命令配置。
package positionrunner

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow 在 Windows 下隐藏岗位运行运行器启动的系统命令窗口。
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
