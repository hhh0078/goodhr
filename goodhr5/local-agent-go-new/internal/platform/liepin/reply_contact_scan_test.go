// Package liepin 文件作用：验证全部联系人入口和每行的持久变化线索，避免依赖未读标签或入口角标。
package liepin

import (
	"context"
	"slices"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// allContactFixture 只提供全部标签、抽屉和行，任何查找未读标签或悬浮数字的实现都无法通过。
func allContactFixture() (*Runtime, *liepinReplyBrowserStub, model.Config) {
	cfg := model.Config{ID: "liepin", Name: "测试猎聘", Selectors: map[string]contract.SelectorSpec{}}
	for key, label := range map[string]string{
		"message.drawer": "测试联系人抽屉", "message.entry": "测试联系人入口", "message.all_tab": "测试全部标签",
		"message.all_tab_selected": "测试全部标签已选中", "message.contact_item": "全部联系人项目",
	} {
		cfg.Selectors[key] = testLiepinSelector(label)
	}
	return NewRuntime(), &liepinReplyBrowserStub{}, cfg
}

// TestAllContactInboxStartsWithoutBadgeAndStaysOnAll 验证入口没有数字时仍打开全部，常驻轮询没有多余点击。
func TestAllContactInboxStartsWithoutBadgeAndStaysOnAll(t *testing.T) {
	r, b, cfg := allContactFixture()
	b.items = []contract.FindAllItem{
		{Index: 0, Text: "甲 09:30", Fields: map[string]string{"name": "甲", "thread_meta": `{"to_imid":"a"}`}},
		{Index: 1, Text: "乙 09:31", Fields: map[string]string{"name": "乙", "unread_count": "2", "thread_meta": `{"to_imid":"b"}`}},
	}
	for range 2 {
		got, err := r.ScanUnreadConversations(context.Background(), b, cfg)
		if err != nil || len(got) != 2 || got[0].Key != "a" || got[1].Key != "b" {
			t.Fatalf("没有返回含无红点联系人的完整列表：%+v %v", got, err)
		}
	}
	if !slices.Equal(b.clicks, []string{"测试联系人入口", "测试全部标签"}) {
		t.Fatalf("轮询产生额外点击：%v", b.clicks)
	}
}

// TestAllContactInboxSignaturesIncludePreviewAndTime 验证无角标、同名及换位等情形有稳定的摘要与会话键。
func TestAllContactInboxSignaturesIncludePreviewAndTime(t *testing.T) {
	r, b, cfg := allContactFixture()
	b.drawerOpen, b.allSelected = true, true
	b.items = []contract.FindAllItem{{Text: "重名 09:30 好", Fields: map[string]string{"name": "重名", "last_message": "好", "thread_meta": `{"to_imid":"first"}`}},
		{Index: 1, Text: "重名 09:30 好", Fields: map[string]string{"name": "重名", "last_message": "好", "thread_meta": `{"to_imid":"second"}`}}}
	snapshot := func() map[string]string {
		t.Helper()
		got, err := r.ScanUnreadConversations(context.Background(), b, cfg)
		if err != nil || len(got) != len(b.items) {
			t.Fatalf("全部联系人读取失败：%v", err)
		}
		signatures := map[string]string{}
		for _, row := range got {
			signatures[row.Key] = row.Fields["contact_list_signature"]
			if len(signatures[row.Key]) != 64 {
				t.Fatalf("缺少行摘要：%+v", row)
			}
		}
		return signatures
	}
	before := snapshot()
	b.items[0], b.items[1] = b.items[1], b.items[0]
	after := snapshot()
	if before["first"] != after["first"] || before["second"] != after["second"] {
		t.Fatal("仅排序变化导致摘要不稳定")
	}
	for _, change := range []string{"time", "preview"} {
		switch change {
		case "time":
			b.items[0].Text = "重名 09:31 好"
		case "preview":
			b.items[0].Fields["last_message"] = "另一条"
		}
		next := snapshot()
		if next["second"] == after["second"] || next["first"] != after["first"] {
			t.Fatalf("未正确检测 %s", change)
		}
		after = next
	}
}

// TestAllContactPollKeepsOpenChatAndReadsDuplicateText 验证全部列表没有红点/摘要变化时，已打开的聊天仍能读取同分钟同文的新消息编号。
func TestAllContactPollKeepsOpenChatAndReadsDuplicateText(t *testing.T) {
	r, b, cfg := allContactFixture()
	b.drawerOpen, b.allSelected, b.conversationOpen = true, true, true
	for key, label := range map[string]string{"message.current_name": "测试当前候选人", "message.current_position": "测试当前岗位", "message.item": "测试聊天消息"} {
		cfg.Selectors[key] = testLiepinSelector(label)
	}
	mk := func(id string) contract.FindAllItem {
		return contract.FindAllItem{Text: "收到", Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "收到",
			"message_time": "09:31", "message_meta": `{"message_id":"` + id + `"}`,
		}}
	}
	b.items = []contract.FindAllItem{{Text: "邓云川 09:31 收到", Fields: map[string]string{"name": "邓云川", "last_message": "收到", "thread_meta": `{"to_imid":"thread"}`}}}
	b.historyBatches = [][]contract.FindAllItem{{mk("original")}}
	snapshot := model.AutoReplyConversationSnapshot{CandidateName: "邓云川", CommunicationPosition: "测试岗位"}
	var signature string
	for _, id := range []string{"original", "subsequent"} {
		if id == "subsequent" {
			b.historyBatches[0] = append(b.historyBatches[0], mk(id))
		}
		rows, err := r.ScanUnreadConversations(context.Background(), b, cfg)
		if err != nil || len(rows) != 1 || (signature != "" && signature != rows[0].Fields["contact_list_signature"]) {
			t.Fatalf("同分钟同文应保留同一份列表摘要：%+v %v", rows, err)
		}
		signature = rows[0].Fields["contact_list_signature"]
		latest, open, err := r.ReadOpenAutoReplyLatestMessage(context.Background(), b, cfg, snapshot)
		if err != nil || !open || latest.PlatformMessageID != id || len(b.clicks) != 0 {
			t.Fatalf("当前聊天没有识别最新消息：latest=%+v open=%t clicks=%v err=%v", latest, open, b.clicks, err)
		}
	}
}

// TestContactActivityUsesDedicatedDatedTime 验证只有独立的联系人时间文本能转为 UTC，不从消息正文猜时间。
func TestContactActivityUsesDedicatedDatedTime(t *testing.T) {
	now := time.Date(2026, 11, 8, 4, 0, 0, 0, time.UTC)
	for raw, want := range map[string]string{
		"昨天 23:10": "2026-11-07T15:10:00Z", "今天 01:25": "2026-11-07T17:25:00Z",
		"2026-11-06 12:45": "2026-11-06T04:45:00Z", "11月6日 12:45": "2026-11-06T04:45:00Z",
		"": "", "约在今天 01:25 面试": "", "11月6日": "", "01:25": "2026-11-07T17:25:00Z", "23:59": "", "昨天": "",
	} {
		if got := liepinContactActivityAt(raw, now); got != want {
			t.Fatalf("时间 %q 被解析为 %q，应为 %q", raw, got, want)
		}
	}
	items, err := liepinConversations([]contract.FindAllItem{{Text: "今天 01:25 面试", Fields: map[string]string{
		"name": "测试", "last_message": "今天 01:25 面试", "thread_meta": `{"to_imid":"a"}`,
	}}})
	if err != nil || len(items) != 1 || items[0].Fields["contact_activity_at"] != "" {
		t.Fatal("错误采用了候选人正文中的时间", items, err)
	}
}
