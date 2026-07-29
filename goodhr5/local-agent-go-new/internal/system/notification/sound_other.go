//go:build !darwin && !windows

// Package notification 文件作用：让未支持系统明确返回提示音不可用错误。
package notification

import (
	"fmt"
	"os/exec"
)

// soundCommand 为暂未支持的操作系统返回明确错误。
func soundCommand(string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("当前系统暂不支持任务提示音")
}
