//go:build !windows

// Package process 负责端口探测和本地进程辅助管理。
package process

// StopOtherInstances 在非 Windows 系统不处理旧进程。
// imageName 为进程镜像名，currentPID 为当前进程 ID。
func StopOtherInstances(imageName string, currentPID int) error {
	return nil
}

// StopGoodHRPortOwner 在非 Windows 系统不处理固定端口旧进程。
// host 和 port 为待检查的监听地址，currentPID 为当前进程 ID。
func StopGoodHRPortOwner(host string, port int, currentPID int) error {
	return nil
}
