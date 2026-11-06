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
	conversation.UpdatedAt = now
	if err = tx.Put("conversations", conversation.ID, conversation); err != nil {
		return result, false, err
	}
	result.LastSyncedMessageKey = conversation.LastSyncedMessageKey
	result.LastCandidateMessageKey = conversation.LastCandidateMessageKey
	return result, unverified, nil
}
