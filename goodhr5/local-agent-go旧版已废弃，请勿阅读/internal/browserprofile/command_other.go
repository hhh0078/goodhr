// Package browserprofile 提供非 Windows 下浏览器 Profile 初始化命令辅助。
//go:build !windows

package browserprofile

import "os/exec"

// browserProfileCommand 创建 Profile 初始化命令。
// name 为命令名称，args 为命令参数。
func browserProfileCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
