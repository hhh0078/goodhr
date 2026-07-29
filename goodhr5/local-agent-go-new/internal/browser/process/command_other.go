//go:build !windows

// Package process 文件作用：配置非 Windows Worker 子进程并发送优雅停止信号。
package process

import (
	"os"
	"os/exec"
)

// configureProcess 在非 Windows 系统沿用默认子进程设置。
func configureProcess(command *exec.Cmd) {}

// stopProcess 向非 Windows Worker 发送中断信号。
func stopProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	return process.Signal(os.Interrupt)
}
