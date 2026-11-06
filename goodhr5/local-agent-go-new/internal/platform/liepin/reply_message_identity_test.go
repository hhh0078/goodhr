// Package liepin 本文件验证无公开编号时，同分钟同文消息仍独立保存，且完整分钟组在重开和增量读取时编号一致。
package liepin

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/storage"
)

// identityTestMessage 构造仅有可见正文和分钟时间的合成消息，不复制真实候选人聊天。
func identityTestMessage(text, minute string) contract.FindAllItem {
	return contract.FindAllItem{Fields: map[string]string{"body_class": "im-ui-message-item-receive", "message_text": text, "message_time": minute}}
}

// TestLiepinDuplicateMessagesHaveStableDistinctKeys 验证先补早分钟、同分钟追加和重开都不改变已完整组的消息键。
func TestLiepinDuplicateMessagesHaveStableDistinctKeys(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	items := []contract.FindAllItem{identityTestMessage("锚点", "09:59"), identityTestMessage("重复问题", "10:00"), identityTestMessage("重复问题", "10:00")}
	messages, err := liepinMessagesAt(items, now)
	if err != nil || messages[1].Key == messages[2].Key {
		t.Fatal("重复消息仍被合并", messages, err)
	}
	reopened, err := liepinMessagesAt(items, now)
	if err != nil || reopened[1].Key != messages[1].Key || reopened[2].Key != messages[2].Key {
		t.Fatal("重开后编号漂移", err)
	}
	withEarlier := append([]contract.FindAllItem{identityTestMessage("重复问题", "09:58")}, items...)
	earlier, err := liepinMessagesAt(withEarlier, now)
	if err != nil || earlier[2].Key != messages[1].Key || earlier[3].Key != messages[2].Key {
		t.Fatal("补早分钟后编号漂移", err)
	}
	withNew := append(append([]contract.FindAllItem{}, items...), identityTestMessage("重复问题", "10:00"))
	newer, err := liepinMessagesAt(withNew, now)
	if err != nil || newer[1].Key != messages[1].Key || newer[2].Key != messages[2].Key || newer[3].Key == messages[2].Key {
		t.Fatal("同分钟追加后编号漂移或重复", err)
	}
}

// TestLiepinPartialMinuteCannotStopAtKnownKey 验证最新窗口恰从重复组中间开始时，不能拿第一次出现的键提前停止。
func TestLiepinPartialMinuteCannotStopAtKnownKey(t *testing.T) {
	dup := identityTestMessage("重复问题", "10:00")
	known, err := liepinMessages([]contract.FindAllItem{dup})
	if err != nil {
		t.Fatal(err)
	}
	all := []contract.FindAllItem{identityTestMessage("较早时间锚点", "09:59"), dup, dup}
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{{dup}, all}}
	result, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), []string{known[0].Key}, 5000)
	if err != nil || len(result.Messages) != 1 || result.Messages[0].Key == known[0].Key || browser.historyBatch < 1 {
		t.Fatalf("未完整分钟被当作已知边界: %+v scrolls=%d err=%v", result, browser.historyBatch, err)
	}
}

// TestLiepinCapDoesNotAssignKeysToPartialLeadingMinute 验证5000截尾首分钟不完整时，仅交付后面时间边界可证明的消息。
func TestLiepinCapDoesNotAssignKeysToPartialLeadingMinute(t *testing.T) {
	items := make([]contract.FindAllItem, 5003)
	for i := range items {
		items[i] = identityTestMessage("重复问题", "10:00")
	}
	items[len(items)-1] = identityTestMessage("最新问题", "10:01")
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}
	result, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), nil, 5000)
	if err != nil || result.HistoryComplete || len(result.Messages) != 1 || result.Messages[0].TextContent != "最新问题" {
		t.Fatalf("截尾首组猜测了重复编号: count=%d complete=%v err=%v", len(result.Messages), result.HistoryComplete, err)
	}
}

// TestLiepinLegacyDuplicateRepairKeepsPendingAndOrder 用合成89行走真实读取与SQLite同步，验证旧87行补齐不重答、不乱序。
func TestLiepinLegacyDuplicateRepairKeepsPendingAndOrder(t *testing.T) {
	ctx := context.Background()
	items := make([]contract.FindAllItem, 89)
	for index := range items {
		items[index] = identityTestMessage(fmt.Sprintf("合成消息%d", index), fmt.Sprintf("8月11日 %02d:%02d", 9+index/60, index%60))
	}
	items[56], items[84] = items[55], items[83]
	items[85].Fields["body_class"] = "im-ui-message-item-send"
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := cloud.New("")
	c.EnableLocalAutoReply(store, dir)
	cred := cloud.AgentCredentials{Token: "fixture-token", MachineID: "fixture-machine"}
	conversation, err := c.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PlatformID: "liepin", PlatformAccountID: "synthetic-account", PlatformThreadID: "synthetic-thread"})
	if err != nil {
		t.Fatal(err)
	}
	legacy := make([]cloud.AutoReplyMessage, 0, 87)
	for index, item := range items {
		if index == 56 || index == 84 {
			continue
		}
		message, baseKey, err := liepinMessage(item, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		legacy = append(legacy, cloud.AutoReplyMessage{Fingerprint: common.StableAutoReplyMessageKey(baseKey), Direction: message.Direction, MessageType: message.MessageType, TextContent: message.TextContent, PlatformSentAt: message.SentAt})
	}
	if _, err = c.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conversation.ID, HistoryComplete: true, Messages: legacy}); err != nil {
		t.Fatal(err)
	}
	if err = c.CompleteAutoReplyMessage(ctx, cred, conversation.ID, legacy[len(legacy)-2].Fingerprint, "sent"); err != nil {
		t.Fatal(err)
	}
	before, err := c.PendingAutoReplyMessages(ctx, cred, conversation.ID)
	if err != nil || len(before) != 1 {
		t.Fatal("合成旧状态错误", before, err)
	}
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}
	result, err := readLiepinConversationHistoryDetail(ctx, browser, historyAuditConfig(), []string{legacy[len(legacy)-1].Fingerprint}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	batch := make([]cloud.AutoReplyMessage, 0, len(result.Messages))
	for _, message := range result.Messages {
		batch = append(batch, cloud.AutoReplyMessage{Fingerprint: message.Key, Direction: message.Direction, MessageType: message.MessageType, TextContent: message.TextContent, PlatformSentAt: message.SentAt})
	}
	for pass := 0; pass < 2; pass++ {
		synced, err := c.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: batch})
		if err != nil || synced.Inserted != 2*(1-pass) {
			t.Fatal("补齐没有幂等", pass, synced, err)
		}
	}
	after, err := c.PendingAutoReplyMessages(ctx, cred, conversation.ID)
	if err != nil || len(after) != 1 || after[0].Fingerprint != before[0].Fingerprint {
		t.Fatal("历史补齐新增待答", after, err)
	}
	stored, err := c.AutoReplyMessages(ctx, cred, conversation.ID)
	expected, parseErr := liepinMessages(items)
	if err != nil || parseErr != nil || len(stored) != 89 {
		t.Fatal("两条历史未补齐", len(stored), err, parseErr)
	}
	for index, message := range stored {
		if message.Fingerprint != expected[index].Key {
			t.Fatalf("第%d条顺序发生变化", index)
		}
	}
}
