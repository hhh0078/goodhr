// Package browserprofile 提供 Windows 下浏览器 Profile 初始化命令辅助。
//go:build windows

package browserprofile

import (
	"os/exec"
	"syscall"
)

// browserProfileCommand 创建不会弹出黑色终端窗口的 Profile 初始化命令。
// name 为命令名称，args 为命令参数。
func browserProfileCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	return cmd
}
