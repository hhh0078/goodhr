// Package auto_reply 本文件负责单个会话的身份、岗位、聊天、简历和发送闭环。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// processConversation 平铺执行页面读取、岗位归属、云端同步、简历门槛、AI决定和安全发送。
func (f *Flow) processConversation(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, positions []cloud.AutoReplyPositionSnapshot, conversation model.Conversation, stats *shared.Stats) error {
	stats.Processed++
	initialState, err := f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{
		PlatformID: prepared.Platform.ID, PlatformAccountID: conversation.PlatformAccountID,
		PlatformCandidateID: conversation.PlatformCandidateID,
		PlatformThreadID:    firstNonEmpty(conversation.PlatformThreadID, conversation.Key),
	})
	if err != nil {
		stats.Failed++
		return fmt.Errorf("读取候选人差量游标失败：%w", err)
	}
	knownLastMessageKey := ""
	if initialState.Conversation != nil {
		knownLastMessageKey = initialState.Conversation.LastSyncedMessageKey
	}
	pageSnapshot, err := runtime.OpenAutoReplyConversation(ctx, f.Browser, prepared.Platform, conversation, knownLastMessageKey, cloud.AutoReplyMaxHistoryMessages)
	if err != nil {
		stats.Failed++
		return fmt.Errorf("打开并读取候选人会话失败：%w", err)
	}
	pageSnapshot = normalizePageSnapshot(conversation, pageSnapshot)
	messages, latestCandidateKey, err := convertMessages(pageSnapshot.Messages)
	if err != nil {
		stats.Failed++
		return err
	}
	position, positionErr := resolvePosition(positions, pageSnapshot.CommunicationPosition)
	state := initialState
	if pageSnapshot.PlatformThreadID != firstNonEmpty(conversation.PlatformThreadID, conversation.Key) ||
		pageSnapshot.PlatformCandidateID != conversation.PlatformCandidateID ||
		pageSnapshot.PlatformAccountID != conversation.PlatformAccountID || pageSnapshot.Phone != "" {
		state, err = f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{
			PlatformID: prepared.Platform.ID, PlatformAccountID: pageSnapshot.PlatformAccountID,
			PlatformCandidateID: pageSnapshot.PlatformCandidateID, PlatformThreadID: pageSnapshot.PlatformThreadID,
			Phone: pageSnapshot.Phone,
		})
		if err != nil {
			stats.Failed++
			return fmt.Errorf("读取候选人云端状态失败：%w", err)
		}
	}
	identity, err := f.savePlatformIdentity(ctx, prepared, pageSnapshot, state)
	if err != nil {
		stats.Failed++
		return err
	}
	cloudConversation, err := f.saveCloudConversation(ctx, prepared, position, positionErr, pageSnapshot, state, identity)
	if err != nil {
		stats.Failed++
		return err
	}
	if _, err = f.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{
		ConversationID: cloudConversation.ID, HistoryComplete: pageSnapshot.HistoryComplete, Messages: messages,
	}); err != nil {
		stats.Failed++
		return fmt.Errorf("同步候选人聊天记录失败：%w", err)
	}
	if latestCandidateKey == "" {
		stats.Skipped++
		return f.notifyUnresolved(ctx, prepared, position, cloudConversation, pageSnapshot, "没有读到候选人的最新消息", "candidate_message_missing", messageFingerprint(model.ConversationMessage{Direction: "candidate", TextContent: pageSnapshot.PlatformThreadID}, 0))
	}
	if positionErr != nil {
		stats.Skipped++
		return f.notifyUnresolved(ctx, prepared, position, cloudConversation, pageSnapshot, positionErr.Error(), "position_unresolved", latestCandidateKey)
	}
	shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
		Kind: "auto_reply", Phase: "loading", Stage: "sync", CandidateName: pageSnapshot.CandidateName,
		Reason: "聊天记录已经同步，正在核对简历", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})

	messages, err = f.Cloud.AutoReplyMessages(ctx, credentials(prepared), cloudConversation.ID)
	if err != nil {
		stats.Failed++
		return fmt.Errorf("读取完整聊天记录失败：%w", err)
	}
	confirmations, err := f.Cloud.AutoReplyConfirmationItems(ctx, credentials(prepared), cloudConversation.ID)
	if err != nil {
		stats.Failed++
		return fmt.Errorf("读取候选人确认项失败：%w", err)
	}
	resume, handled, err := f.ensureResume(ctx, prepared, runtime, position, &cloudConversation, pageSnapshot, state, latestCandidateKey, stats)
	if err != nil || handled {
		return err
	}
	if f.Responder == nil {
		stats.Failed++
		return fmt.Errorf("自动回复 AI 处理器没有准备完整")
	}
	shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
		Kind: "auto_reply", Phase: "loading", Stage: "ai", CandidateName: pageSnapshot.CandidateName,
		Reason: "AI 正在认真看消息", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	decision, err := f.Responder.Reply(ctx, ReplyContext{
		TaskID: prepared.Request.TaskID, Credentials: credentials(prepared), Position: position,
		AIConfig: prepared.Position.AI, EnableThinking: prepared.Position.EnableThinking,
		Conversation: cloudConversation, CandidateState: state, Messages: messages,
		ConfirmationItems: confirmations, PageSnapshot: pageSnapshot, Resume: resume,
		BasedOnMessageKey: latestCandidateKey,
	})
	if err != nil {
		stats.Failed++
		return fmt.Errorf("AI 自动回复判断失败：%w", err)
	}
	if strings.TrimSpace(decision.ManualReason) != "" {
		stats.Skipped++
		reasonKey := firstNonEmpty(decision.ReasonKey, "ai_manual_handoff")
		return f.notifyUnresolved(ctx, prepared, position, cloudConversation, pageSnapshot, decision.ManualReason, reasonKey, latestCandidateKey)
	}
	reply := strings.TrimSpace(decision.Reply)
	if reply == "" || len([]rune(reply)) > 1000 {
		stats.Failed++
		return fmt.Errorf("AI 回复为空或超过1000字")
	}
	sent, err := f.sendVerifiedMessage(ctx, prepared, runtime, cloudConversation, pageSnapshot, latestCandidateKey, reply, true)
	if err != nil {
		stats.Failed++
		return err
	}
	if !sent {
		stats.Skipped++
		return nil
	}
	stats.Succeeded++
	shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
		Kind: "auto_reply", Phase: "result", Stage: "sent", Terminal: true,
		CandidateName: pageSnapshot.CandidateName, Accepted: boolPointer(true),
		Reason: "候选人消息已经回复", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	return nil
}

