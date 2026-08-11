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
func (f *Flow) processConversation(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, positions []cloud.AutoReplyPositionSnapshot, conversation model.Conversation, openSnapshot *model.AutoReplyConversationSnapshot, stats *shared.Stats) error {
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
	knownMessageKeys := knownMessageKeysFromState(initialState)
	pageSnapshot := model.AutoReplyConversationSnapshot{}
	if openSnapshot != nil && openSnapshot.CandidateName != "" {
		pageSnapshot = *openSnapshot
		pageSnapshot.Messages, pageSnapshot.HistoryComplete, err = runtime.ReadAutoReplyMessages(
			ctx, f.Browser, prepared.Platform, pageSnapshot, knownMessageKeys, cloud.AutoReplyMaxHistoryMessages,
		)
	} else {
		pageSnapshot, err = runtime.OpenAutoReplyConversation(ctx, f.Browser, prepared.Platform, conversation, knownMessageKeys, cloud.AutoReplyMaxHistoryMessages)
	}
	if err != nil {
		stats.Failed++
		return fmt.Errorf("打开并读取候选人会话失败：%w", err)
	}
	pageSnapshot = normalizePageSnapshot(conversation, pageSnapshot)
	if openSnapshot != nil {
		*openSnapshot = pageSnapshot
	}
	messages, latestCandidateKey, err := convertMessages(pageSnapshot.Messages)
	if err != nil {
		stats.Failed++
		return err
	}
	if len(messages) == 0 {
		stats.Skipped++
		return nil
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
	for refreshes := 0; ; refreshes++ {
		shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
			Kind: "auto_reply", Phase: "loading", Stage: "ai", CandidateName: pageSnapshot.CandidateName,
			Reason: "AI 正在认真看消息", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		decision, changed, replyErr := f.replyWhileMonitoring(ctx, prepared, runtime, pageSnapshot, ReplyContext{
			TaskID: prepared.Request.TaskID, Credentials: credentials(prepared), Position: position,
			AIConfig: prepared.Position.AI, EnableThinking: prepared.Position.EnableThinking,
			Conversation: cloudConversation, CandidateState: state, Messages: messages,
			ConfirmationItems: confirmations, PageSnapshot: pageSnapshot, Resume: resume,
			BasedOnMessageKey: latestCandidateKey,
		})
		if replyErr != nil {
			stats.Failed++
			return fmt.Errorf("AI 自动回复判断失败：%w", replyErr)
		}
		if !changed {
			unchanged, checkErr := f.latestCandidateMessageUnchanged(ctx, runtime, prepared, pageSnapshot, latestCandidateKey)
			if checkErr != nil {
				stats.Failed++
				return checkErr
			}
			changed = !unchanged
		}
		if changed {
			f.reportChangedMessage(prepared.Request.TaskID, pageSnapshot.CandidateName)
			if refreshes >= maxAutoReplyMessageRefreshes {
				stats.Skipped++
				return f.notifyUnresolved(ctx, prepared, position, cloudConversation, pageSnapshot,
					"候选人连续发来新消息，我没敢抢着插话，请人工看一下", "candidate_message_keeps_changing", latestCandidateKey)
			}
			pageMessages, historyComplete, readErr := runtime.ReadAutoReplyMessages(
				ctx, f.Browser, prepared.Platform, pageSnapshot, recentAutoReplyMessageKeys(messages), cloud.AutoReplyMaxHistoryMessages,
			)
			if readErr != nil {
				stats.Failed++
				return fmt.Errorf("候选人发来新消息后差量读取失败：%w", readErr)
			}
			pageSnapshot.Messages = pageMessages
			pageSnapshot.HistoryComplete = historyComplete
			delta, nextCandidateKey, convertErr := convertMessages(pageMessages)
			if convertErr != nil {
				stats.Failed++
				return convertErr
			}
			if len(delta) == 0 {
				stats.Skipped++
				return nil
			}
			if _, syncErr := f.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{
				ConversationID: cloudConversation.ID, HistoryComplete: historyComplete, Messages: delta,
			}); syncErr != nil {
				stats.Failed++
				return fmt.Errorf("同步候选人新消息失败：%w", syncErr)
			}
			if nextCandidateKey == "" {
				stats.Skipped++
				return nil
			}
			messages, err = f.Cloud.AutoReplyMessages(ctx, credentials(prepared), cloudConversation.ID)
			if err != nil {
				stats.Failed++
				return fmt.Errorf("刷新完整聊天记录失败：%w", err)
			}
			confirmations, err = f.Cloud.AutoReplyConfirmationItems(ctx, credentials(prepared), cloudConversation.ID)
			if err != nil {
				stats.Failed++
				return fmt.Errorf("刷新候选人确认项失败：%w", err)
			}
			latestCandidateKey = nextCandidateKey
			continue
		}
		if strings.TrimSpace(decision.ManualReason) != "" {
			stats.Skipped++
			reasonKey := firstNonEmpty(decision.ReasonKey, "ai_manual_handoff")
			return f.notifyUnresolved(ctx, prepared, position, cloudConversation, pageSnapshot, decision.ManualReason, reasonKey, latestCandidateKey)
		}
		reply := strings.TrimSpace(decision.Reply)
		if reply == "" || len([]rune(reply)) > maxAutoReplyMessageRunes {
			stats.Failed++
			return fmt.Errorf("AI 回复为空或超过200字")
		}
		sent, sendErr := f.sendVerifiedMessage(ctx, prepared, runtime, cloudConversation, pageSnapshot, latestCandidateKey, reply, false)
		if sendErr != nil {
			stats.Failed++
			return sendErr
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
}

// knownMessageKeysFromState 返回云端最近两条消息游标，并兼容只返回最后一条游标的旧服务端。
func knownMessageKeysFromState(state cloud.AutoReplyCandidateState) []string {
	keys := cleanMessageKeys(state.RecentMessageKeys)
	if len(keys) == 0 && state.Conversation != nil {
		keys = cleanMessageKeys([]string{state.Conversation.LastSyncedMessageKey})
	}
	return keys
}

// recentAutoReplyMessageKeys 返回聊天记录最后两条可用的稳定消息编号。
func recentAutoReplyMessageKeys(messages []cloud.AutoReplyMessage) []string {
	start := len(messages) - 2
	if start < 0 {
		start = 0
	}
	keys := make([]string, 0, 2)
	for _, message := range messages[start:] {
		keys = append(keys, firstNonEmpty(message.PlatformMessageID, message.Fingerprint))
	}
	return cleanMessageKeys(keys)
}

// cleanMessageKeys 清理空白消息编号并只保留最后两条。
func cleanMessageKeys(values []string) []string {
	result := make([]string, 0, 2)
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			value = strings.TrimPrefix(strings.TrimPrefix(value, "id:"), "fp:")
			result = append(result, value)
		}
	}
	if len(result) > 2 {
		result = result[len(result)-2:]
	}
	return result
}

// normalizePageSnapshot 用未读列表字段补齐打开会话后可能为空的身份字段。
func normalizePageSnapshot(conversation model.Conversation, snapshot model.AutoReplyConversationSnapshot) model.AutoReplyConversationSnapshot {
	snapshot.Conversation = conversation
	snapshot.CandidateName = firstNonEmpty(snapshot.CandidateName, conversation.Name)
	snapshot.AvatarURL = firstNonEmpty(snapshot.AvatarURL, conversation.AvatarURL)
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
		} else if direction == "self" {
			latestCandidateKey = ""
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
