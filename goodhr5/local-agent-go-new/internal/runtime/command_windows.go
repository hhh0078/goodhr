//go:build windows

// Package runtime 文件作用：让 Windows 组件检查和安装命令在后台运行。
package runtime

import (
	"os/exec"
	"syscall"
)

// configureRuntimeCommand 隐藏安装子进程的控制台，避免 GUI 程序弹出黑色终端。
func configureRuntimeCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
