// Package cloud 本文件保护同一原生身份已核实的交换手机号，并将联系方式冲突幂等记入现有运行账本，不改写附件原文或新增数据表。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// comparableAutoReplyContact 只归一完整联系方式的常见书写格式，掩码及不合法值不会被剥成另一个号码。
func comparableAutoReplyContact(kind, value string) string {
	value = strings.TrimSpace(value)
	if kind == "wechat" {
		if nativeWechatValue.MatchString(value) {
			return value
		}
		return ""
	}
	if kind != "phone" {
		return ""
	}
	value = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", "＋", "+").Replace(value)
	if !nativePhoneValue.MatchString(value) {
		return ""
	}
	for _, prefix := range []string{"+86", "0086"} {
		if strings.HasPrefix(value, prefix) {
			local := strings.TrimPrefix(value, prefix)
			if len(local) == 11 && local[0] == '1' && local[1] >= '3' && local[1] <= '9' {
				return local
			}
		}
	}
	return value
}

// AutoReplyContactValuesEqual 判断两个完整联系方式是否仅格式不同，不推断掩码、空值或不同国家的号码相同。
func AutoReplyContactValuesEqual(kind, first, second string) bool {
	value := comparableAutoReplyContact(kind, first)
	return value != "" && value == comparableAutoReplyContact(kind, second)
}

// storedContactConflictSource 只从当前会话寻找已同步的来源，设备或AI提供的其它会话编号不能借来登记冲突。
func storedContactConflictSource(tx *storage.AutoReplyTx, conversation AutoReplyConversation, source string) (AutoReplyMessage, bool, error) {
	if source == "" {
		return AutoReplyMessage{}, false, nil
	}
	row, found, err := tx.MessageByKey(conversation.ID, source)
	if err != nil {
		return AutoReplyMessage{}, false, err
	}
	var message AutoReplyMessage
	if found {
		err = json.Unmarshal(row.Data, &message)
	} else {
		message, found, err = getAutoReplyEntity[AutoReplyMessage](tx, "messages", source)
	}
	return message, found && message.ConversationID == conversation.ID, err
}

// nativeReceivedPhoneEvidence 仅相信平台解析的系统交换卡，归属姓名、可见原文及完整号码必须与当前会话和已有号码一致。
func nativeReceivedPhoneEvidence(message AutoReplyMessage, conversation AutoReplyConversation, phone string) bool {
	if message.ConversationID != conversation.ID || message.Direction != "system" || message.MessageType == "text" {
		return false
	}
	var card struct {
		InfoType   string `json:"info_type"`
		InfoStatus string `json:"info_status"`
		Phone      string `json:"phone"`
		OwnerName  string `json:"owner_name"`
		Summary    string `json:"summary"`
	}
	if json.Unmarshal(message.CardContent, &card) != nil || card.InfoType != "phone" || card.InfoStatus != model.AutoReplyInfoReceived || card.OwnerName == "" || card.OwnerName != strings.TrimSpace(conversation.CandidateName) || strings.TrimSpace(card.Summary) == "" || strings.TrimSpace(card.Summary) != strings.TrimSpace(message.TextContent) {
		return false
	}
	visible := strings.Join(strings.Fields(message.TextContent), "")
	return AutoReplyContactValuesEqual("phone", card.Phone, phone) && strings.Contains(visible, card.OwnerName) && strings.Contains(visible, comparableAutoReplyContact("phone", card.Phone))
}

