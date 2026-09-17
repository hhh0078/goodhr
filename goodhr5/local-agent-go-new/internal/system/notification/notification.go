// Package notification 提供跨系统、非阻塞的任务成功和失败提示音能力。
package notification

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Notifier 使用当前操作系统的原生播放器启动提示音。
type Notifier struct{}

// DownloadAction 表示下载提示窗返回的用户动作。
type DownloadAction string

const (
	// DownloadDismiss 表示用户关闭提示或暂不处理。
	DownloadDismiss DownloadAction = "dismiss"
	// DownloadOpen 表示用户希望打开下载文件。
	DownloadOpen DownloadAction = "open"
	// DownloadReveal 表示用户希望在文件管理器中显示下载文件。
	DownloadReveal DownloadAction = "reveal"
)

// parseDownloadAction 从提示窗脚本的输出里解析用户选择的动作。
// 只认输出中最后一个非空行，避免系统或脚本的额外输出把结果带偏。
func parseDownloadAction(output string) DownloadAction {
	lines := strings.Split(output, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		switch strings.TrimSpace(lines[index]) {
		case string(DownloadOpen):
			return DownloadOpen
		case string(DownloadReveal):
			return DownloadReveal
		}
	}
	return DownloadDismiss
}

// PlaySuccess 播放一次成功提示音。
func (Notifier) PlaySuccess(ctx context.Context) error {
	return play(ctx, "success")
}

// PlayFailure 播放一次失败提示音。
func (Notifier) PlayFailure(ctx context.Context) error {
	return play(ctx, "failure")
}

// play 启动当前系统的提示音命令后立即返回，播放过程不阻塞任务主流程。
func play(ctx context.Context, kind string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	command, err := soundCommand(kind)
	if err != nil {
		return err
	}
	if command == nil {
		return fmt.Errorf("当前系统没有生成提示音播放命令")
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("启动系统提示音失败：%w", err)
	}
	go waitSound(command)
	return nil
}

// waitSound 回收已经启动的提示音子进程，播放失败不影响任务结果。
func waitSound(command *exec.Cmd) {
	_ = command.Wait()
}
