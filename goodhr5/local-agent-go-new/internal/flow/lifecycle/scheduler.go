// Package lifecycle 本文件在候选人安全边界调度主动打招呼和自动回复，保证浏览器动作始终串行。
package lifecycle

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/auto_reply"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// runGreetingWithAutoReply 运行主动打招呼，并由外层检查点在每位候选人前穿插自动回复。
func (r *Runner) runGreetingWithAutoReply(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime) (shared.Stats, error) {
	session := &auto_reply.CheckpointSession{}
	checkpoint := func(checkpointCtx context.Context) error {
		startedAt := time.Now()
		if r.logger != nil {
			r.logger.Step(prepared.Request.TaskID, "scheduler", "auto_reply_checkpoint", "start", startedAt, nil)
		}
		result, checkpointErr := r.reply.RunCheckpoint(checkpointCtx, prepared, runtime, session)
		if result.TouchedPage {
			restoreErr := runtime.InitializeGreetingPage(context.WithoutCancel(checkpointCtx), r.greeting.Browser, prepared.Platform)
			if restoreErr != nil && checkpointErr == nil {
				checkpointErr = fmt.Errorf("恢复打招呼页面失败：%w", restoreErr)
			}
		}
		if r.logger != nil {
			status := "success"
			if checkpointErr != nil {
				status = "failed"
			}
			r.logger.Step(prepared.Request.TaskID, "scheduler", "auto_reply_checkpoint", status, startedAt, checkpointErr)
		}
		return checkpointErr
	}
	return r.greeting.Run(ctx, prepared, runtime, checkpoint)
}
