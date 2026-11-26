//go:build !windows

// Package sessionfile 文件作用：在 macOS 和其他 Unix 系统限制会话目录及文件为当前用户可读写。
package sessionfile

import "os"

// protectPath 为目录设置 0700、文件设置 0600；不改变其他业务数据目录的权限。
func protectPath(path string, directory bool) error {
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	return os.Chmod(path, mode)
}
