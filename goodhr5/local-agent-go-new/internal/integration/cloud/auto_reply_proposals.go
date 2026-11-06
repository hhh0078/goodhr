// Package cloud 本文件保存AI判断提交收据，确保消息发送或进程重启不会丢失后续推荐检查。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"time"
)

// AutoReplyProposalReceipt 是与确认项同事务落盘的后续工作收据，不含凭证或模型密钥。
type AutoReplyProposalReceipt struct {
	Attempts          int                   `json:"attempts"`
	NextAttemptAt     time.Time             `json:"next_attempt_at"`
	LastError         string                `json:"last_error"`
	ID                string                `json:"id"`
	ConversationID    string                `json:"conversation_id"`
	Conversation      AutoReplyConversation `json:"conversation"`
	BasedOnMessageKey string                `json:"based_on_message_key"`
	ConfigVersion     int                   `json:"config_version"`
	Status            string                `json:"status"`
}

// PendingAutoReplyProposals 有界读取尚未完成推荐资格检查的已提交判断。
func (c *Client) PendingAutoReplyProposals(ctx context.Context, credentials AgentCredentials) (items []AutoReplyProposalReceipt, err error) {
	items = []AutoReplyProposalReceipt{}
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		rows, err := tx.PendingProposalReceipts(30)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var item AutoReplyProposalReceipt
			if err = json.Unmarshal(row.Data, &item); err != nil {
				return err
			}
			items = append(items, item)
		}
		return nil
	})
	return
}

// CompleteAutoReplyProposal 只在后续任务成功登记或已证明陈旧后完成收据。
func (c *Client) CompleteAutoReplyProposal(ctx context.Context, credentials AgentCredentials, id string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		item, found, err := getAutoReplyEntity[AutoReplyProposalReceipt](tx, "proposal_receipts", id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("判断收据没有找到")
		}
		item.Status = "completed"
		return tx.Put("proposal_receipts", id, item)
	})
}

// FailAutoReplyProposal 保存失败原因并退避，让暂时不可用的旧收据不阻挡其他候选人。
func (c *Client) FailAutoReplyProposal(ctx context.Context, credentials AgentCredentials, id, reason string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		item, found, err := getAutoReplyEntity[AutoReplyProposalReceipt](tx, "proposal_receipts", id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("判断收据没有找到")
		}
		if item.Status == "completed" {
			return nil
		}
		item.Attempts++
		item.LastError = reason
		item.NextAttemptAt = time.Now().UTC().Add(time.Duration(min(item.Attempts, 15)) * time.Minute)
		return tx.Put("proposal_receipts", id, item)
	})
}
