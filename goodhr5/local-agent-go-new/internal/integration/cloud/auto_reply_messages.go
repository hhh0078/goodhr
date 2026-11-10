// Package cloud 本文件原子入库聊天差量，并区分新消息、补采历史与无法定位的旧历史。
package cloud

import (
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"strings"
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
	if b := conversation.DeletionBoundary; b != nil {
		for _, m := range input.Messages {
			if m.PlatformSentAt == nil || !m.PlatformSentAt.After(b.DeletedAt) {
				return result, false, fmt.Errorf("重新联系的会话不接收删除前或时间不明的历史")
			}
		}
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
	verifiedTail := -1
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
			if key == conversation.LastSyncedMessageKey {
				verifiedTail = index
			}
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
	// 系统提示可紧随候选人消息，只有最后一条真实对话才可能与列表摘要构成同分钟见证。
	lastDialog := len(input.Messages) - 1
	for lastDialog >= 0 && input.Messages[lastDialog].Direction == "system" {
		lastDialog--
	}
	now := time.Now().UTC()
	for index, item := range input.Messages {
		if known[index] {
			// 旧系统卡只补确定的原生元数据；未知顺序的封存行不因Put的初始化规则改变排序。
			if orders[index] != 0 {
				row, found, readErr := tx.MessageByKey(input.ConversationID, localAutoReplyMessageKey(item))
				if readErr != nil {
					return result, false, readErr
				}
				if found {
					if raw, enriched := enrichKnownSystemInfoCard(row.Data, item, conversation); enriched {
						if err = tx.Put("messages", row.ID, raw); err != nil {
							return result, false, err
						}
					}
				}
			}
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
		// 全部联系人初次核对可看到过往来信。保留它们作为上下文，只给监听开始后的来信排回复。
		if verifiedTail < 0 && item.Direction == "candidate" && !input.ReplyAfter.IsZero() && (item.PlatformSentAt == nil || !item.PlatformSentAt.After(input.ReplyAfter)) {
			witness := index == lastDialog && item.PlatformSentAt != nil && item.PlatformSentAt.Truncate(time.Minute).Equal(input.ReplyAfter.Truncate(time.Minute)) &&
				input.WitnessDigest != "" && input.WitnessDigest == autoReplyDigest(strings.TrimSpace(item.TextContent))
			if !witness {
				order, err := tx.MessageOrder(item.ID)
				if err != nil {
					return result, false, err
				}
				if err = tx.MarkHistoricalMessage(item.ID, "history_backfill", order); err != nil {
					return result, false, err
				}
			}
		}
		conversation.LastSyncedMessageKey = key
		if conversation.GoalFollowup != nil && key != conversation.GoalFollowup.ExpectedLatestKey && (item.Direction == "candidate" || item.Direction == "self") {
			conversation.GoalFollowup = nil
		}
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

// knownSystemInfoMetadata 只允许补入已核实的资料种类、状态及完整联系方式，其它卡片字段保持原样。
type knownSystemInfoMetadata struct {
	InfoType   string `json:"info_type"`
	InfoStatus string `json:"info_status"`
	Phone      string `json:"phone"`
	Wechat     string `json:"wechat"`
	OwnerName  string `json:"owner_name"`
}

// enrichKnownSystemInfoCard 仅同源补齐系统卡或候选人的原生简历邀请，保留原摘要、正文、时间、未知字段和稳定编号。
func enrichKnownSystemInfoCard(raw json.RawMessage, incoming AutoReplyMessage, conversation AutoReplyConversation) (json.RawMessage, bool) {
	var original AutoReplyMessage
	if json.Unmarshal(raw, &original) != nil || localAutoReplyMessageKey(original) == "" || localAutoReplyMessageKey(original) != localAutoReplyMessageKey(incoming) || original.ConversationID != conversation.ID || incoming.ConversationID != "" && incoming.ConversationID != conversation.ID || incoming.Direction != original.Direction || incoming.MessageType != original.MessageType || incoming.TextContent != original.TextContent || original.MessageType != "system" && original.MessageType != "card" {
		return nil, false
	}
	var old, next knownSystemInfoMetadata
	var fields map[string]json.RawMessage
	if json.Unmarshal(original.CardContent, &old) != nil || json.Unmarshal(incoming.CardContent, &next) != nil || json.Unmarshal(original.CardContent, &fields) != nil || fields == nil {
		return nil, false
	}
	var summary string
	if json.Unmarshal(fields["summary"], &summary) != nil || strings.TrimSpace(summary) == "" {
		return nil, false
	}
	offer := original.Direction == "candidate" && original.MessageType == "card" && next.InfoType == "resume" && next.InfoStatus == "offered" && next.Phone == "" && next.Wechat == "" && next.OwnerName == ""
	if !offer && (original.Direction != "system" || next.InfoType != "resume" && next.InfoType != "phone" && next.InfoType != "wechat" && next.InfoType != "contact" || next.InfoStatus != "requested" && next.InfoStatus != "received" && next.InfoStatus != "declined") {
		return nil, false
	}
	// 泛联系方式申请是旧版已保存的合法类型，但不能据此把同一条泛提示改为另一种确定资料。
	if next.InfoType == "contact" && next.InfoStatus != "requested" || old.InfoType != "" && old.InfoType != next.InfoType || old.InfoStatus != "" && old.InfoStatus != next.InfoStatus || next.OwnerName != "" && next.OwnerName != strings.TrimSpace(conversation.CandidateName) {
		return nil, false
	}
	if next.InfoStatus == "received" && (next.InfoType == "phone" && (!nativePhoneValue.MatchString(next.Phone) || !strings.Contains(original.TextContent, next.Phone)) || next.InfoType == "wechat" && (!nativeWechatValue.MatchString(next.Wechat) || !strings.Contains(original.TextContent, next.Wechat))) {
		return nil, false
	}
	if next.InfoType == "phone" && (next.Wechat != "" || old.Wechat != "") || next.InfoType == "wechat" && (next.Phone != "" || old.Phone != "") {
		return nil, false
	}
	previous := map[string]string{"info_type": old.InfoType, "info_status": old.InfoStatus, "phone": old.Phone, "wechat": old.Wechat, "owner_name": old.OwnerName}
	values := map[string]string{"info_type": next.InfoType, "info_status": next.InfoStatus, "phone": next.Phone, "wechat": next.Wechat, "owner_name": next.OwnerName}
	changed := false
	for key, value := range values {
		if value == "" || value == previous[key] {
			continue
		}
		if previous[key] != "" {
			return nil, false
		}
		fields[key], _ = json.Marshal(value)
		changed = true
	}
	if !changed {
		return nil, false
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(raw, &payload) != nil {
		return nil, false
	}
	payload["card_content"], _ = json.Marshal(fields) // 仅替换白名单元数据，原summary及其它字段仍来自旧记录。
	enriched, err := json.Marshal(payload)
	return enriched, err == nil
}
