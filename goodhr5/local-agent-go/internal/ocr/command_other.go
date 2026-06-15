//go:build !windows

// Package ocr 提供非 Windows 下 OCR 子进程启动辅助。
package ocr

import "os/exec"

// hideCommandWindow 在非 Windows 系统下无需隐藏控制台窗口。
// cmd 为即将启动的命令对象。
func hideCommandWindow(cmd *exec.Cmd) {}
