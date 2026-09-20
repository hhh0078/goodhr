//go:build !windows

// Package runtime 文件作用：在非 Windows 系统执行运行环境检查命令。
package runtime

import (
	"context"
	"os"
	"os/exec"
)

// commandOutput 执行命令并返回标准输出。
func commandOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// commandCombinedOutput 执行短检查命令，并附加受控环境变量。
func commandCombinedOutput(ctx context.Context, environment []string, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = append(os.Environ(), environment...)
	return command.CombinedOutput()
}
