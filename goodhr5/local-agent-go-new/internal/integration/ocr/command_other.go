//go:build !windows

// Package ocr 文件作用：提供非 Windows 平台的 OCR 子进程配置入口。
package ocr

import "os/exec"

// configureCommand 保留非 Windows 平台默认后台子进程行为。
func configureCommand(command *exec.Cmd) {}
