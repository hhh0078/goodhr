//go:build windows

// Package profile 文件作用：在 Windows 上判断 Chromium 单例锁中的进程是否仍存活。
package profile

import (
	"errors"
	"syscall"
)

const windowsStillActive = 259

// processAlive 安全判断单例锁中的 Windows 进程是否仍存在；权限不足时按仍存活处理。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer syscall.CloseHandle(handle)
	var exitCode uint32
	if err = syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return true
	}
	return exitCode == windowsStillActive
}
