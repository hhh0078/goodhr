//go:build windows

// Package ocr 文件作用：隐藏 Windows OCR 子进程的控制台，防止终端抢焦点或被关闭后影响识别。
package ocr

import (
	"os/exec"
	"syscall"
)

// configureCommand 让 OCR 在后台运行，不创建或继承可见控制台。
func configureCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
