// Package notification 提供跨系统、非阻塞的任务成功和失败提示音能力。
package notification

import (
	"context"
	"fmt"
	"os/exec"
)

// Notifier 使用当前操作系统的原生播放器启动提示音。
type Notifier struct{}

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
