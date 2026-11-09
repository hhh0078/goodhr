// Package cloudserver 本文件验证目标派发前失效保护与真实发送回执、提问次数的同事务提交。
package cloudserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/replybridge"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestWaitingHRNoticeDoesNotClaimDeviceStopped 验证正常待确认提醒排队不会冒充设备故障，真实技术阶段保留原提示。
func TestWaitingHRNoticeDoesNotClaimDeviceStopped(t *testing.T) {
	message := safeRunFailure("awaiting_hr_notice")
	if !strings.Contains(message, "待确认") || strings.Contains(message, "设备") || strings.Contains(message, "停下") || strings.Contains(message, "重新启动") {
		t.Fatal("待确认排队被显示成设备停机", message)
	}
	if !strings.Contains(safeRunFailure("notify_manual_handoff"), "设备") {
		t.Fatal("真正技术问题的排查入口被改动")
	}
}

// serverGoalFixture 复用真实服务器状态适配器建立一个已领取发送意图的合成沟通目标。
func serverGoalFixture(t *testing.T) (*activeRun, *stateEndpointFixture, cloud.AutoReplyConversation, cloud.CandidateConfirmationItem, replybridge.Command, context.Context) {
	t.Helper()
	ctx := context.Background()
	fixture := &stateEndpointFixture{data: map[string]storage.AutoReplyStateChange{}}
	backend := httptest.NewServer(http.HandlerFunc(fixture.handle))
	t.Cleanup(backend.Close)
	db, err := storage.OpenAutoReplyScratch(newStateAuthority(backend.URL, "1234567890abcdef", "tenant", "user"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := cloud.New(backend.URL)
	cred := cloud.AgentCredentials{Token: "user", MachineID: "machine"}
	if _, err = c.ValidateSession(ctx, cred.Token); err != nil {
		t.Fatal(err)
	}
	c.EnableLocalAutoReply(db, filepath.Join(t.TempDir(), "private"))
	r := &activeRun{id: "run", scope: runScope{machine: "machine"}, client: c, cred: cred, leaseKey: "lease:one", platform: "liepin", position: "p"}
	if err = r.updateLease(ctx, "claim"); err != nil {
		t.Fatal(err)
	}
	conv, err := c.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PlatformID: "liepin", PlatformAccountID: "account", PlatformThreadID: "thread", PositionID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.EnsureLocalAutoReplyCandidate(ctx, cred, conv); err != nil {
		t.Fatal(err)
	}
	if _, err = c.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: []cloud.AutoReplyMessage{{Direction: "candidate", PlatformMessageID: "m1", TextContent: "我有兴趣"}}}); err != nil {
		t.Fatal(err)
	}
	conv, _, err = c.AutoReplyAnswerConversation(ctx, cred, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	goal, err := c.SaveAutoReplyConfirmationItem(ctx, cred, cloud.CandidateConfirmationItem{ID: "goal", ConversationID: conv.ID, CandidateID: conv.CandidateID, PositionID: "p", SourceType: "ai", CreatedByKind: "ai", ItemType: "confirm", Content: "方便通话时间", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	bind := &cloud.AutoReplyGoalSend{ConfirmationItemID: goal.ID, ExpectedVersion: cloud.AutoReplyConfirmationRevision(goal), BasedOnMessageKey: "m1", ExpectedLatestKey: "m1"}
	if _, claimed, err := c.ClaimAutoReplySend(ctx, cred, conv.ID, "m1", "方便什么时间通话？", 2, bind); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	r.conversation = conv.ID
	payload, _ := json.Marshal(replybridge.Request{Message: "方便什么时间通话？"})
	cmd := replybridge.Command{ID: "command", RunID: r.id, PlatformID: r.platform, Action: "send", Deadline: time.Now().Add(time.Minute), Payload: payload}
	ctx = cloud.WithAutoReplySendIntent(ctx, cloud.AutoReplySendIntentID(conv.ID, "m1", 2))
	if err = r.persistCommand(ctx, cmd, "pending", replybridge.Result{}); err != nil {
		t.Fatal(err)
	}
	return r, fixture, conv, goal, cmd, ctx
}

// TestQueuedGoalEditAndArchiveCancelBeforeDeviceClaim 验证目标修改或删除后取消尚未派给设备的命令，不让旧问题继续发送。
func TestQueuedGoalEditAndArchiveCancelBeforeDeviceClaim(t *testing.T) {
	for _, archive := range []bool{false, true} {
		r, fixture, conv, goal, cmd, ctx := serverGoalFixture(t)
		if archive {
			now := time.Now().UTC()
			goal.ArchivedAt = &now
		} else {
			goal.Content = "新的目标问题"
		}
		if _, err := r.client.SaveAutoReplyConfirmationItem(ctx, r.cred, goal); err != nil {
			t.Fatal(err)
		}
		err := r.persistCommand(context.Background(), cmd, "executing", replybridge.Result{})
		var rejected *replybridge.CommandRejectedError
		if !errors.As(err, &rejected) {
			t.Fatal("目标变化仍允许设备领取", err)
		}
		var bound commandState
		if err = json.Unmarshal(fixture.data["runtime_commands|command"].Payload, &bound); err != nil || bound.Status != "cancelled" || bound.ConversationID != conv.ID {
			t.Fatal("取消状态没有写入服务器", bound, err)
		}
	}
}

// TestGoalReceiptCommitsCounterAndSendTogether 验证服务器确认真实气泡的同一事务写命令、发送状态和提问计数，失败时三者回滚。
func TestGoalReceiptCommitsCounterAndSendTogether(t *testing.T) {
	r, fixture, conv, _, cmd, ctx := serverGoalFixture(t)
	if err := r.persistCommand(ctx, cmd, "executing", replybridge.Result{}); err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal(replybridge.Response{Latest: model.ConversationMessage{Direction: "self", PlatformMessageID: "real-out", TextContent: "方便什么时间通话？"}})
	result := replybridge.Result{ID: cmd.ID, Value: value}
	fixture.beforeCommit = func(changes []storage.AutoReplyStateChange) error {
		for _, row := range changes {
			if row.Kind == "runtime_commands" {
				var b commandState
				_ = json.Unmarshal(row.Payload, &b)
				if b.Status == "completed" {
					return errors.New("合成数据库拒绝")
				}
			}
		}
		return nil
	}
	if err := r.persistCommand(ctx, cmd, "completed", result); err == nil {
		t.Fatal("拒绝提交后仍确认设备回执")
	}
	items, err := r.client.AutoReplyConfirmationItems(ctx, r.cred, conv.ID)
	if err != nil || items[0].AskCount != 0 {
		t.Fatal("提问计数没有跟随回执回滚", items, err)
	}
	status, _, err := r.client.AutoReplySendReceipt(ctx, r.cred, conv.ID, "m1", 2)
	if err != nil || status != "sending" {
		t.Fatal("失败事务提前保存了已发送", status, err)
	}
	fixture.beforeCommit = nil
	if err = r.persistCommand(ctx, cmd, "completed", result); err != nil {
		t.Fatal(err)
	}
	if err = r.client.FinishAutoReplySend(ctx, r.cred, conv.ID, "m1", 2, "sent", "real-out"); err != nil {
		t.Fatal(err)
	}
	items, err = r.client.AutoReplyConfirmationItems(ctx, r.cred, conv.ID)
	if err != nil || items[0].AskCount != 1 {
		t.Fatal("同一回执重复累计了次数", items, err)
	}
	status, outbound, err := r.client.AutoReplySendReceipt(ctx, r.cred, conv.ID, "m1", 2)
	if err != nil || status != "sent" || outbound != "real-out" {
		t.Fatal("实际发送回执未入账", status, outbound, err)
	}
}
