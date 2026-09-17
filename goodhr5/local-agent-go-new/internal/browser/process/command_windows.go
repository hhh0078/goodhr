//go:build windows

// Package process 文件作用：隐藏 Windows Worker 子进程窗口并停止完整进程树。
package process

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// configureProcess 隐藏 Windows Node Worker 控制台窗口。
func configureProcess(command *exec.Cmd) {
	if command == nil {
		return
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
}

// configureOutput 把 GUI 安装版的 Worker 输出交给内部日志接收器，避免无效终端句柄让 Node 退出。
func configureOutput(command *exec.Cmd, lineWriter *lineSinkWriter) {
	command.Stdout = lineWriter
	command.Stderr = lineWriter
}

// stopProcess 使用 taskkill 停止 Node Worker 和它启动的浏览器进程树。
func stopProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	command := exec.Command("taskkill.exe", "/PID", strconv.Itoa(process.Pid), "/T", "/F")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("停止 Worker 进程树失败：%w，输出：%s", err, string(output))
	}
	return nil
}
