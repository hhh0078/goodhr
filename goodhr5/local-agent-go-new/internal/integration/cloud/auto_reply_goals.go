// Package cloud 本文件把动态沟通目标、一次性追问与发送回执绑定在现有服务器事务中。
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// ErrAutoReplyGoalChanged 表示目标、消息或人工接管已使旧追问失效，调用方应放弃该问题而不重发。
var ErrAutoReplyGoalChanged = errors.New("沟通目标或对话已经变化，旧追问已放下")

type autoReplySendIntentContextKey struct{}

// WithAutoReplySendIntent 仅在服务器调用上下文携带发送意图，不把目标或业务版本传给旧版设备。
func WithAutoReplySendIntent(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, autoReplySendIntentContextKey{}, id)
}

// AutoReplySendIntentFromContext 供服务器首次登记命令时保存原意图编号，设备命令协议保持不变。
func AutoReplySendIntentFromContext(ctx context.Context) string {
	id, _ := ctx.Value(autoReplySendIntentContextKey{}).(string)
	return id
}

// AutoReplyGoalContentKey 标识问题内容；只改状态或证据不能把已经问过的问题重新变成未问。
func AutoReplyGoalContentKey(item CandidateConfirmationItem) string {
	return autoReplyDigest(item.ItemType + "\n" + strings.ToLower(strings.Join(strings.Fields(item.Content), "")))
}

// AutoReplyConfirmationRevision 绑定本轮读取的目标版本，修改、完成或归档后旧追问立即失效。
func AutoReplyConfirmationRevision(item CandidateConfirmationItem) string {
	return autoReplyDigest(item.ID + "\n" + item.UpdatedAt.UTC().Format(time.RFC3339Nano) + "\n" + item.Status + "\n" + AutoReplyGoalContentKey(item))
}

// AutoReplyGoalContextRevision 冻结追问采用的岗位、话术、提示词与公司正文；会员剩余秒数不属于事实版本。
func AutoReplyGoalContextRevision(snapshot AutoReplyPositionSnapshot) string {
	raw, _ := json.Marshal(struct {
		Position AutoReplyPosition
		Config   PositionAutoReplyConfig
		Company  CompanyProfile
	}{snapshot.Position, snapshot.Config, snapshot.CompanyProfile})
	return autoReplyDigest(string(raw))
}

// AutoReplyGoalCanAsk 只允许未确认且没有问过当前问题的目标；无新证据时不循环催问。
func AutoReplyGoalCanAsk(item CandidateConfirmationItem) bool {
	if item.ArchivedAt != nil || item.Status != "pending" || item.ItemType != "required" && item.ItemType != "confirm" || item.AskCount >= 3 {
		return false
	}
	if item.LastAskedAt != nil || item.AskCount > 0 {
		return item.LastAskedContentKey != "" && item.LastAskedContentKey != AutoReplyGoalContentKey(item)
	}
	return true
}

// AutoReplySendIntentID 返回与发送账本一致的稳定编号，供设备命令领取时再次核对目标。
func AutoReplySendIntentID(conversation, key string, sequence int) string {
	return autoReplySendID(conversation, key, sequence)
}

