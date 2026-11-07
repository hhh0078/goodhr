// Package auto_reply 本文件负责自动回复轮询、单轮上限和连续错误策略。
package auto_reply

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// unreadConversationScanner 定义检查点读取未读会话所需的最小平台能力。
type unreadConversationScanner interface {
	ScanUnreadConversations(context.Context, model.Browser, model.Config) ([]model.Conversation, error)
}

// openAutoReplyConversation 保存当前仍然打开的候选人和页面快照，供下一轮直接检查新消息。
type openAutoReplyConversation struct {
	conversation model.Conversation
	snapshot     model.AutoReplyConversationSnapshot
	scanNext     bool // 连续优先处理当前会话后，下一轮须给其他未读一次机会。
}

// runLoop 持续读取实时开关和岗位快照，关闭后处理完当前会话再退出。
func (f *Flow) runLoop(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime, replyRuntime model.AutoReplyRuntime, stats *shared.Stats) error {
	errorPolicy := &shared.ConsecutiveErrorPolicy{}
	var openConversation *openAutoReplyConversation
	historyRefreshQueued := false
	for {
		if shared.GracefulStopRequested(ctx) {
			return nil
		}
		positions, err := f.Cloud.AutoReplySnapshots(ctx, credentials(prepared), prepared.Platform.ID)
		if err != nil {
			wrapped := fmt.Errorf("读取自动回复岗位列表失败：%w", err)
			f.log(prepared.Request.TaskID, "load_positions", "warning", time.Now(), wrapped)
			if stopErr := errorPolicy.Record(wrapped); stopErr != nil {
				return stopErr
			}
			if err = waitForNextCheckpoint(ctx, 3); err != nil {
				return err
			}
			continue
		}
		if len(positions) == 0 {
			errorPolicy.Reset()
			shared.ReportProgress(f.Logger, prepared.Request.TaskID, prepared.Platform.Name+"暂时没有已开启自动回复的岗位，我先等一小会儿")
			if err := waitForNextCheckpoint(ctx, 3); err != nil {
				return err
			}
			continue
		}
		if !historyRefreshQueued {
			if err := f.queueHistoryRefresh(ctx, prepared, positions); err != nil {
				return err
			}
			historyRefreshQueued = true
		}
		limit, waitSeconds := checkpointSettings(positions, prepared.Position.ID)
		succeededBefore := stats.Succeeded
		_, openConversation, err = f.processCheckpoint(
			ctx, prepared, runtime, replyRuntime, positions, limit, stats, errorPolicy, openConversation,
		)
		if err != nil {
			return err
		}
		f.reportCheckpointStats(prepared.Request.TaskID, *stats)
		if openConversation != nil {
			if stats.Succeeded > succeededBefore && lastInboundWasResume(openConversation.snapshot.Messages) {
				// 已签收的新卡片有独立持久待办，下一轮直接核对下载；普通会话轮换和失败退避照常执行。
				continue
			}
			waitSeconds = min(waitSeconds, int(defaultAutoReplyMessagePollInterval/time.Second))
		}
		if err = waitForNextCheckpoint(ctx, waitSeconds); err != nil {
			return err
		}
	}
}

// lastInboundWasResume 检查刚处理的最后一条非系统消息，避免旧简历卡让后续普通问答跳过空闲轮询。
func lastInboundWasResume(messages []model.ConversationMessage) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Direction != "system" {
			return m.Direction == "candidate" && m.MessageType == "resume"
		}
	}
	return false
}

// queueHistoryRefresh 每次任务启动只登记一次旧版本重采，后续轮询直接复用持久采集意图。
func (f *Flow) queueHistoryRefresh(ctx context.Context, prepared shared.PreparedTask, positions []cloud.AutoReplyPositionSnapshot) error {
	if f.Cloud != nil && f.Cloud.LocalAutoReplyEnabled() && prepared.Platform.Behavior.AutoReplyMessageIdentityVersion > 0 {
		scope := cloud.AutoReplyHistoryRefreshScope{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, Version: prepared.Platform.Behavior.AutoReplyMessageIdentityVersion}
		for _, position := range positions {
			if position.Config.Enabled {
				scope.PositionIDs = append(scope.PositionIDs, position.Position.ID)
			}
		}
		if target := prepared.Request.AutoReplyTarget; target != nil {
			scope.Target = &cloud.AutoReplyCaptureIntent{PlatformThreadID: target.PlatformThreadID, PlatformCandidateID: target.PlatformCandidateID, CandidateName: target.CandidateName}
		}
		if err := f.Cloud.QueueAutoReplyHistoryRefresh(ctx, credentials(prepared), scope); err != nil {
			return fmt.Errorf("登记旧聊天历史补采失败：%w", err)
		}
	}
	return nil
}

