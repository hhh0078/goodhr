//go:build !windows

// Package runtime 文件作用：在非 Windows 系统执行运行环境检查命令。
package runtime

import "os/exec"

// commandOutput 执行命令并返回标准输出。
func commandOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}
