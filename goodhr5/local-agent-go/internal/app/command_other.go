//go:build !windows

// Package app 提供非 Windows 下启动系统命令的辅助能力。
package app

import "os/exec"

// hideCommandWindow 在非 Windows 系统下无需隐藏控制台窗口。
// cmd 为即将启动的命令对象。
func hideCommandWindow(cmd *exec.Cmd) {}
