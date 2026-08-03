//go:build !windows

// Package process 文件作用：提供非 Windows 系统的进程结束兼容入口。
package process

import "os"

// TerminateTree 在非 Windows 系统结束指定进程。
// pid 为目标进程 ID。
func TerminateTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	target, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return target.Kill()
}
