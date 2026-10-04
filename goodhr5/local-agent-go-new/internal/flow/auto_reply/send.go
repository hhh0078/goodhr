// Package auto_reply 本文件负责发送前复核、发送后回读和本地重复发送保护。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type monitoredReplyResult struct {
	decision ReplyDecision
	err      error
}

// replyWhileMonitoring 等待 AI 决策时每三秒复核最新消息，发现变化后只废弃旧结果，不并发操作页面流程。
func (f *Flow) replyWhileMonitoring(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, snapshot model.AutoReplyConversationSnapshot, input ReplyContext) (ReplyDecision, bool, error) {
	resultChannel := make(chan monitoredReplyResult, 1)
	go func() {
		decision, err := f.Responder.Reply(ctx, input)
		resultChannel <- monitoredReplyResult{decision: decision, err: err}
	}()
	interval := f.messagePollInterval
	if interval <= 0 {
		interval = defaultAutoReplyMessagePollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	changed := false
	var monitorErr error
	for {
		select {
		case <-ctx.Done():
			return ReplyDecision{}, changed, ctx.Err()
		case result := <-resultChannel:
			if result.err != nil {
				return ReplyDecision{}, changed, result.err
			}
			if monitorErr != nil {
				return ReplyDecision{}, changed, monitorErr
			}
			return result.decision, changed, nil
		case <-ticker.C:
			if changed || monitorErr != nil {
				continue
			}
			unchanged, err := f.latestCandidateMessageUnchanged(ctx, runtime, prepared, snapshot, input.BasedOnMessageKey)
			if err != nil {
				monitorErr = err
				continue
			}
			changed = !unchanged
		}
	}
}

// sendVerifiedMessage 发送一条消息并回读确认，结果未知时不直接重发。
func (f *Flow) sendVerifiedMessage(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, conversation cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, basedOnMessageKey string, message string, verifyCandidateBefore bool, sequence ...int) (bool, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return false, fmt.Errorf("候选人回复内容不能为空")
	}
	if verifyCandidateBefore {
		unchanged, err := f.latestCandidateMessageUnchanged(ctx, runtime, prepared, snapshot, basedOnMessageKey)
		if err != nil || !unchanged {
			if err == nil {
				f.reportChangedMessage(prepared.Request.TaskID, snapshot.CandidateName)
				f.saveLocalReplyRecord(ctx, prepared, conversation, replyRecordHash(basedOnMessageKey, "message_changed"), "skipped")
			}
			return false, err
		}
	}
	messageSequence := 0
	if len(sequence) > 0 {
		messageSequence = sequence[0]
	}
	duplicate, err := f.replyAlreadyRecorded(ctx, prepared, conversation, basedOnMessageKey, message, messageSequence)
	if err != nil || duplicate {
		return false, err
	}
	if err = runtime.SendAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot, message); err != nil {
		return false, fmt.Errorf("发送候选人消息失败：%w", err)
	}
	latest, err := runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot)
	if err != nil {
		f.saveLocalReplyRecord(ctx, prepared, conversation, replyRecordHashWithSequence(basedOnMessageKey, message, messageSequence), "unknown")
		return false, fmt.Errorf("消息已经点击发送，但回读结果失败；我先不重发：%w", err)
	}
	direction := strings.ToLower(strings.TrimSpace(latest.Direction))
	if direction != "self" || strings.TrimSpace(latest.TextContent) != message {
		f.saveLocalReplyRecord(ctx, prepared, conversation, replyRecordHashWithSequence(basedOnMessageKey, message, messageSequence), "unknown")
		return false, fmt.Errorf("消息发送结果暂时不能确认，我先不重复发送")
	}
	messages, _, err := convertMessages([]model.ConversationMessage{latest})
	if err != nil {
		return false, err
	}
	if _, err = f.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{
		ConversationID: conversation.ID, Messages: messages,
	}); err != nil {
		return false, fmt.Errorf("消息已经发送，但云端记录没同步成功：%w", err)
	}
	f.saveLocalReplyRecord(ctx, prepared, conversation, replyRecordHashWithSequence(basedOnMessageKey, message, messageSequence), "success")
	return true, nil
}

// latestCandidateMessageUnchanged 在页面动作前确认候选人没有又发来新消息。
func (f *Flow) latestCandidateMessageUnchanged(ctx context.Context, runtime model.AutoReplyRuntime, prepared shared.PreparedTask, snapshot model.AutoReplyConversationSnapshot, basedOnMessageKey string) (bool, error) {
	latest, err := runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot)
	if err != nil {
		return false, fmt.Errorf("发送前复核候选人最新消息失败：%w", err)
	}
	if strings.ToLower(strings.TrimSpace(latest.Direction)) != "candidate" {
		return false, nil
	}
	key := firstNonEmpty(latest.PlatformMessageID, latest.Key, messageFingerprint(latest, 0))
	return key == strings.TrimSpace(basedOnMessageKey), nil
}

// latestMessageAllowsFollowup 确认第一条回复仍是页面最新消息，候选人没有在两条消息之间插入新内容。
func (f *Flow) latestMessageAllowsFollowup(ctx context.Context, runtime model.AutoReplyRuntime, prepared shared.PreparedTask, snapshot model.AutoReplyConversationSnapshot, previousContent string) (bool, error) {
	latest, err := runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot)
	if err != nil {
		return false, fmt.Errorf("发送第二条消息前复核最新聊天失败：%w", err)
	}
	return strings.EqualFold(strings.TrimSpace(latest.Direction), "self") && strings.TrimSpace(latest.TextContent) == strings.TrimSpace(previousContent), nil
}

// replyAlreadyRecorded 检查同一候选人新消息是否已经生成过完全相同的回复。
func (f *Flow) replyAlreadyRecorded(ctx context.Context, prepared shared.PreparedTask, conversation cloud.AutoReplyConversation, basedOnMessageKey string, message string, sequence ...int) (bool, error) {
	messageSequence := 0
	if len(sequence) > 0 {
		messageSequence = sequence[0]
	}
	exists, err := f.Store.ConversationExists(ctx, prepared.Request.TaskID, conversation.PlatformThreadID, replyRecordHashWithSequence(basedOnMessageKey, message, messageSequence))
	if err != nil {
		return false, fmt.Errorf("检查重复回复失败：%w", err)
	}
	return exists, nil
}

// replyRecordHash 返回“候选人消息 + 回复动作”的本地去重哈希。
func replyRecordHash(basedOnMessageKey string, reply string) string {
	return replyRecordHashWithSequence(basedOnMessageKey, reply, 0)
}

// replyRecordHashWithSequence 把同轮消息顺序加入去重键，允许两条内容相同但职责不同的消息独立记录。
func replyRecordHashWithSequence(basedOnMessageKey string, reply string, sequence int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%d\n%s", strings.TrimSpace(basedOnMessageKey), sequence, strings.TrimSpace(reply))))
	return hex.EncodeToString(sum[:])
}

// reportChangedMessage 向悬浮窗说明旧回复已废弃，等待下一轮重新分析。
func (f *Flow) reportChangedMessage(taskID string, candidateName string) {
	shared.ReportAnalysis(f.Logger, taskID, shared.AnalysisStatus{
		Kind: "auto_reply", Phase: "result", Stage: "message_changed", Terminal: true,
		CandidateName: candidateName, Accepted: boolPointer(false),
		Reason: "候选人又发来新消息，旧回复已放弃，下一轮重新看", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}