// processCheckpoint 按页面顺序处理单轮最多三个未读会话。
func (f *Flow) processCheckpoint(ctx context.Context, prepared shared.PreparedTask, scanner unreadConversationScanner, replyRuntime model.AutoReplyRuntime, positions []cloud.AutoReplyPositionSnapshot, limit int, stats *shared.Stats, errorPolicy *shared.ConsecutiveErrorPolicy, openConversation *openAutoReplyConversation) (int, *openAutoReplyConversation, error) {
	localQueue := f.Cloud != nil && f.Cloud.LocalAutoReplyEnabled()
	checkpointHadError := false
	openVerified, openHasNew := false, false
	openThreadID := ""
	if openConversation != nil && !targetConversationAllowed(prepared.Request.AutoReplyTarget, openConversation.conversation) {
		openConversation = nil
	}
	if openConversation != nil {
		item := openConversation.conversation
		openThreadID = persistentConversationThreadID(item.PlatformThreadID, item.Key)
		stillOpen, hasNewMessage, err := f.openConversationHasNewMessage(ctx, prepared, replyRuntime, openConversation)
		if err != nil {
			f.log(prepared.Request.TaskID, "check_open_conversation", "warning", time.Now(), err)
			checkpointHadError = true
			if stopErr := errorPolicy.Record(err); stopErr != nil {
				return 0, openConversation, stopErr
			}
		} else if !stillOpen {
			openConversation = nil
		} else {
			openVerified, openHasNew = true, hasNewMessage
		}
	}

	conversations := make([]model.Conversation, 0, limit)
	seen := make(map[string]bool)
	openResumeDue := false
	var reviews map[string]cloud.AutoReplyListReview
	if localQueue {
		pending, err := f.Cloud.ListPendingAutoReplyConversations(ctx, credentials(prepared), prepared.Platform.ID, prepared.Request.PlatformAccountID)
		if err != nil {
			return 0, openConversation, err
		}
		for _, saved := range pending {
			positionID := saved.PositionID
			if saved.CaptureIntent != nil {
				positionID = saved.CaptureIntent.PositionID
			}
			enabled := positionID == "" && saved.CaptureIntent != nil && len(positions) > 0
			for _, position := range positions {
				if position.Position.ID == positionID {
					enabled = true
					break
				}
			}
			if !enabled || seen[saved.PlatformThreadID] {
				continue
			}
			seen[saved.PlatformThreadID] = true
			item := model.Conversation{Key: saved.PlatformThreadID, PlatformThreadID: saved.PlatformThreadID, PlatformAccountID: saved.PlatformAccountID, Name: saved.CandidateName, CommunicationPosition: saved.PagePositionText}
			if intent := saved.CaptureIntent; intent != nil {
				item.PlatformThreadID, item.PlatformCandidateID = intent.PlatformThreadID, intent.PlatformCandidateID
				item.Name, item.CommunicationPosition = intent.CandidateName, intent.PagePositionText
				item.ReplyAfter, item.WitnessDigest = intent.ReplyAfter, intent.WitnessDigest
			}
			if openVerified && openThreadID != "" && saved.PlatformThreadID == openThreadID && targetConversationAllowed(prepared.Request.AutoReplyTarget, item) {
				openResumeDue = autoReplyResumeRetryDue(saved.ResumeRetry, time.Now())
			}
			conversations = append(conversations, item)
		}
	}
	yieldOpen := openVerified && localQueue && prepared.Request.AutoReplyTarget == nil && openConversation.scanNext
	reuseOpenConversation := openVerified && !yieldOpen && (openHasNew || openResumeDue)
	if yieldOpen && openHasNew {
		// 已打开的聊天可能自动标为已读。先留可恢复的采集意图，切到其他人后也能找回这条来信。
		item := openConversation.conversation
		item.PlatformCandidateID = firstNonEmpty(openConversation.snapshot.PlatformCandidateID, item.PlatformCandidateID)
		intent := captureIntentForConversation(prepared, item)
		err := f.Cloud.BeginAutoReplyCapture(ctx, credentials(prepared), prepared.Platform.ID, firstNonEmpty(prepared.Request.PlatformAccountID, item.PlatformAccountID), openThreadID, intent)
		if errors.Is(err, storage.ErrAutoReplyCandidateDeleted) {
			// 被删除的身份只有当前窗口能验证新入站，交给原来的删除后重联路径。
			reuseOpenConversation = true
		} else if err != nil {
			return 0, openConversation, err
		} else if !seen[openThreadID] {
			seen[openThreadID] = true
			conversations = append(conversations, item)
		}
	}
	if reuseOpenConversation {
		conversations = []model.Conversation{openConversation.conversation}
		openConversation.scanNext = true
	} else {
		unread, err := scanner.ScanUnreadConversations(ctx, f.Browser, prepared.Platform)
		if err != nil {
			wrapped := fmt.Errorf("读取候选人会话列表失败：%w", err)
			f.log(prepared.Request.TaskID, "scan_unread_conversations", "warning", time.Now(), wrapped)
			if stopErr := errorPolicy.Record(wrapped); stopErr != nil {
				return 0, openConversation, stopErr
			}
			return 0, openConversation, nil
		}
		if prepared.Platform.Behavior.AutoReplyAllContactInbox {
			unread, reviews, err = f.reviewAllContacts(ctx, prepared, unread)
			if err != nil {
				return 0, openConversation, err
			}
		}
		for _, item := range unread {
			id := persistentConversationThreadID(item.PlatformThreadID, item.Key)
			if id == "" || !seen[id] {
				conversations = append(conversations, item)
				seen[id] = true
			}
		}
		prioritizeDueListReview(conversations, reviews)
		if yieldOpen {
			openConversation.scanNext = false
			// 即使每轮名额只有一个，也让其他人的待办和新来信排在连续处理的会话前面。
			i := slices.IndexFunc(conversations, func(item model.Conversation) bool {
				return persistentConversationThreadID(item.PlatformThreadID, item.Key) == openThreadID
			})
			if i >= 0 {
				current := conversations[i]
				copy(conversations[i:], conversations[i+1:])
				conversations[len(conversations)-1] = current
			}
		}
	}
	if target := prepared.Request.AutoReplyTarget; target != nil {
		allowed := make([]model.Conversation, 0, 1)
		for _, conversation := range conversations {
			if targetConversationAllowed(target, conversation) {
				allowed = append(allowed, conversation)
			}
		}
		conversations = allowed
	}
	if len(conversations) == 0 {
		if !checkpointHadError {
			errorPolicy.Reset()
		}
		return 0, openConversation, nil
	}
	limit = min(max(limit, 1), defaultCheckpointLimit)
	processed := 0
	for _, conversation := range conversations {
		if processed >= limit {
			break
		}
		if shared.GracefulStopRequested(ctx) {
			return processed, openConversation, nil
		}
		threadID := persistentConversationThreadID(conversation.PlatformThreadID, conversation.Key)
		review, hasReview := reviews[threadID]
		if hasReview && conversation.ReplyAfter.IsZero() {
			conversation.ReplyAfter, conversation.WitnessDigest = review.After, review.WitnessDigest
		}
		current := &openAutoReplyConversation{conversation: conversation}
		if openConversation != nil && persistentConversationThreadID(conversation.PlatformThreadID, conversation.Key) == persistentConversationThreadID(openConversation.conversation.PlatformThreadID, openConversation.conversation.Key) {
			if !reuseOpenConversation {
				// 扫描会话列表可能收起原聊天；复用旧快照前重新确认，明确未打开才转入正常恢复入口。
				_, stillOpen, err := replyRuntime.ReadOpenAutoReplyLatestMessage(ctx, f.Browser, prepared.Platform, openConversation.snapshot)
				if err != nil {
					f.log(prepared.Request.TaskID, "check_open_after_scan", "warning", time.Now(), err)
					if stopErr := errorPolicy.Record(err); stopErr != nil {
						return processed, openConversation, stopErr
					}
					continue
				}
				if !stillOpen {
					openConversation = nil
				}
			}
			if openConversation != nil {
				current = openConversation
			}
		}
		startedAt := time.Now()
		f.log(prepared.Request.TaskID, "process_conversation", "start", startedAt, nil)
		countBefore := stats.Processed
		err := f.processConversation(ctx, prepared, replyRuntime, positions, conversation, &current.snapshot, stats)
		if hasReview && (err == nil || errors.Is(err, storage.ErrAutoReplyCandidateDeleted)) {
			scope := ""
			if target := prepared.Request.AutoReplyTarget; target != nil {
				scope = target.PlatformThreadID
			}
			if ackErr := f.Cloud.AckAutoReplyList(ctx, credentials(prepared), prepared.Platform.ID, prepared.Request.PlatformAccountID, scope, review); ackErr != nil {
				return processed, openConversation, ackErr
			}
		}
		// 未核实到删除后的新入站时只计跳过，保留名额给列表后面的会话。
		if errors.Is(err, storage.ErrAutoReplyCandidateDeleted) {
			stats.Processed = countBefore
			stats.Skipped++
			if openConversation == current {
				openConversation = nil
			}
			f.log(prepared.Request.TaskID, "process_conversation", "skipped", startedAt, nil)
			continue
		}
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
		// 采集成功后，持续打开的会话改用已落盘的消息游标。列表初次观测的时间仅约束本次采集。
		current.conversation.ReplyAfter, current.conversation.WitnessDigest = time.Time{}, ""
		errorPolicy.Reset()
		f.log(prepared.Request.TaskID, "process_conversation", "success", startedAt, nil)
	}
	return processed, openConversation, nil
}

