// Package cloud 本文件复用发送账本保存候选人原生附件邀约的一次性同意结果，并为新的真实附件来源登记原有简历待办。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// AutoReplyResumeOfferContent 是原生同意操作的固定账本文案，不能混同为平台索要简历。
const AutoReplyResumeOfferContent = "接收候选人简历附件"

// AutoReplyResumeOfferSendKey 按原生邀约消息独立防重，同一候选人的不同邀约拥有不同操作凭据。
func AutoReplyResumeOfferSendKey(offer string) string { return "resume-offer:" + offer }

// ValidateAutoReplyResumeOfferSource 只认可当前会话真实候选人卡片；删除前来源、人工关闭和未核实来源均不能派发同意。
func ValidateAutoReplyResumeOfferSource(tx *storage.AutoReplyTx, conversationID, offer string) error {
	conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
	if err != nil {
		return err
	}
	if !found || conversation.CandidateID == "" || offer == "" || conversation.Status == "manual" || conversation.Status == "closed" || conversation.Status == "paused" {
		return fmt.Errorf("%w：简历邀请所属会话已变化", ErrAutoReplyGoalChanged)
	}
	message, err := resumeOfferSourceMessage(tx, conversationID, offer)
	if err != nil {
		return err
	}
	var card struct {
		InfoType   string `json:"info_type"`
		InfoStatus string `json:"info_status"`
	}
	if json.Unmarshal(message.CardContent, &card) != nil || message.Direction != "candidate" || message.MessageType != "card" || card.InfoType != "resume" || card.InfoStatus != "offered" || message.PlatformSentAt == nil {
		return fmt.Errorf("%w：还没有核实这条原生简历邀请", ErrAutoReplyGoalChanged)
	}
	if b := conversation.DeletionBoundary; b != nil && !message.PlatformSentAt.After(b.DeletedAt) {
		return fmt.Errorf("%w：旧简历邀请不能用于重新建档", ErrAutoReplyGoalChanged)
	}
	return nil
}

// resumeOfferSourceMessage 通过服务器同一账号和会话的原消息键查找来源，不接受另一个会话的消息编号。
func resumeOfferSourceMessage(tx *storage.AutoReplyTx, conversationID, key string) (AutoReplyMessage, error) {
	var message AutoReplyMessage
	row, found, err := tx.MessageByKey(conversationID, key)
	if err != nil {
		return message, err
	}
	if !found || json.Unmarshal(row.Data, &message) != nil || message.ConversationID != conversationID {
		return message, fmt.Errorf("%w：简历来源消息尚未同步", ErrAutoReplyGoalChanged)
	}
	return message, nil
}

// CommitAutoReplyResumeOffer 在新文件原消息同步后原子落同意回执及简历待办，原邀约不再作为岗位问题回复。
func (c *Client) CommitAutoReplyResumeOffer(ctx context.Context, cred AgentCredentials, conversationID, offer, source string) (conversation AutoReplyConversation, err error) {
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		if err := ValidateAutoReplyResumeOfferSource(tx, conversationID, offer); err != nil {
			return err
		}
		if source == "" || source == offer {
			return fmt.Errorf("真正的附件来源不能沿用简历邀请编号")
		}
		file, err := resumeOfferSourceMessage(tx, conversationID, source)
		if err != nil {
			return err
		}
		invitation, err := resumeOfferSourceMessage(tx, conversationID, offer)
		if err != nil {
			return err
		}
		var card struct {
			InfoType   string `json:"info_type"`
			InfoStatus string `json:"info_status"`
		}
		if json.Unmarshal(file.CardContent, &card) != nil || file.Direction != "candidate" || file.MessageType != "resume" || card.InfoType != "resume" || card.InfoStatus != "received" || file.PlatformSentAt == nil || file.PlatformSentAt.Before(*invitation.PlatformSentAt) {
			return fmt.Errorf("原生同意后尚未核实新的附件消息")
		}
		conversation, _, err = getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if b := conversation.DeletionBoundary; b != nil && !file.PlatformSentAt.After(b.DeletedAt) {
			return fmt.Errorf("删除前附件不能用于本轮邀请")
		}
		id := AutoReplySendIntentID(conversationID, AutoReplyResumeOfferSendKey(offer), 0)
		intent, found, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", id)
		if err != nil {
			return err
		}
		if !found || intent.ConversationID != conversationID || intent.Content != AutoReplyResumeOfferContent || intent.Status != "sending" && intent.Status != "sent" || intent.Status == "sent" && intent.OutboundMessageKey != source {
			return fmt.Errorf("同意简历邀请缺少有效且未失效的独立操作账本")
		}
		if intent.Status == "sent" {
			// 首次同意已经原子保存文件待办；重复回执不能覆盖后来新文件的进度。
			return nil
		}
		if err = finishAutoReplySend(tx, id, "sent", source); err != nil {
			return err
		}
		row, _, err := tx.MessageByKey(conversationID, offer)
		if err != nil {
			return err
		}
		outcome, err := tx.MessageOutcome(conversationID, offer)
		if err != nil {
			return err
		}
		if outcome == "" {
			order, err := tx.MessageOrder(row.ID)
			if err != nil {
				return err
			}
			if err = tx.MarkHistoricalMessage(row.ID, "completed", order); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		candidate, found, err := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", conversation.CandidateID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("候选人资料已移除，暂不下载附件")
		}
		if (!candidate.ResumeStructured || candidate.ResumeSourceMessageID != source) && (conversation.ResumeRetry == nil || conversation.ResumeRetry.SourceMessageID != source) {
			conversation.ResumeRetry = &AutoReplyResumeRetry{SourceMessageID: source, NextAttemptAt: now}
		}
		if conversation.InfoRequests == nil {
			conversation.InfoRequests = map[string]AutoReplyInfoRequestProgress{}
		}
		conversation.InfoRequests["resume"] = AutoReplyInfoRequestProgress{Status: "received", LastAttemptMessageKey: source, UpdatedAt: now}
		conversation.UpdatedAt = now
		return tx.Put("conversations", conversation.ID, conversation)
	})
	return
}

// IsAutoReplyResumeOfferMessage 只识别已存原生邀请标记，不把普通“我可以发简历吗”文字当作可点击按钮。
func IsAutoReplyResumeOfferMessage(message AutoReplyMessage) bool {
	var card struct {
		InfoType   string `json:"info_type"`
		InfoStatus string `json:"info_status"`
	}
	return message.Direction == "candidate" && message.MessageType == "card" && json.Unmarshal(message.CardContent, &card) == nil && card.InfoType == "resume" && strings.TrimSpace(card.InfoStatus) == "offered"
}
