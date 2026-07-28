// Package notification 提供 macOS 任务成功和失败提示音能力。
package notification

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

const (
	successSound = "/System/Library/Sounds/Glass.aiff"
	failureSound = "/System/Library/Sounds/Basso.aiff"
)

// Notifier 使用 macOS 系统 afplay 同步播放提示音。
type Notifier struct{}

// PlaySuccess 播放一次成功提示音。
func (Notifier) PlaySuccess(ctx context.Context) error {
	return play(ctx, successSound)
}

// PlayFailure 播放一次失败提示音。
func (Notifier) PlayFailure(ctx context.Context) error {
	return play(ctx, failureSound)
}

// play 校验系统播放器和音频文件后同步播放。
func play(ctx context.Context, soundPath string) error {
	if _, err := os.Stat(soundPath); err != nil {
		return fmt.Errorf("系统提示音不可用：%w", err)
	}
	player, err := exec.LookPath("afplay")
	if err != nil {
		return fmt.Errorf("系统没有找到 afplay：%w", err)
	}
	if err := exec.CommandContext(ctx, player, soundPath).Run(); err != nil {
		return fmt.Errorf("播放系统提示音失败：%w", err)
	}
	return nil
}