// validateGoalSend 在原事务中检查目标版本、候选人来信、人工接管和已经在途的问题。
func validateGoalSend(tx *storage.AutoReplyTx, conversation AutoReplyConversation, intent localAutoReplySend) (CandidateConfirmationItem, error) {
	item, found, err := getAutoReplyEntity[CandidateConfirmationItem](tx, "confirmations", intent.ConfirmationItemID)
	if err != nil {
		return item, err
	}
	if !found || item.ConversationID != conversation.ID || item.CandidateID != conversation.CandidateID || item.PositionID != conversation.PositionID || !AutoReplyGoalCanAsk(item) || AutoReplyConfirmationRevision(item) != intent.GoalVersion || conversation.LastCandidateMessageKey != intent.BasedOnMessageKey {
		return item, ErrAutoReplyGoalChanged
	}
	if intent.ExpectedLatestKey != "" && conversation.LastSyncedMessageKey != intent.ExpectedLatestKey {
		return item, ErrAutoReplyGoalChanged
	}
	if intent.AllowNoPending && (conversation.GoalFollowup == nil || "goal-event:"+conversation.GoalFollowup.EventKey != intent.MessageKey || conversation.GoalFollowup.ConfirmationItemID != intent.ConfirmationItemID) {
		return item, ErrAutoReplyGoalChanged
	}
	if conversation.Status == "manual" || conversation.Status == "closed" || conversation.Status == "paused" {
		return item, ErrAutoReplyGoalChanged
	}
	outcome, err := tx.MessageOutcome(conversation.ID, intent.BasedOnMessageKey)
	if err != nil {
		return item, err
	}
	if outcome == "manual" {
		return item, ErrAutoReplyGoalChanged
	}
	pending, err := tx.PendingMessages(conversation.ID)
	if err != nil {
		return item, err
	}
	if intent.AllowNoPending && len(pending) != 0 {
		return item, ErrAutoReplyGoalChanged
	}
	sends, err := listAutoReplyEntities[localAutoReplySend](tx, "sends", autoReplyFilter("conversation_id", conversation.ID))
	if err != nil {
		return item, err
	}
	for _, old := range sends {
		if old.ConfirmationItemID == item.ID && old.GoalContentKey == AutoReplyGoalContentKey(item) && (old.MessageKey != intent.MessageKey || old.Sequence != intent.Sequence) && (old.Status == "sending" || old.Status == "sent" || old.Status == "unknown") {
			return item, ErrAutoReplyGoalChanged
		}
	}
	return item, nil
}

// ValidateAutoReplySendIntent 在命令交给设备之前复核已登记草稿，不允许归档或修改后的追问排队继续发送。
func ValidateAutoReplySendIntent(tx *storage.AutoReplyTx, id, conversationID, content string) error {
	if id == "" {
		return nil
	} // 兼容升级前已创建的普通发送命令。
	intent, found, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", id)
	if err != nil {
		return err
	}
	if !found || intent.ConversationID != conversationID || intent.Content != content || intent.Status != "sending" {
		return ErrAutoReplyGoalChanged
	}
	if intent.ConfirmationItemID == "" {
		return nil
	}
	conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
	if err != nil {
		return err
	}
	if !found {
		return ErrAutoReplyGoalChanged
	}
	_, err = validateGoalSend(tx, conversation, intent)
	return err
}

// finishAutoReplySend 将真实发送回执及当前问题的询问记录一起提交；旧版本回执不能改写新问题状态。
func finishAutoReplySend(tx *storage.AutoReplyTx, id, status, outbound string) error {
	intent, found, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("发送意图没有找到")
	}
	if intent.Status == "sent" {
		return nil
	}
	now := time.Now().UTC()
	if status == "sent" {
		intent.OutboundMessageKey = strings.TrimSpace(outbound)
		if intent.ConfirmationItemID != "" {
			if intent.OutboundMessageKey == "" {
				return fmt.Errorf("追问回执缺少实际出站编号")
			}
			item, exists, err := getAutoReplyEntity[CandidateConfirmationItem](tx, "confirmations", intent.ConfirmationItemID)
			if err != nil {
				return err
			}
			if exists && AutoReplyGoalContentKey(item) == intent.GoalContentKey {
				item.AskCount++
				item.LastAskedAt, item.LastAskedContentKey, item.UpdatedAt = &now, intent.GoalContentKey, now
				if err = tx.Put("confirmations", item.ID, item); err != nil {
					return err
				}
			}
		}
	}
	intent.Status, intent.UpdatedAt = status, now
	return tx.Put("sends", id, intent)
}

// ConfirmAutoReplySendIntent 在设备回执入库的同一事务保存实际出站编号，随后重交回执不会重复递增询问次数。
func ConfirmAutoReplySendIntent(tx *storage.AutoReplyTx, id, conversation, content, outbound string) error {
	if id == "" {
		return nil
	}
	intent, found, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", id)
	if err != nil {
		return err
	}
	if !found || intent.ConversationID != conversation || intent.Content != content || outbound == "" {
		return fmt.Errorf("发送回执与原草稿不一致")
	}
	return finishAutoReplySend(tx, id, "sent", outbound)
}

