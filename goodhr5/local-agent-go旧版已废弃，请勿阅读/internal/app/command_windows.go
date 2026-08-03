//go:build windows

// Package app 提供 Windows 下启动系统命令的辅助能力。
package app

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow 在 Windows 下隐藏系统命令控制台窗口。
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
