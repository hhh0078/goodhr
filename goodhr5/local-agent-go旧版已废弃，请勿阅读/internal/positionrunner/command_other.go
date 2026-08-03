//go:build !windows

// Package positionrunner 提供非 Windows 下岗位运行运行辅助命令配置。
package positionrunner

import "os/exec"

// hideCommandWindow 在非 Windows 系统下无需隐藏命令窗口。
// cmd 为即将启动的命令对象。
func hideCommandWindow(cmd *exec.Cmd) {}
