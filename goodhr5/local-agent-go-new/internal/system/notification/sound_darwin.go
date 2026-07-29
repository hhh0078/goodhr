//go:build darwin

// Package notification 文件作用：使用 macOS afplay 播放任务提示音。
package notification

import (
	"fmt"
	"os/exec"
)

// soundCommand 创建 macOS 成功或失败提示音命令。
func soundCommand(kind string) (*exec.Cmd, error) {
	player, err := exec.LookPath("afplay")
	if err != nil {
		return nil, fmt.Errorf("系统没有找到 afplay：%w", err)
	}
	soundPath := "/System/Library/Sounds/Glass.aiff"
	if kind == "failure" {
		soundPath = "/System/Library/Sounds/Basso.aiff"
	}
	return exec.Command(player, soundPath), nil
}
