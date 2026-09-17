//go:build !windows

// Package device 文件作用：在非 Windows 系统执行设备信息查询命令。
package device

import "os/exec"

// commandOutput 执行设备信息查询命令并返回结果。
func commandOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}