// normalizePageSnapshot 用未读列表字段补齐打开会话后可能为空的身份字段。
func normalizePageSnapshot(conversation model.Conversation, snapshot model.AutoReplyConversationSnapshot) model.AutoReplyConversationSnapshot {
	snapshot.Conversation = conversation
	snapshot.CandidateName = firstNonEmpty(snapshot.CandidateName, conversation.Name)
	snapshot.Gender = firstNonEmpty(snapshot.Gender, conversation.Gender)
	snapshot.PlatformThreadID = firstNonEmpty(snapshot.PlatformThreadID, conversation.PlatformThreadID, conversation.Key)
	snapshot.PlatformCandidateID = firstNonEmpty(snapshot.PlatformCandidateID, conversation.PlatformCandidateID)
	snapshot.PlatformAccountID = firstNonEmpty(snapshot.PlatformAccountID, conversation.PlatformAccountID)
	snapshot.CommunicationPosition = firstNonEmpty(snapshot.CommunicationPosition, conversation.CommunicationPosition)
	return snapshot
}

// savePlatformIdentity 优先保存平台候选人编号，获取不到时由手机号和会话编号后备。
func (f *Flow) savePlatformIdentity(ctx context.Context, prepared shared.PreparedTask, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState) (cloud.CandidatePlatformIdentity, error) {
	if strings.TrimSpace(snapshot.PlatformCandidateID) == "" {
		if state.Identity != nil {
			return *state.Identity, nil
		}
		return cloud.CandidatePlatformIdentity{}, nil
	}
	candidateID := ""
	if state.Candidate != nil {
		candidateID = state.Candidate.ID
	}
	identity, err := f.Cloud.SaveAutoReplyIdentity(ctx, credentials(prepared), cloud.CandidatePlatformIdentity{
		CandidateID: candidateID, PlatformID: prepared.Platform.ID,
		PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID,
		CandidateName: snapshot.CandidateName, Gender: snapshot.Gender, NormalizedPhone: snapshot.Phone,
	})
	if err != nil {
		return cloud.CandidatePlatformIdentity{}, fmt.Errorf("保存平台候选人身份失败：%w", err)
	}
	return identity, nil
}

