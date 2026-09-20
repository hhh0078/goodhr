//go:build windows

// Package runtime 文件作用：在 Windows 后台执行运行环境检查，并隐藏短命令的终端窗口。
package runtime

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// commandOutput 隐藏 Windows 控制台窗口并返回命令输出。
func commandOutput(name string, args ...string) ([]byte, error) {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	return command.Output()
}

// commandCombinedOutput 使用隐藏窗口执行短检查命令，并附加受控环境变量。
func commandCombinedOutput(ctx context.Context, environment []string, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	command.Env = append(os.Environ(), environment...)
	return command.CombinedOutput()
}
