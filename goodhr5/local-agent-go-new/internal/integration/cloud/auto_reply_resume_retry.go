// Package cloud 本文件持久领取简历待办并原子提交补审结果，避免进程中断导致重复收费或遗漏推荐检查。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// ClaimAutoReplyResumeRetry 在浏览器和模型操作前领取一次简历尝试；同来源最多三次，十分钟租约内不重复领取。
func (c *Client) ClaimAutoReplyResumeRetry(ctx context.Context, credentials AgentCredentials, conversationID, sourceMessageID string) (conversation AutoReplyConversation, claimed bool, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		var found bool
		conversation, found, err = getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if !found || conversation.CandidateID == "" {
			return fmt.Errorf("会话候选人尚未核实，暂不采集简历")
		}
		now := time.Now().UTC()
		sourceMessageID = strings.TrimSpace(sourceMessageID)
		retry := conversation.ResumeRetry
		if retry != nil && sourceMessageID == "" {
			sourceMessageID = retry.SourceMessageID
		}
		if retry == nil || retry.SourceMessageID != sourceMessageID {
			retry = &AutoReplyResumeRetry{SourceMessageID: sourceMessageID}
		}
		if retry.Attempts >= 3 || retry.NextAttemptAt.After(now) {
			return nil
		}
		retry.Attempts++
		retry.NextAttemptAt = now.Add(10 * time.Minute)
		leaseUntil := retry.NextAttemptAt
		retry.LeaseUntil = &leaseUntil
		retry.LastError = "简历步骤尚未完成，稍后继续"
		conversation.ResumeRetry, conversation.UpdatedAt = retry, now
		if err = tx.Put("conversations", conversation.ID, conversation); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return
}

// FinishAutoReplyResumeRetry 只结束当前来源的尝试；失败采用一分钟起的退避，成功清除待办。
func (c *Client) FinishAutoReplyResumeRetry(ctx context.Context, credentials AgentCredentials, conversationID, sourceMessageID string, success bool, reason string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("简历待办对应的会话没有找到")
		}
		retry := conversation.ResumeRetry
		if retry == nil {
			return nil
		}
		if retry.SourceMessageID != strings.TrimSpace(sourceMessageID) {
			return fmt.Errorf("简历来源已经变化，本轮结果没有覆盖新待办")
		}
		now := time.Now().UTC()
		retry.LeaseUntil = nil
		if success {
			conversation.ResumeRetry = nil
		} else {
			retry.LastError = strings.TrimSpace(reason)
			retry.NextAttemptAt = now.Add(time.Minute * time.Duration(1<<min(max(retry.Attempts-1, 0), 2)))
		}
		conversation.UpdatedAt = now
		return tx.Put("conversations", conversation.ID, conversation)
	})
}

// AutoReplyResumeRevision 对完整本地资料计算版本；头像刷新不改变简历事实或重复触发模型补审。
func AutoReplyResumeRevision(candidate AutoReplyStoredCandidate) string {
	candidate.AvatarURL = ""
	raw, _ := json.Marshal(candidate)
	return autoReplyDigest(string(raw))
}

// CountBlockedAutoReplyResumeRetries 返回当前登录账号需要人工确认后重试的简历待办数量。
func (c *Client) CountBlockedAutoReplyResumeRetries(ctx context.Context, credentials AgentCredentials) (count int, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		count, err = tx.CountBlockedResumeRetries()
		return err
	})
	return
}

// ResetAutoReplyResumeRetries 只在用户明确重试时重置当前账号耗尽的简历待办，不改已完成消息和发送凭据。
func (c *Client) ResetAutoReplyResumeRetries(ctx context.Context, credentials AgentCredentials) (count int, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		count, err = tx.ResetBlockedResumeRetries()
		return err
	})
	return
}

// CommitAutoReplyResumeProposal 只补审已回答会话的条件，并同事务结束待办、登记独立推荐收据，不产生发送动作。
func (c *Client) CommitAutoReplyResumeProposal(ctx context.Context, credentials AgentCredentials, conversationID, basedOnMessageKey, sourceMessageID, expectedResumeRevision string, configVersion int, confirmations []CandidateConfirmationItem) error {
	if configVersion <= 0 || basedOnMessageKey == "" || expectedResumeRevision == "" {
		return fmt.Errorf("简历补审所需的消息或岗位版本尚未确认")
	}
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if !found || conversation.LastCandidateMessageKey != basedOnMessageKey || conversation.ResumeRetry == nil || conversation.ResumeRetry.SourceMessageID != sourceMessageID {
			return fmt.Errorf("简历待办或候选人消息已变化，本轮补审未保存")
		}
		pending, err := tx.PendingMessages(conversationID)
		if err != nil {
			return err
		}
		if len(pending) != 0 {
			return fmt.Errorf("候选人还有新消息，请先处理消息再补审简历")
		}
		candidate, found, err := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", conversation.CandidateID)
		if err != nil {
			return err
		}
		if !found || !candidate.ResumeStructured || candidate.ResumeSourceMessageID != sourceMessageID || AutoReplyResumeRevision(candidate) != expectedResumeRevision {
			return fmt.Errorf("简历资料已变化，本轮补审未保存")
		}
		for _, item := range confirmations {
			if item.ConversationID != conversationID || item.CandidateID != conversation.CandidateID || item.PositionID != conversation.PositionID {
				return fmt.Errorf("确认项与本轮候选人或岗位不一致")
			}
			if _, err = putLocalAutoReplyConfirmation(tx, item); err != nil {
				return err
			}
		}
		conversation.ResumeRetry = nil
		conversation.UpdatedAt = time.Now().UTC()
		if err = tx.Put("conversations", conversationID, conversation); err != nil {
			return err
		}
		receiptID := autoReplyDigest(fmt.Sprintf("resume|%s|%s|%d|%s|%s", conversationID, basedOnMessageKey, configVersion, sourceMessageID, expectedResumeRevision))
		_, exists, err := tx.Get("proposal_receipts", receiptID)
		if err != nil || exists {
			return err
		}
		return tx.Put("proposal_receipts", receiptID, AutoReplyProposalReceipt{ID: receiptID, ConversationID: conversationID, Conversation: conversation, BasedOnMessageKey: basedOnMessageKey, ConfigVersion: configVersion, Status: "pending"})
	})
}
