// Package cloud 本文件在已认证本地账号事务中保存招聘平台全部联系人列表的匿名观测和核对凭据。
package cloud

import (
	"context"
	"goodhr5/local-agent-go-new/internal/storage"
	"time"
)

// AutoReplyListObservation 表示平台列表的一行可见证据，持久化只保留匿名指纹。
type AutoReplyListObservation = storage.AutoReplyListObservation

// AutoReplyListReview 表示需要在真实聊天中核对的一次观察版本。
type AutoReplyListReview = storage.AutoReplyListReview

// ObserveAutoReplyList 仅在本地提交该平台账号的一份稳定列表快照与新待办。
func (c *Client) ObserveAutoReplyList(ctx context.Context, cred AgentCredentials, platformID, accountID, targetScope string, rows []AutoReplyListObservation, now time.Time) (reviews []AutoReplyListReview, err error) {
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		var e error
		reviews, e = tx.ObserveAutoReplyList(platformID, accountID, targetScope, rows, now)
		return e
	})
	return reviews, err
}

// AckAutoReplyList 在同一所有者和扫描范围内按观察版本确认，只清除已成功核对的待办。
func (c *Client) AckAutoReplyList(ctx context.Context, cred AgentCredentials, platformID, accountID, targetScope string, review AutoReplyListReview) error {
	return c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		return tx.AckAutoReplyList(platformID, accountID, targetScope, review)
	})
}
