//go:build windows

// Package device 文件作用：在 Windows 后台读取设备信息时隐藏临时终端窗口。
package device

import (
	"os/exec"
	"syscall"
)

// commandOutput 隐藏 Windows 控制台窗口并返回设备查询结果。
func commandOutput(name string, args ...string) ([]byte, error) {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return command.Output()
}
