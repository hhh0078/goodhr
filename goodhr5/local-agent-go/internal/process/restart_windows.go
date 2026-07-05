//go:build windows

// Package process 负责端口探测和本地进程辅助管理。
package process

import (
	"fmt"
	"os/exec"
	"strconv"
)

// StopOtherInstances 关闭除当前进程外的同名 Windows 进程。
// imageName 为进程镜像名，currentPID 为当前进程 ID。
func StopOtherInstances(imageName string, currentPID int) error {
	if imageName == "" || currentPID <= 0 {
		return nil
	}
	cmd := exec.Command("taskkill", "/IM", imageName, "/T", "/F", "/FI", "PID ne "+strconv.Itoa(currentPID))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w：%s", err, string(output))
	}
	return nil
}
