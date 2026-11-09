// Package cloud 本文件持久记录待回复消息、跨任务发送意图及原子AI判断，读取消息不代表已完成处理。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"strings"
	"time"
)

// localAutoReplySend 固定同一轮消息的发送正文，重启后不因模型重写而重复发送。
type localAutoReplySend struct {
	OutboundMessageKey string    `json:"outbound_message_key,omitempty"`
	ConversationID     string    `json:"conversation_id"`
	MessageKey         string    `json:"message_key"`
	Content            string    `json:"content"`
	Sequence           int       `json:"sequence"`
	Status             string    `json:"status"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// EnsureLocalAutoReplyCandidate 在没有手机号时也建立本地候选人，并原子关联当前会话与平台身份。
func (c *Client) EnsureLocalAutoReplyCandidate(ctx context.Context, credentials AgentCredentials, conversation AutoReplyConversation) (candidate AutoReplyStoredCandidate, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		stored, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversation.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("本地会话尚未建立")
		}
		if stored.CandidateID != "" {
			candidate, found, err = getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", stored.CandidateID)
			if err != nil {
				return err
			}
			if found {
				return nil
			}
		}
		if stored.PlatformIdentityID != "" {
			identity, ok, err := getAutoReplyEntity[CandidatePlatformIdentity](tx, "identities", stored.PlatformIdentityID)
			if err != nil {
				return err
			}
			if ok && identity.CandidateID != "" {
				candidate, _, err = getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", identity.CandidateID)
				if err != nil {
					return err
				}
			}
		}
		if candidate.ID == "" {
			id, err := newLocalAutoReplyCandidateID()
			if err != nil {
				return err
			}
			candidate = AutoReplyStoredCandidate{ID: id, CandidateName: stored.CandidateName, Gender: stored.Gender}
			if err = tx.Put("candidates", id, candidate); err != nil {
				return err
			}
		}
		stored.CandidateID = candidate.ID
		if err = tx.Put("conversations", stored.ID, stored); err != nil {
			return err
		}
		if stored.PlatformIdentityID != "" {
			identity, found, err := getAutoReplyEntity[CandidatePlatformIdentity](tx, "identities", stored.PlatformIdentityID)
			if err != nil {
				return err
			}
			if found {
				identity.CandidateID = candidate.ID
				return tx.Put("identities", identity.ID, identity)
			}
		}
		return nil
	})
	return
}

// PendingAutoReplyMessages 返回尚未回复且未被后续HR消息覆盖的完整候选人消息批次。
func (c *Client) PendingAutoReplyMessages(ctx context.Context, credentials AgentCredentials, conversationID string) (items []AutoReplyMessage, err error) {
	items = []AutoReplyMessage{}
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		rows, err := tx.PendingMessages(conversationID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var item AutoReplyMessage
			if err = json.Unmarshal(row.Data, &item); err != nil {
				return err
			}
			items = append(items, item)
		}
		return nil
	})
	return
}

// ListPendingAutoReplyConversations 返回当前招聘账号待采集或待答的会话，用于恢复已读和重启任务。
func (c *Client) ListPendingAutoReplyConversations(ctx context.Context, credentials AgentCredentials, platformID, accountID string) (items []AutoReplyConversation, err error) {
	items = []AutoReplyConversation{}
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		rows, err := tx.PendingConversations(platformID, accountID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var item AutoReplyConversation
			if err = json.Unmarshal(row.Data, &item); err != nil {
				return err
			}
			items = append(items, item)
		}
		probes, err := tx.ContactProbes(platformID, accountID)
		if err != nil {
			return err
		}
		for _, raw := range probes {
			var intent AutoReplyCaptureIntent
			if err = json.Unmarshal([]byte(raw), &intent); err != nil {
				return err
			}
			items = append(items, AutoReplyConversation{PlatformID: platformID, PlatformAccountID: accountID, PlatformThreadID: intent.PlatformThreadID, CaptureIntent: &intent, CandidateName: intent.CandidateName, PositionID: intent.PositionID})
		}
		return nil
	})
	return
}

// CompleteAutoReplyMessage 原子完成本轮消息及其之前的同会话消息，采集失败时调用方不得调用。
func (c *Client) CompleteAutoReplyMessage(ctx context.Context, credentials AgentCredentials, conversationID, messageKey, outcome string) error {
	if outcome != "sent" && outcome != "manual" && outcome != "skipped_self" && outcome != "completed" {
		return fmt.Errorf("未知消息处理结果")
	}
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error { return tx.CompleteMessages(conversationID, messageKey, outcome) })
}

// autoReplySendID 仅以会话、入站批次和发送序号固定动作，正文变化不能绕过去重。
func autoReplySendID(conversationID, messageKey string, sequence int) string {
	return autoReplyDigest(fmt.Sprintf("%s\x00%s\x00%d", conversationID, messageKey, sequence))
}

// ClaimAutoReplySend 在真正发送前持久写入sending；重启遇到sending或unknown时禁止自动重发。
func (c *Client) ClaimAutoReplySend(ctx context.Context, credentials AgentCredentials, conversationID, messageKey, content string, sequence int) (status string, claimed bool, err error) {
	if content == "" || sequence < 0 {
		return "", false, fmt.Errorf("发送内容或顺序不正确")
	}
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		id := autoReplySendID(conversationID, messageKey, sequence)
		old, exists, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", id)
		if err != nil {
			return err
		}
		if exists {
			status = old.Status
			if old.Content != content {
				return fmt.Errorf("这轮消息已有不同的发送内容，请人工核对")
			}
			return nil
		}
		conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("发送会话没有找到")
		}
		legacyHash := autoReplyDigest(fmt.Sprintf("%s\n%d\n%s", strings.TrimSpace(messageKey), sequence, strings.TrimSpace(content)))
		legacy := ""
		if conversation.PlatformAccountID == "" {
			legacy, err = tx.LegacyAutoReplySendResult(conversation.PlatformID, conversation.PositionID, conversation.PlatformThreadID, legacyHash)
		}
		if err != nil {
			return err
		}
		status = "sending"
		claimed = true
		if legacy == "success" {
			status = "sent"
			claimed = false
		} else if legacy == "unknown" {
			status = "unknown"
			claimed = false
		}
		return tx.Put("sends", id, localAutoReplySend{ConversationID: conversationID, MessageKey: messageKey, Content: content, Sequence: sequence, Status: status, UpdatedAt: time.Now().UTC()})
	})
	return
}

// ReleaseUnsentAutoReply 仅释放调用方已领取、内容完全一致且能证明尚未触发发送的旧意图。
func (c *Client) ReleaseUnsentAutoReply(ctx context.Context, credentials AgentCredentials, conversationID, messageKey string, sequence int, content string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		id := autoReplySendID(conversationID, messageKey, sequence)
		item, found, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", id)
		if err != nil || !found {
			return err
		}
		if item.Status != "sending" || item.Content != content {
			return fmt.Errorf("发送意图状态已变化，不能撤销")
		}
		return tx.Delete("sends", id)
	})
}

// FinishAutoReplySend 只允许发送意图转为已确认发送或结果不明，避免失败后自动重复发送。
func (c *Client) FinishAutoReplySend(ctx context.Context, credentials AgentCredentials, conversationID, messageKey string, sequence int, status string, outbound ...string) error {
	if status != "sent" && status != "unknown" {
		return fmt.Errorf("发送结果需要明确为sent或unknown")
	}
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		id := autoReplySendID(conversationID, messageKey, sequence)
		item, found, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("发送意图没有找到")
		}
		if item.Status == "sent" {
			return nil
		}
		if status == "sent" && len(outbound) > 0 {
			item.OutboundMessageKey = strings.TrimSpace(outbound[0])
		}
		item.Status = status
		item.UpdatedAt = time.Now().UTC()
		return tx.Put("sends", id, item)
	})
}

// IncrementAutoReplyConfirmationAsked 在事务中递增询问次数，不用旧AI快照覆盖最新计数。
func (c *Client) IncrementAutoReplyConfirmationAsked(ctx context.Context, credentials AgentCredentials, itemID string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		item, found, err := getAutoReplyEntity[CandidateConfirmationItem](tx, "confirmations", itemID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("确认项没有找到")
		}
		now := time.Now().UTC()
		item.AskCount++
		item.LastAskedAt = &now
		item.UpdatedAt = now
		return tx.Put("confirmations", itemID, item)
	})
}

// ArchiveAutoReplyPositionConfirmations 仅归档已删除或停用的岗位条件，不删除AI独立提出的确认项。
func (c *Client) ArchiveAutoReplyPositionConfirmations(ctx context.Context, credentials AgentCredentials, candidateID, positionID string, activeConditionIDs []string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		items, err := listAutoReplyEntities[CandidateConfirmationItem](tx, "confirmations", autoReplyFilter("candidate_id", candidateID), autoReplyFilter("position_id", positionID), autoReplyFilter("source_type", "position"))
		if err != nil {
			return err
		}
		active := map[string]bool{}
		for _, id := range activeConditionIDs {
			active[id] = true
		}
		now := time.Now().UTC()
		for _, item := range items {
			if !active[item.PositionConditionID] && item.ArchivedAt == nil {
				item.ArchivedAt = &now
				item.UpdatedAt = now
				if err = tx.Put("confirmations", item.ID, item); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// CommitAutoReplyProposal 原子提交经过复核的全部AI判断与建议，消息改变时整批拒绝。
func (c *Client) CommitAutoReplyProposal(ctx context.Context, credentials AgentCredentials, conversationID, basedOnMessageKey string, configVersion int, confirmations []CandidateConfirmationItem, suggestions []AutoReplyConfigSuggestion) error {
	if configVersion <= 0 {
		return fmt.Errorf("自动回复岗位版本尚未确认")
	}
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if !found || conversation.LastCandidateMessageKey != basedOnMessageKey {
			return fmt.Errorf("候选人消息已变化，本轮判断未保存")
		}
		pending, err := tx.PendingMessages(conversationID)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			return fmt.Errorf("这轮消息已处理或HR已接手，本轮判断未保存")
		}
		for _, item := range confirmations {
			if item.ConversationID != conversationID || item.CandidateID != conversation.CandidateID || item.PositionID != conversation.PositionID {
				return fmt.Errorf("确认项与本轮候选人或岗位不一致")
			}
			if _, err = putLocalAutoReplyConfirmation(tx, item); err != nil {
				return err
			}
		}
		for _, item := range suggestions {
			if item.ConversationID != conversationID || item.PositionID != conversation.PositionID {
				return fmt.Errorf("建议与本轮岗位不一致")
			}
			raw, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if item.ID == "" {
				item.ID = "suggestion-" + autoReplyDigest(string(raw))
			}
			item.Status = "pending"
			item.CreatedAt = time.Now().UTC()
			if err = tx.Put("suggestions", item.ID, item); err != nil {
				return err
			}
		}
		receiptID := autoReplyDigest(fmt.Sprintf("%s|%s|%d", conversationID, basedOnMessageKey, configVersion))
		_, exists, err := tx.Get("proposal_receipts", receiptID)
		if err != nil {
			return err
		}
		if !exists {
			if err = tx.Put("proposal_receipts", receiptID, AutoReplyProposalReceipt{ID: receiptID, ConversationID: conversationID, Conversation: conversation, BasedOnMessageKey: basedOnMessageKey, ConfigVersion: configVersion, Status: "pending"}); err != nil {
				return err
			}
		}

		return nil
	})
}

// SaveAutoReplyChatPhone 从已保存的新消息补全本机会话候选人的空手机号，不覆盖旧号码或改变简历状态。
func (c *Client) SaveAutoReplyChatPhone(ctx context.Context, credentials AgentCredentials, conversationID, candidateID, messageKey, phone string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		conversation, ok, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if !ok || candidateID == "" || conversation.CandidateID != candidateID {
			return fmt.Errorf("手机号的候选人与当前会话不一致")
		}
		row, found, err := tx.MessageByKey(conversationID, messageKey)
		if err != nil {
			return err
		}
		var msg AutoReplyMessage
		if !found || json.Unmarshal(row.Data, &msg) != nil || msg.Direction != "candidate" || msg.MessageType != "text" || len(phone) != 11 || !strings.Contains(strings.NewReplacer(" ", "", "-", "").Replace(msg.TextContent), phone) {
			return fmt.Errorf("候选人手机号缺少本轮聊天证据")
		}
		candidate, found, err := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", candidateID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("本机没有找到这份候选人资料")
		}
		if candidate.Phone != "" && candidate.Phone != phone {
			return fmt.Errorf("候选人发来的号码与已有号码不同，请人工核实")
		}
		if candidate.Phone == phone {
			return nil
		}
		others, err := scopedAutoReplyPhoneCandidates(tx, conversation.PlatformID, conversation.PlatformAccountID, phone)
		if err != nil {
			return err
		}
		for _, other := range others {
			if other.ID != candidateID {
				return fmt.Errorf("此号码已关联其他候选人，请人工核实")
			}
		}
		candidate.Phone = phone
		if err = tx.Put("candidates", candidateID, candidate); err != nil {
			return err
		}
		identity, found, err := getAutoReplyEntity[CandidatePlatformIdentity](tx, "identities", conversation.PlatformIdentityID)
		if err != nil {
			return err
		}
		if found && identity.CandidateID == candidateID && identity.NormalizedPhone == "" {
			identity.NormalizedPhone = phone
			return tx.Put("identities", identity.ID, identity)
		}
		return nil
	})
}