// FinishAutoReplyGoalFollowup 只清除同一新证据事件的待发追问，不影响后续新消息与新任务。
func (c *Client) FinishAutoReplyGoalFollowup(ctx context.Context, cred AgentCredentials, conversationID, event string) error {
	return c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		conv, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil || !found {
			return err
		}
		if conv.GoalFollowup == nil || conv.GoalFollowup.EventKey != event {
			return nil
		}
		conv.GoalFollowup, conv.UpdatedAt = nil, time.Now().UTC()
		return tx.Put("conversations", conv.ID, conv)
	})
}

// currentGoalFollowup 验证新简历事件、最新消息锚点和当前目标，人工发言或新候选人消息都会阻断旧任务。
func currentGoalFollowup(tx *storage.AutoReplyTx, conv AutoReplyConversation) (CandidateConfirmationItem, bool, error) {
	var empty CandidateConfirmationItem
	followup := conv.GoalFollowup
	if followup == nil || followup.EventKind != "message" && conv.ResumeRetry != nil || conv.LastCandidateMessageKey != followup.BasedOnMessageKey || conv.LastSyncedMessageKey != followup.ExpectedLatestKey {
		return empty, false, nil
	}
	if followup.EventKind == "" || followup.EventKind == "resume" {
		candidate, found, err := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", conv.CandidateID)
		if err != nil {
			return empty, false, err
		}
		if !found || !candidate.ResumeStructured || AutoReplyResumeRevision(candidate) != followup.ResumeRevision {
			return empty, false, nil
		}
	} else if followup.EventKind != "message" {
		return empty, false, nil
	}
	intent := localAutoReplySend{ConversationID: conv.ID, MessageKey: "goal-event:" + followup.EventKey, Sequence: 1, ConfirmationItemID: followup.ConfirmationItemID, GoalVersion: followup.ExpectedGoalVersion, BasedOnMessageKey: followup.BasedOnMessageKey, ExpectedLatestKey: followup.ExpectedLatestKey, AllowNoPending: true}
	item, err := validateGoalSend(tx, conv, intent)
	if errors.Is(err, ErrAutoReplyGoalChanged) {
		return empty, false, nil
	}
	if err != nil {
		return empty, false, err
	}
	row, exists, err := tx.MessageByKey(conv.ID, conv.LastSyncedMessageKey)
	if err != nil || !exists {
		return empty, false, err
	}
	var latest AutoReplyMessage
	if err = json.Unmarshal(row.Data, &latest); err != nil {
		return empty, false, err
	}
	if latest.Direction == "self" {
		sends, err := listAutoReplyEntities[localAutoReplySend](tx, "sends", autoReplyFilter("conversation_id", conv.ID))
		if err != nil {
			return empty, false, err
		}
		owned := false
		for _, sent := range sends {
			if sent.Status == "sent" && sent.OutboundMessageKey == conv.LastSyncedMessageKey {
				owned = true
				break
			}
		}
		if !owned {
			return empty, false, nil
		}
	}
	return item, true, nil
}

// AutoReplyGoalFollowupState 从权威事务读取待发追问；无效任务由调用方清除，不重新调用模型催问。
func (c *Client) AutoReplyGoalFollowupState(ctx context.Context, cred AgentCredentials, conversationID string) (conv AutoReplyConversation, goal CandidateConfirmationItem, valid bool, err error) {
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		var found bool
		conv, found, err = getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil || !found {
			return err
		}
		goal, valid, err = currentGoalFollowup(tx, conv)
		return err
	})
	return
}

// ClaimAutoReplyMessageGoalReview 为已实际回答的新消息事件领取唯一一次目标复核，失败或重启不反复调用模型。
func (c *Client) ClaimAutoReplyMessageGoalReview(ctx context.Context, cred AgentCredentials, conversationID, messageKey, latestKey string, configVersion int) (claimed bool, err error) {
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		conversation, valid, err := messageGoalReviewConversation(tx, conversationID, messageKey, latestKey)
		if err != nil || !valid {
			return err
		}
		id := autoReplyDigest("message-goal-review|" + conversationID + "|" + messageKey)
		if _, exists, err := tx.Get("proposal_receipts", id); err != nil || exists {
			return err
		}
		claimed = true
		return tx.Put("proposal_receipts", id, AutoReplyProposalReceipt{ID: id, ConversationID: conversationID, Conversation: conversation, BasedOnMessageKey: messageKey, ConfigVersion: configVersion, Status: "goal_review_started"})
	})
	return
}

