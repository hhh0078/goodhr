// Package auto_reply 本文件负责自动回复轮询、单轮上限和连续错误策略。
package auto_reply

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// unreadConversationScanner 定义检查点读取未读会话所需的最小平台能力。
type unreadConversationScanner interface {
	ScanUnreadConversations(context.Context, model.Browser, model.Config) ([]model.Conversation, error)
}

// runLoop 持续读取实时开关和岗位快照，关闭后处理完当前会话再退出。
func (f *Flow) runLoop(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime, replyRuntime model.AutoReplyRuntime, stats *shared.Stats) error {
	errorPolicy := &shared.ConsecutiveErrorPolicy{}
	for {
		if shared.GracefulStopRequested(ctx) {
			return nil
		}
		status, err := f.Cloud.AutoReplyStatus(ctx, credentials(prepared), prepared.Position.ID)
		if err != nil {
			return fmt.Errorf("读取自动回复实时开关失败：%w", err)
		}
		if !status.Enabled {
			shared.ReportProgress(f.Logger, prepared.Request.TaskID, "自动回复已经关闭，我处理完当前会话就停下来了")
			return nil
		}
		positions, err := f.Cloud.AutoReplySnapshots(ctx, credentials(prepared), prepared.Platform.ID)
		if err != nil {
			return fmt.Errorf("读取自动回复岗位列表失败：%w", err)
		}
		if len(positions) == 0 {
			return fmt.Errorf("%s暂时没有已开启自动回复的岗位", prepared.Platform.Name)
		}
		limit, waitSeconds := checkpointSettings(positions, prepared.Position.ID)
		_, err = f.processCheckpoint(ctx, prepared, runtime, replyRuntime, positions, limit, stats, errorPolicy, true)
		if err != nil {
			return err
		}
		f.reportCheckpointStats(prepared.Request.TaskID, *stats)
		if err = waitForNextCheckpoint(ctx, waitSeconds); err != nil {
			return err
		}
	}
}

// processCheckpoint 按页面顺序处理单轮最多三个未读会话。
func (f *Flow) processCheckpoint(ctx context.Context, prepared shared.PreparedTask, scanner unreadConversationScanner, replyRuntime model.AutoReplyRuntime, positions []cloud.AutoReplyPositionSnapshot, limit int, stats *shared.Stats, errorPolicy *shared.ConsecutiveErrorPolicy, stopWhenStartedPositionDisabled bool) (int, error) {
	conversations, err := scanner.ScanUnreadConversations(ctx, f.Browser, prepared.Platform)
	if err != nil {
		wrapped := fmt.Errorf("读取未读候选人列表失败：%w", err)
		f.log(prepared.Request.TaskID, "scan_unread_conversations", "warning", time.Now(), wrapped)
		if stopErr := errorPolicy.Record(wrapped); stopErr != nil {
			return 0, stopErr
		}
		return 0, nil
	}
	if len(conversations) == 0 {
		errorPolicy.Reset()
		return 0, nil
	}
	limit = min(max(limit, 1), defaultCheckpointLimit)
	if len(conversations) > limit {
		conversations = conversations[:limit]
	}
	processed := 0
	for _, conversation := range conversations {
		if shared.GracefulStopRequested(ctx) {
			return processed, nil
		}
		if stopWhenStartedPositionDisabled && processed > 0 {
			status, statusErr := f.Cloud.AutoReplyStatus(ctx, credentials(prepared), prepared.Position.ID)
			if statusErr != nil {
				wrapped := fmt.Errorf("复核自动回复开关失败：%w", statusErr)
				f.log(prepared.Request.TaskID, "check_auto_reply_status", "warning", time.Now(), wrapped)
				if stopErr := errorPolicy.Record(wrapped); stopErr != nil {
					return processed, stopErr
				}
				return processed, nil
			}
			if !status.Enabled {
				shared.ReportProgress(f.Logger, prepared.Request.TaskID, "自动回复已经关闭，当前会话处理完了，我先停在这里")
				return processed, nil
			}
		}
		startedAt := time.Now()
		f.log(prepared.Request.TaskID, "process_conversation", "start", startedAt, nil)
		err = f.processConversation(ctx, prepared, replyRuntime, positions, conversation, stats)
		processed++
		if err != nil {
			f.log(prepared.Request.TaskID, "process_conversation", "failed", startedAt, err)
			if stopErr := errorPolicy.Record(err); stopErr != nil {
				return processed, stopErr
			}
			continue
		}
		errorPolicy.Reset()
		f.log(prepared.Request.TaskID, "process_conversation", "success", startedAt, nil)
	}
	return processed, nil
}

// checkpointSettings 返回启动岗位配置的单轮上限和轮询间隔。
func checkpointSettings(items []cloud.AutoReplyPositionSnapshot, startedPositionID string) (int, int) {
	limit := defaultCheckpointLimit
	waitSeconds := 3
	for _, item := range items {
		if item.Position.ID != startedPositionID {
			continue
		}
		if item.Config.MaxThreadsPerCheckpoint > 0 {
			limit = min(item.Config.MaxThreadsPerCheckpoint, defaultCheckpointLimit)
		}
		if item.Config.PollIntervalSeconds > 0 {
			waitSeconds = item.Config.PollIntervalSeconds
		}
		break
	}
	return limit, max(waitSeconds, 1)
}

// waitForNextCheckpoint 等待下一轮时同时响应任务取消和优雅停止。
func waitForNextCheckpoint(ctx context.Context, seconds int) error {
	timer := time.NewTimer(time.Duration(max(seconds, 1)) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-shared.GracefulStopSignal(ctx):
		return nil
	case <-timer.C:
		return nil
	}
}
