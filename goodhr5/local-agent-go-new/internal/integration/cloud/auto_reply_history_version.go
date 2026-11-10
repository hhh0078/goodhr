// Package cloud 本文件把旧消息键版本会话登记为现有采集待办，只在当前账号与已开启岗位范围内修复历史。
package cloud

import (
	"context"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// AutoReplyHistoryRefreshScope 明确历史重采的账号、岗位和可选单会话边界，不授予额外浏览器操作权限。
type AutoReplyHistoryRefreshScope struct {
	PlatformID, PlatformAccountID string
	Version                       int
	PositionIDs                   []string
	Target                        *AutoReplyCaptureIntent
}

// QueueAutoReplyHistoryRefresh 幂等登记旧版本已有会话；成功同步已升级的会话不会再次进入采集待办。
func (c *Client) QueueAutoReplyHistoryRefresh(ctx context.Context, credentials AgentCredentials, scope AutoReplyHistoryRefreshScope) error {
	if scope.Version <= 0 || len(scope.PositionIDs) == 0 || scope.PlatformID == "" || scope.PlatformAccountID == "" {
		return nil
	}
	positions := make(map[string]bool, len(scope.PositionIDs))
	for _, id := range scope.PositionIDs {
		if id = strings.TrimSpace(id); id != "" {
			positions[id] = true
		}
	}
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		filters := []storage.AutoReplyFilter{autoReplyFilter("platform_id", scope.PlatformID), autoReplyFilter("account_id", scope.PlatformAccountID)}
		if scope.Target != nil {
			if scope.Target.PlatformThreadID == "" || scope.Target.CandidateName == "" || scope.Target.PlatformCandidateID == "" {
				return nil
			}
			filters = append(filters, autoReplyFilter("thread_id", scope.Target.PlatformThreadID))
		}
		items, err := listAutoReplyEntities[AutoReplyConversation](tx, "conversations", filters...)
		if err != nil {
			return err
		}
		for _, item := range items {
			if !positions[item.PositionID] || item.MessageIdentityVersion >= scope.Version || item.CaptureIntent != nil || item.LastSyncedMessageKey == "" || item.PlatformThreadID == "" || item.CandidateName == "" {
				continue
			}
			intent := AutoReplyCaptureIntent{PositionID: item.PositionID, PlatformThreadID: item.PlatformThreadID, CandidateName: item.CandidateName, PagePositionText: item.PagePositionText}
			if target := scope.Target; target != nil {
				if item.CandidateName != target.CandidateName {
					continue
				}
				if item.PlatformIdentityID != "" {
					identity, found, err := getAutoReplyEntity[CandidatePlatformIdentity](tx, "identities", item.PlatformIdentityID)
					if err != nil {
						return err
					}
					if found && identity.PlatformCandidateID != "" && identity.PlatformCandidateID != target.PlatformCandidateID {
						continue
					}
				}
				intent.PlatformCandidateID = target.PlatformCandidateID
			}
			item.CaptureIntent = &intent
			item.UpdatedAt = time.Now().UTC()
			if err = tx.Put("conversations", item.ID, item); err != nil {
				return err
			}
		}
		return nil
	})
}