// recordAutoReplyContactConflictTx 在现有运行账本只记一次固定提示，描述及稳定编号不含具体联系方式。
func recordAutoReplyContactConflictTx(tx *storage.AutoReplyTx, conversation AutoReplyConversation, kind string, message AutoReplyMessage) error {
	if kind != "phone" && kind != "wechat" || conversation.ID == "" || conversation.PositionID == "" || message.ID == "" || message.ConversationID != conversation.ID {
		return fmt.Errorf("联系方式冲突的会话或来源尚未核实")
	}
	id := "contact-conflict:" + autoReplyDigest(conversation.ID+"|"+conversation.PositionID+"|"+kind+"|"+message.ID)
	old, err := tx.ServerLedger("runtime_events", id, nil)
	if err != nil || len(old) > 0 {
		return err
	}
	event := struct {
		ConversationID  string    `json:"conversation_id"`
		PositionID      string    `json:"position_id"`
		SourceMessageID string    `json:"source_message_id"`
		ContactKind     string    `json:"contact_kind"`
		Title           string    `json:"title"`
		Status          string    `json:"status"`
		Description     string    `json:"description"`
		OccurredAt      time.Time `json:"occurred_at"`
	}{conversation.ID, conversation.PositionID, message.ID, kind, "联系方式待核对", "warning", "联系方式待核对：平台交换与已有资料不一致，已保留原资料，普通问答继续。", time.Now().UTC()}
	_, err = tx.ServerLedger("runtime_events", id, event)
	return err
}

// RecordAutoReplyContactConflict 复用当前权威仓储保存冲突事件，找不到真实来源或服务器保存失败时仍返回错误。
func (c *Client) RecordAutoReplyContactConflict(ctx context.Context, credentials AgentCredentials, conversationID, kind, source string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if !found || conversation.CandidateID == "" {
			return fmt.Errorf("联系方式冲突的候选人会话已不存在")
		}
		message, found, err := storedContactConflictSource(tx, conversation, source)
		if err != nil {
			return err
		}
		if !found || message.Direction != "candidate" && message.Direction != "system" {
			return fmt.Errorf("联系方式冲突尚无当前候选人的真实来源")
		}
		return recordAutoReplyContactConflictTx(tx, conversation, kind, message)
	})
}

// protectConfirmedNativePhoneTx 只有actor、同一原生身份和真实交换卡三者同号时保护手机号，缺强证据继续旧合并规则。
func protectConfirmedNativePhoneTx(tx *storage.AutoReplyTx, candidate AutoReplyStoredCandidate, identity CandidatePlatformIdentity, input AutoReplyCandidateInput) (AutoReplyCandidateInput, error) {
	if input.Phone == "" || candidate.ID == "" || identity.ID == "" || identity.CandidateID != candidate.ID || identity.PlatformID != input.PlatformID || identity.PlatformAccountID != input.PlatformAccountID || identity.PlatformCandidateID != input.PlatformCandidateID || !AutoReplyContactValuesEqual("phone", candidate.Phone, identity.NormalizedPhone) {
		return input, nil
	}
	conversations, err := listAutoReplyEntities[AutoReplyConversation](tx, "conversations", autoReplyFilter("candidate_id", candidate.ID), autoReplyFilter("identity_id", identity.ID), autoReplyFilter("position_id", input.PositionID))
	if err != nil {
		return input, err
	}
	for _, conversation := range conversations {
		if conversation.PlatformID != input.PlatformID || conversation.PlatformAccountID != input.PlatformAccountID || conversation.InfoRequests["phone"].Status != model.AutoReplyInfoReceived {
			continue
		}
		source, found, err := storedContactConflictSource(tx, conversation, input.ResumeSourceMessageID)
		if err != nil {
			return input, err
		}
		if !found || source.Direction != "candidate" {
			continue
		}
		messages, err := listAutoReplyEntities[AutoReplyMessage](tx, "messages", autoReplyFilter("conversation_id", conversation.ID), autoReplyFilter("direction", "system"))
		if err != nil {
			return input, err
		}
		for _, message := range messages {
			if !nativeReceivedPhoneEvidence(message, conversation, candidate.Phone) {
				continue
			}
			if !AutoReplyContactValuesEqual("phone", candidate.Phone, input.Phone) {
				if err = recordAutoReplyContactConflictTx(tx, conversation, "phone", source); err != nil {
					return input, err
				}
			}
			input.Phone = candidate.Phone
			return input, nil
		}
	}
	return input, nil
}
