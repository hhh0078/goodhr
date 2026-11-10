// Package cloud 本文件验证旧系统卡仅补可信元数据，原正文、摘要、时间、顺序、已处理状态与消息水位均保持不变。
package cloud

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// TestKnownSystemCardMetadataEnrichment 验证已入库摘要卡可补齐原生收到状态，重复同步不新增行也不重复修改元数据。
func TestKnownSystemCardMetadataEnrichment(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	conversation := entityTestConversation(t, c, cred)
	at := time.Date(2026, 11, 10, 1, 2, 3, 0, time.UTC)
	old := AutoReplyMessage{Fingerprint: "native-card", Direction: "system", MessageType: "card", TextContent: "测试候选人的手机号：13800138000复制手机号", CardContent: json.RawMessage(`{"summary":"原来的摘要","legacy_keep":"保留字段"}`), PlatformSentAt: &at}
	syncEntityMessages(t, c, cred, conversation, old, AutoReplyMessage{Fingerprint: "self-tail", Direction: "self", MessageType: "text", TextContent: "已收到"})
	if err := c.CompleteAutoReplyMessage(ctx, cred, conversation.ID, "native-card", "completed"); err != nil {
		t.Fatal(err)
	}
	read := func() (storage.AutoReplyRow, float64, string) {
		t.Helper()
		var row storage.AutoReplyRow
		var order float64
		var outcome string
		err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
			var err error
			var found bool
			row, found, err = tx.MessageByKey(conversation.ID, "native-card")
			if err != nil || !found {
				t.Fatal("原系统卡没有找到", err)
			}
			order, err = tx.MessageOrder(row.ID)
			if err == nil {
				outcome, err = tx.MessageOutcome(conversation.ID, "native-card")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return row, order, outcome
	}
	before, order, outcome := read()
	incoming := old
	incoming.CardContent = json.RawMessage(`{"summary":"新读取的摘要不覆盖原文","info_type":"phone","info_status":"received","phone":"13800138000","owner_name":"测试候选人","unapproved":"不能新增"}`)
	// 观察时间改变也不能覆盖原系统卡原来保存的平台时间和入库时间。
	later := at.Add(time.Hour)
	incoming.PlatformSentAt, incoming.IngestedAt = &later, later
	result, err := c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: []AutoReplyMessage{incoming}})
	if err != nil || result.Inserted != 0 || result.LastSyncedMessageKey != "self-tail" {
		t.Fatal("元数据补齐影响了消息水位或重复建行", result, err)
	}
	after, nextOrder, nextOutcome := read()
	var oldPayload, newPayload map[string]json.RawMessage
	_ = json.Unmarshal(before.Data, &oldPayload)
	_ = json.Unmarshal(after.Data, &newPayload)
	var card map[string]any
	_ = json.Unmarshal(newPayload["card_content"], &card)
	if card["summary"] != "原来的摘要" || card["legacy_keep"] != "保留字段" || card["info_type"] != "phone" || card["info_status"] != "received" || card["phone"] != "13800138000" || card["unapproved"] != nil || nextOrder != order || nextOutcome != outcome {
		t.Fatal("元数据或已封存字段变化不符", card, order, nextOrder, outcome, nextOutcome)
	}
	delete(oldPayload, "card_content")
	delete(newPayload, "card_content")
	if !reflect.DeepEqual(oldPayload, newPayload) {
		t.Fatal("补齐时修改了原消息字段")
	}
	if _, err = c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: []AutoReplyMessage{incoming}}); err != nil {
		t.Fatal(err)
	}
	repeated, _, _ := read()
	if repeated.Revision != after.Revision {
		t.Fatal("相同元数据反复更新旧消息")
	}
	// 原始顺序未知的封存行保持不变，不能触发Put将零顺序改为sequence的初始化逻辑。
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error { return tx.MarkHistoricalMessage(after.ID, "history_backfill", 0) }); err != nil {
		t.Fatal(err)
	}
	if _, err = c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: []AutoReplyMessage{incoming}}); err != nil {
		t.Fatal(err)
	}
	_, zeroOrder, historical := read()
	if zeroOrder != 0 || historical != "history_backfill" {
		t.Fatal("补齐元数据改变了未知顺序封存行", zeroOrder, historical)
	}
}

// TestKnownSystemCardMetadataRejectsConflicts 验证来源、正文或已知类型和值不一致时不修改旧数据，泛联系方式申请保持兼容。
func TestKnownSystemCardMetadataRejectsConflicts(t *testing.T) {
	conversation := AutoReplyConversation{ID: "thread", CandidateName: "测试候选人"}
	base := AutoReplyMessage{ConversationID: "thread", Fingerprint: "same", Direction: "system", MessageType: "system", TextContent: "测试候选人的手机号：13800138000复制手机号", CardContent: json.RawMessage(`{"summary":"原摘要"}`)}
	valid := base
	valid.CardContent = json.RawMessage(`{"info_type":"phone","info_status":"received","phone":"13800138000","owner_name":"测试候选人"}`)
	for _, mode := range []string{"key", "conversation", "direction", "type", "text", "owner", "masked", "kind_conflict", "value_conflict", "legacy_contact", "plain_text", "no_summary"} {
		t.Run(mode, func(t *testing.T) {
			old, incoming := base, valid
			switch mode {
			case "key":
				incoming.Fingerprint = "another"
			case "conversation":
				incoming.ConversationID = "another"
			case "direction":
				incoming.Direction = "candidate"
			case "type":
				incoming.MessageType = "card"
			case "text":
				incoming.TextContent += "变化"
			case "owner":
				incoming.CardContent = json.RawMessage(`{"info_type":"phone","info_status":"received","phone":"13800138000","owner_name":"HR名字"}`)
			case "masked":
				incoming.CardContent = json.RawMessage(`{"info_type":"phone","info_status":"received","phone":"138****8000"}`)
			case "kind_conflict":
				old.CardContent = json.RawMessage(`{"summary":"原摘要","info_type":"wechat","info_status":"received","wechat":"existing_wechat"}`)
			case "value_conflict":
				old.CardContent = json.RawMessage(`{"summary":"原摘要","info_type":"phone","info_status":"received","phone":"13900138000"}`)
			case "legacy_contact":
				old.TextContent, incoming.TextContent = "请求交换联系方式已发送", "请求交换联系方式已发送"
				old.CardContent = json.RawMessage(`{"summary":"原摘要","info_type":"contact","info_status":"requested"}`)
				incoming.CardContent = json.RawMessage(`{"summary":"新摘要","info_type":"contact","info_status":"requested"}`)
			case "plain_text":
				old.Direction, incoming.Direction = "candidate", "candidate"
				old.MessageType, incoming.MessageType = "text", "text"
			case "no_summary":
				old.CardContent = json.RawMessage(`{}`)
			}
			raw, _ := json.Marshal(old)
			if _, changed := enrichKnownSystemInfoCard(raw, incoming, conversation); changed {
				t.Fatal("冲突、普通消息或已有泛申请被改写", mode)
			}
		})
	}
	// 摘要版本尚未标注的泛联系方式申请可以补合法contact/requested，但不猜手机或微信。
	legacy := base
	legacy.TextContent = "请求交换联系方式已发送"
	newRequest := legacy
	newRequest.CardContent = json.RawMessage(`{"info_type":"contact","info_status":"requested"}`)
	raw, _ := json.Marshal(legacy)
	if enriched, changed := enrichKnownSystemInfoCard(raw, newRequest, conversation); !changed || len(enriched) == 0 {
		t.Fatal("旧泛联系方式申请不能补齐明确的申请状态")
	}
}
