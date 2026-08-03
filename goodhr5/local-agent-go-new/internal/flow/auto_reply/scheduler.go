// Package auto_reply 本文件提供外层调度器调用的单次自动回复检查点，不与主动打招呼主流程互相调用。
package auto_reply

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// CheckpointSession 保存同一打招呼任务穿插自动回复时的统计和连续错误状态。
type CheckpointSession struct {
	stats       shared.Stats
	errorPolicy shared.ConsecutiveErrorPolicy
}

// CheckpointResult 表示一次候选人前检查是否启用、是否碰过页面以及处理了多少会话。
type CheckpointResult struct {
	Enabled     bool
	TouchedPage bool
	Processed   int
}

// RunCheckpoint 在当前招聘页同步处理最多三个未读会话，失败连续三次前只记录并让打招呼继续。
func (f *Flow) RunCheckpoint(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime, session *CheckpointSession) (CheckpointResult, error) {
	result := CheckpointResult{}
	if session == nil {
		return result, fmt.Errorf("自动回复检查点缺少会话状态")
	}
	if shared.GracefulStopRequested(ctx) {
		return result, nil
	}
	if !prepared.Subscription.Active || !prepared.Subscription.AllowAutoReply {
		return result, nil
	}
	if f == nil || f.Cloud == nil {
		return result, fmt.Errorf("自动回复云端组件没有准备完整")
	}
	positions, err := f.Cloud.AutoReplySnapshots(ctx, credentials(prepared), prepared.Platform.ID)
	if err != nil {
		return result, f.recordCheckpointError(prepared.Request.TaskID, session, "load_positions", fmt.Errorf("读取自动回复岗位列表失败：%w", err))
	}
	if len(positions) == 0 {
		session.errorPolicy.Reset()
		return result, nil
	}
	if err = f.validateDependencies(); err != nil {
		return result, err
	}
	replyRuntime, ok := runtime.(model.AutoReplyRuntime)
	if !ok {
		return result, fmt.Errorf("%s 自动回复页面能力还没有准备完整", prepared.Platform.Name)
	}
	result.Enabled = true
	if err = f.selectCurrentPlatformPage(ctx, prepared.Platform); err != nil {
		return result, f.recordCheckpointError(prepared.Request.TaskID, session, "select_page", err)
	}
	result.TouchedPage = true
	if err = replyRuntime.InitializeAutoReplyPage(ctx, f.Browser, prepared.Platform); err != nil {
		return result, f.recordCheckpointError(prepared.Request.TaskID, session, "initialize_page", fmt.Errorf("整理自动回复页面失败：%w", err))
	}
	limit, _ := checkpointSettings(positions, prepared.Position.ID)
	result.Processed, err = f.processCheckpoint(
		ctx, prepared, runtime, replyRuntime, positions, limit, &session.stats, &session.errorPolicy, false,
	)
	if err != nil {
		return result, err
	}
	f.reportCheckpointStats(prepared.Request.TaskID, session.stats)
	return result, nil
}

// recordCheckpointError 记录检查点外层错误，只有致命错误或连续第三次同类错误才停止主任务。
func (f *Flow) recordCheckpointError(taskID string, session *CheckpointSession, step string, checkpointErr error) error {
	f.log(taskID, "checkpoint_"+step, "warning", time.Now(), checkpointErr)
	if stopErr := session.errorPolicy.Record(checkpointErr); stopErr != nil {
		return stopErr
	}
	return nil
}

// reportCheckpointStats 把穿插自动回复的会话、回复和跳过数量写入当前悬浮窗状态。
func (f *Flow) reportCheckpointStats(taskID string, stats shared.Stats) {
	status := shared.ReadAnalysis(f.Logger, taskID)
	if status == nil || status.Kind != "auto_reply" {
		return
	}
	status.ProcessedCount = stats.Processed
	status.SucceededCount = stats.Succeeded
	status.SkippedCount = stats.Skipped
	shared.ReportAnalysis(f.Logger, taskID, *status)
}