// openConversationHasNewMessage 仅在聊天框仍然打开时比较最新消息和云端游标，不为轮询重新打开旧会话。
func (f *Flow) openConversationHasNewMessage(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, current *openAutoReplyConversation) (bool, bool, error) {
	if current == nil || current.snapshot.CandidateName == "" {
		return false, false, nil
	}
	latest, stillOpen, err := runtime.ReadOpenAutoReplyLatestMessage(ctx, f.Browser, prepared.Platform, current.snapshot)
	if err != nil {
		return stillOpen, false, fmt.Errorf("检查当前聊天框新消息失败：%w", err)
	}
	if !stillOpen {
		return false, false, nil
	}
	state, err := f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{
		PlatformID: prepared.Platform.ID, PlatformAccountID: firstNonEmpty(prepared.Request.PlatformAccountID, current.snapshot.PlatformAccountID, current.conversation.PlatformAccountID),
		PlatformCandidateID: firstNonEmpty(current.snapshot.PlatformCandidateID, current.conversation.PlatformCandidateID),
		PlatformThreadID: persistentConversationThreadID(
			firstNonEmpty(current.snapshot.PlatformThreadID, current.conversation.PlatformThreadID),
			current.conversation.Key,
		),
		Phone: current.snapshot.Phone,
	})
	if err != nil {
		return true, false, fmt.Errorf("读取当前候选人消息游标失败：%w", err)
	}
	if strings.ToLower(strings.TrimSpace(latest.Direction)) != "candidate" {
		return true, false, nil
	}
	if state.Conversation != nil && f.Cloud.LocalAutoReplyEnabled() {
		pending, err := f.Cloud.PendingAutoReplyMessages(ctx, credentials(prepared), state.Conversation.ID)
		if err != nil {
			return true, false, err
		}
		if len(pending) > 0 {
			return true, true, nil
		}
	}
	latestKeys := cleanMessageKeys([]string{firstNonEmpty(latest.PlatformMessageID, latest.Key, messageFingerprint(latest, 0))})
	if len(latestKeys) == 0 {
		return true, false, nil
	}
	latestKey := latestKeys[0]
	for _, knownKey := range knownMessageKeysFromState(state) {
		if latestKey == knownKey {
			return true, false, nil
		}
	}
	return true, latestKey != "", nil
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