// messageGoalReviewConversation 只允许刚由 AI 回答且尚无插话或人工接管的会话继续一次目标检查。
func messageGoalReviewConversation(tx *storage.AutoReplyTx, id, messageKey, latestKey string) (conversation AutoReplyConversation, valid bool, err error) {
	conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", id)
	if err != nil || !found || conversation.LastCandidateMessageKey != messageKey || conversation.LastSyncedMessageKey != latestKey || latestKey == "" || conversation.Status == "manual" || conversation.Status == "closed" || conversation.Status == "paused" {
		return conversation, false, err
	}
	outcome, err := tx.MessageOutcome(id, messageKey)
	if err != nil || outcome == "manual" {
		return conversation, false, err
	}
	pending, err := tx.PendingMessages(id)
	if err != nil || len(pending) != 0 {
		return conversation, false, err
	}
	sends, err := listAutoReplyEntities[localAutoReplySend](tx, "sends", autoReplyFilter("conversation_id", id))
	for _, sent := range sends {
		if sent.Status == "sent" && sent.MessageKey == messageKey && sent.OutboundMessageKey == latestKey {
			return conversation, true, err
		}
	}
	return conversation, false, err
}

// CommitAutoReplyMessageGoalReview 将本次消息事件的目标更新和可选追问一起提交，独立于推荐准入收据。
func (c *Client) CommitAutoReplyMessageGoalReview(ctx context.Context, cred AgentCredentials, conversationID, messageKey, latestKey string, updates []CandidateConfirmationItem, followup ...AutoReplyGoalFollowup) error {
	return c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		conversation, valid, err := messageGoalReviewConversation(tx, conversationID, messageKey, latestKey)
		if err != nil {
			return err
		}
		if !valid {
			return ErrAutoReplyGoalChanged
		}
		id := autoReplyDigest("message-goal-review|" + conversationID + "|" + messageKey)
		receipt, found, err := getAutoReplyEntity[AutoReplyProposalReceipt](tx, "proposal_receipts", id)
		if err != nil {
			return err
		}
		if !found || receipt.Status != "goal_review_started" {
			return ErrAutoReplyGoalChanged
		}
		for _, item := range updates {
			if item.ConversationID != conversationID || item.CandidateID != conversation.CandidateID || item.PositionID != conversation.PositionID {
				return fmt.Errorf("目标复核结果与当前候选人不一致")
			}
			if _, err = putLocalAutoReplyConfirmation(tx, item); err != nil {
				return err
			}
		}
		if len(followup) > 0 {
			draft := followup[0]
			if draft.ContextRevision == "" || draft.Content == "" || len([]rune(draft.Content)) > 200 || strings.ContainsAny(draft.Content, "{}｛｝") {
				return fmt.Errorf("消息后目标追问缺少当前事实版本或有效正文")
			}
			goal, found, err := getAutoReplyEntity[CandidateConfirmationItem](tx, "confirmations", draft.ConfirmationItemID)
			if err != nil {
				return err
			}
			if found && AutoReplyGoalCanAsk(goal) && goal.ConversationID == conversationID && (draft.ExpectedContentKey == "" || draft.ExpectedContentKey == AutoReplyGoalContentKey(goal)) {
				draft.EventKind, draft.EventKey = "message", autoReplyDigest("message-goal|"+conversationID+"|"+messageKey)
				draft.BasedOnMessageKey, draft.ExpectedLatestKey, draft.ExpectedGoalVersion = messageKey, latestKey, AutoReplyConfirmationRevision(goal)
				draft.CreatedAt = time.Now().UTC()
				conversation.GoalFollowup = &draft
				if _, valid, err = currentGoalFollowup(tx, conversation); err != nil {
					return err
				} else if !valid {
					conversation.GoalFollowup = nil
				}
			}
		}
		receipt.Status = "goal_review_completed"
		if err = tx.Put("proposal_receipts", id, receipt); err != nil {
			return err
		}
		return tx.Put("conversations", conversation.ID, conversation)
	})
}
