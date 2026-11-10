// Package cloud 本文件验证采集意图的账号隔离、资料保护、同步提交和失败回滚。
package cloud

import (
	"context"
	"testing"
)

// TestAutoReplyCaptureSurvivesSaveAndFailedSync 验证页面资料保存不清意图，失败同步回滚，成功同步原子转为消息待办。
func TestAutoReplyCaptureSurvivesSaveAndFailedSync(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	intent := AutoReplyCaptureIntent{PositionID: "start-position", PlatformThreadID: "thread", CandidateName: "列表候选人"}
	if err := c.BeginAutoReplyCapture(ctx, cred, "boss", "account-one", "thread", intent); err != nil {
		t.Fatal(err)
	}
	state, err := c.AutoReplyCandidateState(ctx, cred, AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "account-one", PlatformThreadID: "thread"})
	if err != nil || state.Conversation == nil || state.Conversation.CaptureIntent == nil || state.Candidate != nil || state.Identity != nil || state.Conversation.PositionID != "" || state.Conversation.CandidateName != "" {
		t.Fatalf("采集线索不能成为已核实的候选人或岗位资料：%+v %v", state, err)
	}
	for _, lookup := range []struct{ platform, account string }{{"boss", "account-two"}, {"liepin", "account-one"}} {
		items, err := c.ListPendingAutoReplyConversations(ctx, cred, lookup.platform, lookup.account)
		if err != nil || len(items) != 0 {
			t.Fatalf("采集意图串平台或招聘账号：%+v %v", items, err)
		}
	}
	other := cred
	other.Token = "another-owner-token"
	items, err := c.ListPendingAutoReplyConversations(ctx, other, "boss", "account-one")
	if err != nil || len(items) != 0 {
		t.Fatalf("采集意图串GoodHR账号：%+v %v", items, err)
	}
	saved, err := c.SaveAutoReplyConversation(ctx, cred, AutoReplyConversation{PlatformID: "boss", PlatformAccountID: "account-one", PlatformThreadID: "thread", PositionID: "verified-position", CandidateName: "核实后的候选人", Status: "active"})
	if err != nil || saved.CaptureIntent == nil {
		t.Fatalf("保存页面资料提前清除了采集意图：%+v %v", saved, err)
	}
	if err = c.BeginAutoReplyCapture(ctx, cred, "boss", "account-one", "thread", AutoReplyCaptureIntent{CandidateName: "另一条列表名称"}); err != nil {
		t.Fatal(err)
	}
	state, err = c.AutoReplyCandidateState(ctx, cred, AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "account-one", PlatformThreadID: "thread"})
	if err != nil || state.Conversation.CandidateName != "核实后的候选人" || state.Conversation.PositionID != "verified-position" || state.Conversation.CaptureIntent.CandidateName != intent.CandidateName {
		t.Fatalf("重复登记覆盖了已核实资料或原始采集意图：%+v %v", state, err)
	}
	if _, err = c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: saved.ID, CaptureCompleted: true, Messages: []AutoReplyMessage{{Direction: "candidate"}}}); err == nil {
		t.Fatal("缺稳定编号的消息应回滚")
	}
	items, err = c.ListPendingAutoReplyConversations(ctx, cred, "boss", "account-one")
	if err != nil || len(items) != 1 || items[0].CaptureIntent == nil {
		t.Fatalf("失败同步丢失恢复意图：%+v %v", items, err)
	}
	if _, err = c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: saved.ID, CaptureCompleted: true, Messages: []AutoReplyMessage{{PlatformMessageID: "question", Direction: "candidate", TextContent: "工作地点？"}}}); err != nil {
		t.Fatal(err)
	}
	items, err = c.ListPendingAutoReplyConversations(ctx, cred, "boss", "account-one")
	if err != nil || len(items) != 1 || items[0].CaptureIntent != nil {
		t.Fatalf("成功采集应清意图但保留未答消息：%+v %v", items, err)
	}
	if err = c.CompleteAutoReplyMessage(ctx, cred, saved.ID, "question", "completed"); err != nil {
		t.Fatal(err)
	}
	items, err = c.ListPendingAutoReplyConversations(ctx, cred, "boss", "account-one")
	if err != nil || len(items) != 0 {
		t.Fatalf("消息完成后不应重复恢复：%+v %v", items, err)
	}
}