// saveCloudConversation 保存正式简历存在前也可使用的临时会话和岗位归属结果。
func (f *Flow) saveCloudConversation(ctx context.Context, prepared shared.PreparedTask, position cloud.AutoReplyPositionSnapshot, positionErr error, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState, identity cloud.CandidatePlatformIdentity) (cloud.AutoReplyConversation, error) {
	item := cloud.AutoReplyConversation{
		PositionID: position.Position.ID, PlatformIdentityID: identity.ID,
		PlatformAccountID: snapshot.PlatformAccountID, PlatformID: prepared.Platform.ID,
		PlatformThreadID: snapshot.PlatformThreadID, CandidateName: snapshot.CandidateName,
		Gender: snapshot.Gender, PagePositionText: snapshot.CommunicationPosition,
		Status: "active", HistoryComplete: snapshot.HistoryComplete,
	}
	if state.Candidate != nil {
		item.CandidateID = state.Candidate.ID
	}
	if state.Conversation != nil {
		item.ID = state.Conversation.ID
		item.EngagementID = state.Conversation.EngagementID
		item.LastSyncedMessageKey = state.Conversation.LastSyncedMessageKey
		item.LastCandidateMessageKey = state.Conversation.LastCandidateMessageKey
	}
	if positionErr != nil {
		item.PositionID = ""
		item.UnresolvedReason = positionErr.Error()
	}
	now := time.Now().UTC()
	item.LastCheckedAt = &now
	saved, err := f.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), item)
	if err != nil {
		return cloud.AutoReplyConversation{}, fmt.Errorf("保存候选人会话失败：%w", err)
	}
	return saved, nil
}

// convertMessages 校验平台消息方向并生成云端幂等指纹和最新候选人消息键。
func convertMessages(items []model.ConversationMessage) ([]cloud.AutoReplyMessage, string, error) {
	result := make([]cloud.AutoReplyMessage, 0, len(items))
	latestCandidateKey := ""
	for index, item := range items {
		direction := strings.ToLower(strings.TrimSpace(item.Direction))
		if direction != "candidate" && direction != "self" && direction != "system" {
			return nil, "", fmt.Errorf("第%d条聊天消息方向无法确认", index+1)
		}
		card := item.CardContent
		if len(strings.TrimSpace(string(card))) == 0 {
			card = json.RawMessage(`{}`)
		}
		fingerprint := firstNonEmpty(item.Key, messageFingerprint(item, index))
		message := cloud.AutoReplyMessage{
			PlatformMessageID: strings.TrimSpace(item.PlatformMessageID), Fingerprint: fingerprint,
			Direction: direction, MessageType: firstNonEmpty(item.MessageType, "text"),
			TextContent: item.TextContent, CardContent: card, SenderName: item.SenderName,
			PlatformSentAt: item.SentAt, IngestedAt: time.Now().UTC(),
		}
		result = append(result, message)
		if direction == "candidate" {
			latestCandidateKey = firstNonEmpty(message.PlatformMessageID, message.Fingerprint)
		}
	}
	return result, latestCandidateKey, nil
}

// messageFingerprint 为没有平台消息编号的消息生成稳定本地指纹。
func messageFingerprint(item model.ConversationMessage, index int) string {
	sentAt := ""
	if item.SentAt != nil {
		sentAt = item.SentAt.UTC().Format(time.RFC3339Nano)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		item.Direction, item.MessageType, item.TextContent, item.SenderName, sentAt, fmt.Sprint(index),
	}, "|")))
	return hex.EncodeToString(sum[:])
}

// firstNonEmpty 返回第一段非空文字。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// boolPointer 返回供悬浮窗状态使用的布尔指针。
func boolPointer(value bool) *bool {
	return &value
}

// saveLocalReplyRecord 保存不含消息正文的本地重复发送摘要。
func (f *Flow) saveLocalReplyRecord(ctx context.Context, prepared shared.PreparedTask, conversation cloud.AutoReplyConversation, replyHash string, result string) {
	if err := f.Store.SaveConversation(context.WithoutCancel(ctx), storage.ConversationRecord{
		TaskID: prepared.Request.TaskID, ConversationKey: conversation.PlatformThreadID,
		PlatformID: prepared.Platform.ID, ReplyHash: replyHash, Result: result,
	}); err != nil {
		f.log(prepared.Request.TaskID, "save_conversation", "warning", time.Now(), err)
	}
}
