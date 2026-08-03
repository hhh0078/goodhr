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

// sendVerifiedMessage 发送一条消息并回读确认，结果未知时不直接重发。
func (f *Flow) sendVerifiedMessage(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, conversation cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, basedOnMessageKey string, message string, verifyCandidateBefore bool) (bool, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return false, fmt.Errorf("候选人回复内容不能为空")
	}
	if verifyCandidateBefore {
		unchanged, err := f.latestCandidateMessageUnchanged(ctx, runtime, prepared, snapshot, basedOnMessageKey)
		if err != nil || !unchanged {
			if err == nil {
				f.reportChangedMessage(prepared.Request.TaskID, snapshot.CandidateName)
			}
			return false, err
		}
	}
	duplicate, err := f.replyAlreadyRecorded(ctx, prepared, conversation, message)
	if err != nil || duplicate {
		return false, err
	}
	if err = runtime.SendAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot, message); err != nil {
		return false, fmt.Errorf("发送候选人消息失败：%w", err)
	}
	latest, err := runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot)
	if err != nil {
		f.saveLocalReplyRecord(ctx, prepared, conversation, hashReply(message), "unknown")
		return false, fmt.Errorf("消息已经点击发送，但回读结果失败；我先不重发：%w", err)
	}
	direction := strings.ToLower(strings.TrimSpace(latest.Direction))
	if direction != "recruiter" || strings.TrimSpace(latest.TextContent) != message {
		f.saveLocalReplyRecord(ctx, prepared, conversation, hashReply(message), "unknown")
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
	f.saveLocalReplyRecord(ctx, prepared, conversation, hashReply(message), "success")
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

// replyAlreadyRecorded 检查同一任务和会话是否已经发送过完全相同的回复。
func (f *Flow) replyAlreadyRecorded(ctx context.Context, prepared shared.PreparedTask, conversation cloud.AutoReplyConversation, message string) (bool, error) {
	exists, err := f.Store.ConversationExists(ctx, prepared.Request.TaskID, conversation.PlatformThreadID, hashReply(message))
	if err != nil {
		return false, fmt.Errorf("检查重复回复失败：%w", err)
	}
	return exists, nil
}

// hashReply 返回回复正文的本地去重哈希。
func hashReply(reply string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(reply)))
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
