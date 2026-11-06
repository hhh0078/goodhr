// Package cloud 本文件原子入库聊天差量，并区分新消息、补采历史与无法定位的旧历史。
package cloud

import (
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"time"
)

// syncLocalAutoReplyMessages 不把补采的旧消息推进到处理水位，完整历史无交集时保守隔离。
func syncLocalAutoReplyMessages(tx *storage.AutoReplyTx, input AutoReplyMessageSyncRequest) (AutoReplyMessageSyncResult, bool, error) {
	result := AutoReplyMessageSyncResult{}
	conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", input.ConversationID)
	if err != nil {
		return result, false, err
	}
	if !found {
		return result, false, fmt.Errorf("本地会话没找到")
	}
	upgrading := input.MessageIdentityVersion > conversation.MessageIdentityVersion
	if upgrading && len(input.Messages) == 0 && conversation.LastSyncedMessageKey == "" {
		// 新的空会话没有历史可迁移，保持普通空同步；既不升级，也不制造反复采集待办。
		upgrading = false
	}
	if upgrading && (!input.HistoryComplete || len(input.Messages) == 0) {
		return result, false, fmt.Errorf("完整聊天历史尚未读到顶部，本轮消息标识版本没有升级")
	}
	known := make([]bool, len(input.Messages))
	orders := make([]float64, len(input.Messages))
	lastKnown := -1
	for index, item := range input.Messages {
		key := localAutoReplyMessageKey(item)
		if key == "" {
			return result, false, fmt.Errorf("消息缺少稳定编号")
		}
		existing, exists, err := tx.MessageByKey(input.ConversationID, key)
		if err != nil {
			return result, false, err
		}
		known[index] = exists
		if exists {
			lastKnown = index
			orders[index], err = tx.MessageOrder(existing.ID)
			if err != nil {
				return result, false, err
			}
		}
	}
	unverified := conversation.LastSyncedMessageKey != "" && input.HistoryComplete && lastKnown < 0 && len(input.Messages) > 0
	if upgrading && unverified {
		// 迁移不能猜测新旧消息边界；保留采集意图和原版本，避免下一轮把隔离数据当成有效锚点。
		return result, false, fmt.Errorf("完整历史没有对上旧消息锚点，本轮没有回填或升级，请人工核对")
	}
	now := time.Now().UTC()
	for index, item := range input.Messages {
		if known[index] {
			continue
		}
		key := localAutoReplyMessageKey(item)
		item.ID = "message-" + autoReplyDigest(input.ConversationID+key)
		item.ConversationID = input.ConversationID
		item.CreatedAt = now
		item.IngestedAt = now
		if err = tx.Put("messages", item.ID, item); err != nil {
			return result, false, err
		}
		result.Inserted++
		if unverified {
			if err = tx.MarkHistoricalMessage(item.ID, "legacy_unverified", 0); err != nil {
				return result, false, err
			}
			continue
		}
		if index < lastKnown {
			next := index + 1
			for !known[next] {
				next++
			}
			previous := index - 1
			for previous >= 0 && !known[previous] {
				previous--
			}
			right := orders[next]
			left := right - float64(next+1)
			if previous >= 0 {
				left = orders[previous]
			}
			order := left + (right-left)*float64(index-previous)/float64(next-previous)
			if err = tx.MarkHistoricalMessage(item.ID, "history_backfill", order); err != nil {
				return result, false, err
			}
			continue
		}
		conversation.LastSyncedMessageKey = key
		if item.Direction == "candidate" {
			conversation.LastCandidateMessageKey = key
		}
	}
	conversation.HistoryComplete = conversation.HistoryComplete || input.HistoryComplete
	if upgrading {
		conversation.MessageIdentityVersion = input.MessageIdentityVersion
	}
	if input.CaptureCompleted {
		conversation.CaptureIntent = nil
	}
	conversation.UpdatedAt = now
	if err = tx.Put("conversations", conversation.ID, conversation); err != nil {
		return result, false, err
	}
	result.LastSyncedMessageKey = conversation.LastSyncedMessageKey
	result.LastCandidateMessageKey = conversation.LastCandidateMessageKey
	return result, unverified, nil
}
