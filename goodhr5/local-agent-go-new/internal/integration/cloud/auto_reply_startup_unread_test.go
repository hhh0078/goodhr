// Package cloud 本文件验证启动前未读通过真实消息同步、人工回复边界和发送账本处理，重启后不会重复回复。
package cloud

import (
	"context"
	"testing"
	"time"
)

// TestStartupUnreadKeepsMessageAndSendDedup 验证旧未读可排队、人工已经答复的旧问题不排队，重启仍保留相同消息的发送结果。
func TestStartupUnreadKeepsMessageAndSendDedup(t *testing.T) {
	ctx := context.Background()
	c, cred, store := entityTestClient(t)
	conversation := entityTestConversation(t, c, cred)
	now := time.Now().UTC()
	old := now.Add(-time.Hour)
	rows := []AutoReplyListObservation{{ThreadID: "thread", Signature: "visible-unread", Unread: true}}
	reviews, err := c.ObserveAutoReplyList(ctx, cred, "boss", "account-one", "", rows, now)
	if err != nil || len(reviews) != 1 || !reviews[0].After.IsZero() {
		t.Fatal("启动前未读没有进入核对队列", reviews, err)
	}
	input := AutoReplyMessageSyncRequest{ConversationID: conversation.ID, ReplyAfter: reviews[0].After, CaptureCompleted: true, HistoryComplete: true, Messages: []AutoReplyMessage{
		{PlatformMessageID: "old-question", Direction: "candidate", TextContent: "旧问题", PlatformSentAt: &old},
		{PlatformMessageID: "manual-reply", Direction: "self", TextContent: "人工已答复", PlatformSentAt: &old},
		{PlatformMessageID: "unread-before-start", Direction: "candidate", TextContent: "这个是地推吗？", PlatformSentAt: &old},
	}}
	if _, err = c.SyncAutoReplyMessages(ctx, cred, input); err != nil {
		t.Fatal(err)
	}
	pending, err := c.PendingAutoReplyMessages(ctx, cred, conversation.ID)
	if err != nil || len(pending) != 1 || pending[0].PlatformMessageID != "unread-before-start" {
		t.Fatal("历史已答复消息混进未读队列", pending, err)
	}
	if _, fresh, err := c.ClaimAutoReplySend(ctx, cred, conversation.ID, "unread-before-start", "测试回复", 1); err != nil || !fresh {
		t.Fatal("未读首次发送登记失败", fresh, err)
	}
	if err = c.FinishAutoReplySend(ctx, cred, conversation.ID, "unread-before-start", 1, "sent"); err != nil {
		t.Fatal(err)
	}
	c = New("")
	c.EnableLocalAutoReply(store, t.TempDir())
	if result, err := c.SyncAutoReplyMessages(ctx, cred, input); err != nil || result.Inserted != 0 {
		t.Fatal("重启后同一消息重复入库", result, err)
	}
	if status, fresh, err := c.ClaimAutoReplySend(ctx, cred, conversation.ID, "unread-before-start", "测试回复", 1); err != nil || fresh || status != "sent" {
		t.Fatal("重启后已发消息再次获准发送", status, fresh, err)
	}
	if _, fresh, err := c.ClaimAutoReplySend(ctx, cred, conversation.ID, "unread-before-start", "换一种说法也不能重发", 1); err == nil || fresh {
		t.Fatal("改写正文绕过了发送账本", fresh, err)
	}
}
