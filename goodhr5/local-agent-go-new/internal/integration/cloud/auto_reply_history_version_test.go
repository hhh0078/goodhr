// Package cloud 本文件验证旧消息键版本的完整回填、原子升级和账号岗位边界，不连接真实数据库。
package cloud

import (
	"context"
	"fmt"
	"testing"

	"goodhr5/local-agent-go-new/internal/storage"
)

// TestHistoryVersionBackfills87To89WithoutReply 验证旧87条补齐两条重复后只升级一次，不新增待答或改写发送结果。
func TestHistoryVersionBackfills87To89WithoutReply(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	v := entityTestConversation(t, c, cred)
	ctx := context.Background()
	full, old := []AutoReplyMessage{}, []AutoReplyMessage{}
	for i := 0; i < 89; i++ {
		m := AutoReplyMessage{Fingerprint: fmt.Sprintf("key-%d", i), Direction: "candidate", MessageType: "text", TextContent: fmt.Sprintf("历史消息%d", i)}
		if i == 56 || i == 84 {
			m.TextContent = fmt.Sprintf("历史消息%d", i-1)
		}
		full = append(full, m)
		if i != 56 && i != 84 {
			old = append(old, m)
		}
	}
	if _, err := c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: v.ID, Messages: old, HistoryComplete: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.CompleteAutoReplyMessage(ctx, cred, v.ID, "key-88", "completed"); err != nil {
		t.Fatal(err)
	}
	for sequence, status := range []string{"sent", "unknown"} {
		if _, claimed, err := c.ClaimAutoReplySend(ctx, cred, v.ID, "key-88", "既有回答", sequence); err != nil || !claimed {
			t.Fatal(err)
		}
		if err := c.FinishAutoReplySend(ctx, cred, v.ID, "key-88", sequence, status); err != nil {
			t.Fatal(err)
		}
	}
	scope := AutoReplyHistoryRefreshScope{PlatformID: "boss", PlatformAccountID: "account-one", Version: 1, PositionIDs: []string{"position"}}
	if err := c.QueueAutoReplyHistoryRefresh(ctx, cred, scope); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: v.ID, Messages: full, HistoryComplete: true, CaptureCompleted: true, MessageIdentityVersion: 1})
		if err != nil || result.Inserted != 2*(1-attempt) || result.LastSyncedMessageKey != "key-88" || result.LastCandidateMessageKey != "key-88" {
			t.Fatalf("回填改变游标或不幂等：%+v %v", result, err)
		}
	}
	if err := c.QueueAutoReplyHistoryRefresh(ctx, cred, scope); err != nil {
		t.Fatal(err)
	}
	rows, err := c.AutoReplyMessages(ctx, cred, v.ID)
	if err != nil || len(rows) != 89 {
		t.Fatalf("87条没有补齐89条：%d %v", len(rows), err)
	}
	pending, err := c.PendingAutoReplyMessages(ctx, cred, v.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("旧消息进入待答：%+v %v", pending, err)
	}
	conversations, err := c.ListPendingAutoReplyConversations(ctx, cred, "boss", "account-one")
	if err != nil || len(conversations) != 0 {
		t.Fatalf("已升级会话再次重采：%+v %v", conversations, err)
	}
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		item, _, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", v.ID)
		if err != nil || item.MessageIdentityVersion != 1 || item.CaptureIntent != nil {
			t.Fatalf("版本没有原子提交：%+v %v", item, err)
		}
		for _, key := range []string{"key-56", "key-84"} {
			row, found, err := tx.MessageByKey(v.ID, key)
			if err != nil || !found {
				t.Fatalf("缺回填消息：%s %v", key, err)
			}
			_ = row
		}
		sends, err := listAutoReplyEntities[localAutoReplySend](tx, "sends", autoReplyFilter("conversation_id", v.ID))
		if err != nil || len(sends) != 2 || sends[0].Status != "sent" || sends[1].Status != "unknown" {
			t.Fatalf("发送凭据被改写：%+v %v", sends, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestHistoryVersionFailureDoesNotUpgrade 验证截尾、空结果和无旧锚点都保留原版本与待办，不落入假成功。
func TestHistoryVersionFailureDoesNotUpgrade(t *testing.T) {
	for _, scenario := range []string{"truncated", "empty", "no-anchor"} {
		t.Run(scenario, func(t *testing.T) {
			c, cred, _ := entityTestClient(t)
			v := entityTestConversation(t, c, cred)
			ctx := context.Background()
			syncEntityMessages(t, c, cred, v, AutoReplyMessage{Fingerprint: "old", Direction: "self", TextContent: "已回答"})
			if err := c.QueueAutoReplyHistoryRefresh(ctx, cred, AutoReplyHistoryRefreshScope{PlatformID: "boss", PlatformAccountID: "account-one", Version: 1, PositionIDs: []string{"position"}}); err != nil {
				t.Fatal(err)
			}
			request := AutoReplyMessageSyncRequest{ConversationID: v.ID, CaptureCompleted: true, MessageIdentityVersion: 1, HistoryComplete: true, Messages: []AutoReplyMessage{{Fingerprint: "old", Direction: "self", TextContent: "已回答"}}}
			switch scenario {
			case "truncated":
				request.HistoryComplete = false
			case "empty":
				request.Messages = nil
			case "no-anchor":
				request.Messages[0].Fingerprint = "unrelated"
			}
			if _, err := c.SyncAutoReplyMessages(ctx, cred, request); err == nil {
				t.Fatal("失败迁移被放行")
			}
			state, err := c.AutoReplyCandidateState(ctx, cred, AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "account-one", PlatformThreadID: "thread"})
			if err != nil || state.Conversation.MessageIdentityVersion != 0 || state.Conversation.CaptureIntent == nil {
				t.Fatalf("失败后版本或待办丢失：%+v %v", state.Conversation, err)
			}
			messages, err := c.AutoReplyMessages(ctx, cred, v.ID)
			if err != nil || len(messages) != 1 {
				t.Fatalf("失败事务写入消息：%d %v", len(messages), err)
			}
		})
	}
}

// TestHistoryVersionQueuesOnlyAllowedScope 验证未开启岗位、其他平台账号、其他候选人与其他登录用户不被登记。
func TestHistoryVersionQueuesOnlyAllowedScope(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	other := cred
	other.Token = "other-owner"
	for _, row := range []struct {
		owner                                     AgentCredentials
		platform, account, position, thread, name string
	}{
		{cred, "liepin", "account", "enabled", "target", "目标"}, {cred, "liepin", "account", "disabled", "disabled-thread", "目标"},
		{cred, "liepin", "account", "enabled", "other-thread", "目标"}, {cred, "liepin", "other-account", "enabled", "target", "目标"},
		{cred, "boss", "account", "enabled", "target", "目标"}, {other, "liepin", "account", "enabled", "target", "目标"},
	} {
		v, err := c.SaveAutoReplyConversation(ctx, row.owner, AutoReplyConversation{PlatformID: row.platform, PlatformAccountID: row.account, PositionID: row.position, PlatformThreadID: row.thread, CandidateName: row.name})
		if err != nil {
			t.Fatal(err)
		}
		syncEntityMessages(t, c, row.owner, v, AutoReplyMessage{Fingerprint: "old-self", Direction: "self", TextContent: "已回答"})
	}
	scope := AutoReplyHistoryRefreshScope{PlatformID: "liepin", PlatformAccountID: "account", Version: 1, PositionIDs: []string{"enabled"}, Target: &AutoReplyCaptureIntent{PlatformThreadID: "target", PlatformCandidateID: "candidate", CandidateName: "目标"}}
	if err := c.QueueAutoReplyHistoryRefresh(ctx, cred, scope); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		owner             AgentCredentials
		platform, account string
		want              int
	}{{cred, "liepin", "account", 1}, {cred, "liepin", "other-account", 0}, {cred, "boss", "account", 0}, {other, "liepin", "account", 0}} {
		pending, err := c.ListPendingAutoReplyConversations(ctx, row.owner, row.platform, row.account)
		if err != nil || len(pending) != row.want {
			t.Fatalf("重采越界：%+v %v", pending, err)
		}
		if len(pending) > 0 && (pending[0].PlatformThreadID != "target" || pending[0].CaptureIntent.PlatformCandidateID != "candidate") {
			t.Fatalf("目标限制丢失：%+v", pending)
		}
	}
}
