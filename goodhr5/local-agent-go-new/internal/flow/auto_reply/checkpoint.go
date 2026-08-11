// Package auto_reply 本文件负责自动回复轮询、单轮上限和连续错误策略。
package auto_reply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// unreadConversationScanner 定义检查点读取未读会话所需的最小平台能力。
type unreadConversationScanner interface {
	ScanUnreadConversations(context.Context, model.Browser, model.Config) ([]model.Conversation, error)
}

// openAutoReplyConversation 保存当前仍然打开的候选人和页面快照，供下一轮直接检查新消息。
type openAutoReplyConversation struct {
	conversation model.Conversation
	snapshot     model.AutoReplyConversationSnapshot
}

// runLoop 持续读取实时开关和岗位快照，关闭后处理完当前会话再退出。
func (f *Flow) runLoop(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime, replyRuntime model.AutoReplyRuntime, stats *shared.Stats) error {
	errorPolicy := &shared.ConsecutiveErrorPolicy{}
	var openConversation *openAutoReplyConversation
	for {
		if shared.GracefulStopRequested(ctx) {
			return nil
		}
		positions, err := f.Cloud.AutoReplySnapshots(ctx, credentials(prepared), prepared.Platform.ID)
		if err != nil {
			return fmt.Errorf("读取自动回复岗位列表失败：%w", err)
		}
		if len(positions) == 0 {
			shared.ReportProgress(f.Logger, prepared.Request.TaskID, prepared.Platform.Name+"暂时没有已开启自动回复的岗位，我先等一小会儿")
			if err := waitForNextCheckpoint(ctx, 3); err != nil {
				return err
			}
			continue
		}
		limit, waitSeconds := checkpointSettings(positions, prepared.Position.ID)
		_, openConversation, err = f.processCheckpoint(
			ctx, prepared, runtime, replyRuntime, positions, limit, stats, errorPolicy, openConversation,
		)
		if err != nil {
			return err
		}
		f.reportCheckpointStats(prepared.Request.TaskID, *stats)
		if openConversation != nil {
			waitSeconds = min(waitSeconds, int(defaultAutoReplyMessagePollInterval/time.Second))
		}
		if err = waitForNextCheckpoint(ctx, waitSeconds); err != nil {
			return err
		}
	}
}

// processCheckpoint 按页面顺序处理单轮最多三个未读会话。
func (f *Flow) processCheckpoint(ctx context.Context, prepared shared.PreparedTask, scanner unreadConversationScanner, replyRuntime model.AutoReplyRuntime, positions []cloud.AutoReplyPositionSnapshot, limit int, stats *shared.Stats, errorPolicy *shared.ConsecutiveErrorPolicy, openConversation *openAutoReplyConversation) (int, *openAutoReplyConversation, error) {
	conversations := make([]model.Conversation, 0, limit)
	reuseOpenConversation := false
	if openConversation != nil {
		hasNewMessage, err := f.openConversationHasNewMessage(ctx, prepared, replyRuntime, openConversation)
		if err != nil {
			f.log(prepared.Request.TaskID, "check_open_conversation", "warning", time.Now(), err)
		} else if hasNewMessage {
			conversations = append(conversations, openConversation.conversation)
			reuseOpenConversation = true
		}
	}
	if !reuseOpenConversation {
		unreadConversations, err := scanner.ScanUnreadConversations(ctx, f.Browser, prepared.Platform)
		if err != nil {
			wrapped := fmt.Errorf("读取未读候选人列表失败：%w", err)
			f.log(prepared.Request.TaskID, "scan_unread_conversations", "warning", time.Now(), wrapped)
			if stopErr := errorPolicy.Record(wrapped); stopErr != nil {
				return 0, openConversation, stopErr
			}
			return 0, openConversation, nil
		}
		conversations = unreadConversations
	}
	if len(conversations) == 0 {
		errorPolicy.Reset()
		return 0, openConversation, nil
	}
	limit = min(max(limit, 1), defaultCheckpointLimit)
	if len(conversations) > limit {
		conversations = conversations[:limit]
	}
	processed := 0
	for _, conversation := range conversations {
		if shared.GracefulStopRequested(ctx) {
			return processed, openConversation, nil
		}
		current := &openAutoReplyConversation{conversation: conversation}
		if reuseOpenConversation {
			current = openConversation
		}
		startedAt := time.Now()
		f.log(prepared.Request.TaskID, "process_conversation", "start", startedAt, nil)
		err := f.processConversation(ctx, prepared, replyRuntime, positions, conversation, &current.snapshot, stats)
		processed++
		if current.snapshot.CandidateName != "" {
			openConversation = current
		}
		if err != nil {
			f.log(prepared.Request.TaskID, "process_conversation", "failed", startedAt, err)
			if stopErr := errorPolicy.Record(err); stopErr != nil {
				return processed, openConversation, stopErr
			}
			continue
		}
		errorPolicy.Reset()
		f.log(prepared.Request.TaskID, "process_conversation", "success", startedAt, nil)
	}
	return processed, openConversation, nil
}

// openConversationHasNewMessage 比较当前聊天框最后一条消息和云端游标，发现候选人新消息时优先继续当前会话。
func (f *Flow) openConversationHasNewMessage(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, current *openAutoReplyConversation) (bool, error) {
	if current == nil || current.snapshot.CandidateName == "" {
		return false, nil
	}
	state, err := f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{
		PlatformID: prepared.Platform.ID, PlatformAccountID: firstNonEmpty(current.snapshot.PlatformAccountID, current.conversation.PlatformAccountID),
		PlatformCandidateID: firstNonEmpty(current.snapshot.PlatformCandidateID, current.conversation.PlatformCandidateID),
		PlatformThreadID:    firstNonEmpty(current.snapshot.PlatformThreadID, current.conversation.PlatformThreadID, current.conversation.Key),
		Phone:               current.snapshot.Phone,
	})
	if err != nil {
		return false, fmt.Errorf("读取当前候选人消息游标失败：%w", err)
	}
	latest, err := runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, current.snapshot)
	if err != nil {
		return false, fmt.Errorf("检查当前聊天框新消息失败：%w", err)
	}
	if strings.ToLower(strings.TrimSpace(latest.Direction)) != "candidate" {
		return false, nil
	}
	latestKeys := cleanMessageKeys([]string{firstNonEmpty(latest.PlatformMessageID, latest.Key, messageFingerprint(latest, 0))})
	if len(latestKeys) == 0 {
		return false, nil
	}
	latestKey := latestKeys[0]
	for _, knownKey := range knownMessageKeysFromState(state) {
		if latestKey == knownKey {
			return false, nil
		}
	}
	return latestKey != "", nil
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
