// Package cloud 本文件在点击清除平台未读前登记采集意图，复用本机会话行恢复尚未读到正文的会话。
package cloud

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// BeginAutoReplyCapture 先事务保存列表身份和采集意图；不覆盖已核实资料，不创建正式候选人或关联岗位。
func (c *Client) BeginAutoReplyCapture(ctx context.Context, credentials AgentCredentials, platformID, accountID, conversationKey string, intent AutoReplyCaptureIntent) error {
	platformID, accountID, conversationKey = strings.TrimSpace(platformID), strings.TrimSpace(accountID), strings.TrimSpace(conversationKey)
	if platformID == "" || accountID == "" || conversationKey == "" || strings.TrimSpace(intent.CandidateName) == "" {
		return fmt.Errorf("采集会话前需要确认招聘账号、稳定会话编号和候选人姓名")
	}
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		items, err := listAutoReplyEntities[AutoReplyConversation](tx, "conversations", autoReplyFilter("platform_id", platformID), autoReplyFilter("account_id", accountID), autoReplyFilter("thread_id", conversationKey))
		if err != nil {
			return err
		}
		if len(items) > 1 {
			return fmt.Errorf("恢复线索对应多个会话，请先确认当前会话")
		}
		// 明确来源已删除时，即使新设备没有老会话行，也只能先通过重新联系核实。
		boundary, err := readContactBoundary(tx, AutoReplyCandidateLookup{PlatformID: platformID, PlatformAccountID: accountID, PlatformThreadID: conversationKey, PlatformCandidateID: intent.PlatformCandidateID})
		if err != nil {
			return err
		}
		if boundary != nil && (len(items) == 0 || items[0].DeletionBoundary == nil || items[0].DeletionBoundary.Sequence != boundary.Sequence) {
			return storage.ErrAutoReplyCandidateDeleted
		}
		now := time.Now().UTC()
		item := AutoReplyConversation{ID: "conversation-" + autoReplyDigest(platformID+"|"+accountID+"|"+conversationKey), PlatformID: platformID, PlatformAccountID: accountID, PlatformThreadID: conversationKey, CreatedAt: now}
		if len(items) == 1 {
			item = items[0]
			if item.CaptureIntent != nil {
				return nil
			}
		}
		item.CaptureIntent = &intent
		item.UpdatedAt = now
		return tx.Put("conversations", item.ID, item)
	})
}
