//go:build !windows

// Package browser 提供非 Windows 系统下的 Node Worker 进程启动辅助。
package browser

import "os/exec"

// hideCommandWindow 在非 Windows 系统下无需处理窗口隐藏。
// cmd 为即将启动的命令对象。
func hideCommandWindow(cmd *exec.Cmd) {}
