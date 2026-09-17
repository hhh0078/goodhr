//go:build !windows

// Package process 文件作用：配置非 Windows Worker 子进程并发送优雅停止信号。
package process

import (
	"io"
	"os"
	"os/exec"
)

// configureProcess 在非 Windows 系统沿用默认子进程设置。
func configureProcess(command *exec.Cmd) {}

// configureOutput 同时保留非 Windows 终端输出并交给内部日志接收器。
func configureOutput(command *exec.Cmd, lineWriter *lineSinkWriter) {
	command.Stdout = io.MultiWriter(os.Stdout, lineWriter)
	command.Stderr = io.MultiWriter(os.Stderr, lineWriter)
}

// stopProcess 向非 Windows Worker 发送中断信号。
func stopProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	return process.Signal(os.Interrupt)
}
