//go:build !windows

// Package profile 文件作用：在 macOS 和 Linux 上判断 Chromium 单例锁中的进程是否仍存活。
package profile

import (
	"errors"
	"syscall"
)

// processAlive 安全判断单例锁中的进程是否仍存在；权限不足时按仍存活处理。
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
