//go:build windows

// Package notification 文件作用：使用 Windows PowerShell 播放任务提示音。
package notification

import (
	"fmt"
	"os/exec"
	"syscall"
)

// soundCommand 创建 Windows 成功或失败提示音命令。
func soundCommand(kind string) (*exec.Cmd, error) {
	player, err := lookPathAny("powershell.exe", "powershell", "pwsh.exe", "pwsh")
	if err != nil {
		return nil, fmt.Errorf("系统没有找到 PowerShell 播放器")
	}
	sound := "Asterisk"
	if kind == "failure" {
		sound = "Hand"
	}
	script := fmt.Sprintf("[System.Media.SystemSounds]::%s.Play(); Start-Sleep -Milliseconds 300", sound)
	command := exec.Command(
		player,
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		script,
	)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command, nil
}

// lookPathAny 返回第一个可用的 Windows 命令路径。
func lookPathAny(names ...string) (string, error) {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("没有找到可用命令")
}
