//go:build !windows

// Package runtime 文件作用：保留非 Windows 安装命令的默认窗口行为。
package runtime

import "os/exec"

// configureRuntimeCommand 在非 Windows 系统使用默认子进程设置。
func configureRuntimeCommand(command *exec.Cmd) {}
