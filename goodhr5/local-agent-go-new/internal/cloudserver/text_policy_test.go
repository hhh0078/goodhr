// Package cloudserver 本文件验证文字队列领取前读取最新配置，空话术与旧稿不能进入设备，已执行的未知或成功回执继续防重。
package cloudserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/replybridge"
	"goodhr5/local-agent-go-new/internal/storage"
)

// queuedTextFixture 在隔离内存权威库排好一条文字命令，并提供可修改的真实岗位快照接口。
type queuedTextFixture struct {
	run          *activeRun
	state        *stateEndpointFixture
	conversation cloud.AutoReplyConversation
	command      replybridge.Command
	intentID     string
	mu           sync.Mutex
	snapshot     cloud.AutoReplyPositionSnapshot
	snapshotGets int
}

// newQueuedTextFixture 复用服务器PG适配器合成队列，所有请求仅访问httptest，不连接真实环境。
func newQueuedTextFixture(t *testing.T, phraseID string, legacy bool) *queuedTextFixture {
	t.Helper()
	f := &queuedTextFixture{state: &stateEndpointFixture{data: map[string]storage.AutoReplyStateChange{}}, snapshot: cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "p", PlatformID: "liepin"}, Config: cloud.PositionAutoReplyConfig{PositionID: "p", Enabled: true, Version: 1, PhrasesVersion: "library", Phrases: []cloud.AutoReplyPhrase{{ID: "receive_resume", Content: "简历收到了。"}}}, CompanyProfile: cloud.CompanyProfile{ID: "co"}, Subscription: cloud.AutoReplySubscription{Active: true, AllowAutoReply: true}}}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/auto-reply/agent/positions/p/snapshot" {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.snapshotGets++
			output(w, http.StatusOK, f.snapshot)
			return
		}
		f.state.handle(w, req)
	}))
	t.Cleanup(backend.Close)
	db, err := storage.OpenAutoReplyScratch(newStateAuthority(backend.URL, "1234567890abcdef", "tenant", "user"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := cloud.New(backend.URL)
	cred := cloud.AgentCredentials{Token: "user", MachineID: "machine"}
	ctx := context.Background()
	if _, err = c.ValidateSession(ctx, cred.Token); err != nil {
		t.Fatal(err)
	}
	c.EnableLocalAutoReply(db, filepath.Join(t.TempDir(), "private"))
	f.run = &activeRun{id: "run", scope: runScope{machine: "machine"}, client: c, cred: cred, leaseKey: "lease:one", platform: "liepin", position: "p"}
	if err = f.run.updateLease(ctx, "claim"); err != nil {
		t.Fatal(err)
	}
	f.conversation, err = c.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PlatformID: "liepin", PlatformAccountID: "account", PlatformThreadID: "thread", PositionID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	f.run.conversation = f.conversation.ID
	if !legacy {
		ctx = cloud.WithAutoReplyTextPolicy(ctx, f.snapshot, phraseID)
	}
	if _, claimed, err := c.ClaimAutoReplySend(ctx, cred, f.conversation.ID, "m1", "简历收到了。", 1); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	f.intentID = cloud.AutoReplySendIntentID(f.conversation.ID, "m1", 1)
	ctx = cloud.WithAutoReplySendIntent(ctx, f.intentID)
	payload, _ := json.Marshal(replybridge.Request{Message: "简历收到了。"})
	f.command = replybridge.Command{ID: "text-command", RunID: f.run.id, PlatformID: f.run.platform, Action: "send", Deadline: time.Now().Add(time.Minute), Payload: payload}
	if err = f.run.persistCommand(ctx, f.command, "pending", replybridge.Result{}); err != nil {
		t.Fatal(err)
	}
	return f
}

// commandState 只读合成权威库中的最终命令，保证未执行取消不会被误当未知重试。
func (f *queuedTextFixture) commandState(t *testing.T) commandState {
	t.Helper()
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	var bound commandState
	if err := json.Unmarshal(f.state.data["runtime_commands|"+f.command.ID].Payload, &bound); err != nil {
		t.Fatal(err)
	}
	return bound
}

// TestQueuedTextRechecksConfigBeforeDeviceClaim 验证排队以后清空、删除、改岗位正文或禁用会使旧稿在设备领取前取消。
func TestQueuedTextRechecksConfigBeforeDeviceClaim(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*cloud.AutoReplyPositionSnapshot)
	}{
		{"清空话术", func(s *cloud.AutoReplyPositionSnapshot) { s.Config.Phrases[0].Content = " \n " }},
		{"删除话术", func(s *cloud.AutoReplyPositionSnapshot) { s.Config.Phrases[0].Deleted = true }},
		{"删去整条话术", func(s *cloud.AutoReplyPositionSnapshot) { s.Config.Phrases = []cloud.AutoReplyPhrase{} }},
		{"岗位正文已改", func(s *cloud.AutoReplyPositionSnapshot) { s.Config.PositionDescription = "最新岗位信息" }},
		{"岗位停止", func(s *cloud.AutoReplyPositionSnapshot) { s.Config.Enabled = false }},
		{"会员失效", func(s *cloud.AutoReplyPositionSnapshot) { s.Subscription.AllowAutoReply = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQueuedTextFixture(t, "receive_resume", false)
			f.mu.Lock()
			tc.change(&f.snapshot)
			f.mu.Unlock()
			err := f.run.persistCommand(context.Background(), f.command, "executing", replybridge.Result{})
			var rejected *replybridge.CommandRejectedError
			if !errors.As(err, &rejected) {
				t.Fatal("变更后的旧稿仍可领取", err)
			}
			bound := f.commandState(t)
			if bound.Status != "cancelled" || !bound.StartedAt.IsZero() || bound.ResultCode != "REPLY_SUPERSEDED" || f.snapshotGets != 1 {
				t.Fatal("未在真实配置复核后取消尚未执行的旧稿", bound, f.snapshotGets)
			}
			// 尚未领取的命令允许原Flow按明确未发送结果释放；这里不替调用方删除意图。
			if status, _, err := f.run.client.AutoReplySendReceipt(context.Background(), f.run.cred, f.conversation.ID, "m1", 1); err != nil || status != "sending" {
				t.Fatal("队列取消擅自改写了发送账本", status, err)
			}
		})
	}
}

// TestQueuedTextLegacyAndUnchangedCompatibility 验证旧无版本排队稿保守取消，当前有版本的普通答案不依赖任何固定话术。
func TestQueuedTextLegacyAndUnchangedCompatibility(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		f := newQueuedTextFixture(t, "", legacy)
		err := f.run.persistCommand(context.Background(), f.command, "executing", replybridge.Result{})
		var rejected *replybridge.CommandRejectedError
		if legacy {
			if !errors.As(err, &rejected) || f.commandState(t).Status != "cancelled" {
				t.Fatal("旧无配置版本的排队文字仍被派发", err)
			}
		} else if err != nil || f.commandState(t).Status != "executing" {
			t.Fatal("未修改的普通答复被固定话术限制", err)
		}
	}
}

// TestStartedTextPolicyChangeKeepsUnknownAndSent 验证一旦设备已领取，后续清空话术不取消未知/成功回执，也不能再次领取或释放发送意图。
func TestStartedTextPolicyChangeKeepsUnknownAndSent(t *testing.T) {
	for _, status := range []string{"unknown", "sent"} {
		t.Run(status, func(t *testing.T) {
			f := newQueuedTextFixture(t, "receive_resume", false)
			ctx := context.Background()
			if err := f.run.persistCommand(ctx, f.command, "executing", replybridge.Result{}); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.snapshot.Config.Phrases[0].Content = ""
			f.mu.Unlock()
			commandStatus := "unknown"
			result := replybridge.Result{Code: "RESULT_UNKNOWN"}
			if status == "sent" {
				commandStatus = "completed"
				result.Code = ""
				result.Value, _ = json.Marshal(replybridge.Response{Latest: model.ConversationMessage{Direction: "self", Key: "real-outbound", TextContent: "简历收到了。"}})
			}
			if err := f.run.persistCommand(ctx, f.command, commandStatus, result); err != nil {
				t.Fatal("配置变化阻止了已执行动作保存原回执", err)
			}
			if status == "unknown" {
				if err := f.run.client.FinishAutoReplySend(ctx, f.run.cred, f.conversation.ID, "m1", 1, "unknown"); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.run.persistCommand(ctx, f.command, "executing", replybridge.Result{}); err == nil {
				t.Fatal("已执行动作被再次领取")
			}
			if f.commandState(t).Status != commandStatus || f.snapshotGets != 1 {
				t.Fatal("终态被新的配置复核改写或重复拉取配置")
			}
			if err := f.run.client.ReleaseUnsentAutoReply(ctx, f.run.cred, f.conversation.ID, "m1", 1, "简历收到了。"); err == nil {
				t.Fatal("未知/已发送的意图被取消释放")
			}
			got, claimed, err := f.run.client.ClaimAutoReplySend(cloud.WithAutoReplyTextPolicy(ctx, f.snapshot, "receive_resume"), f.run.cred, f.conversation.ID, "m1", "简历收到了。", 1)
			if err != nil || claimed || got != status {
				t.Fatal("配置变更让终态自动重发", got, claimed, err)
			}
		})
	}
}

// TestNativeInfoIgnoresTextPhrasePolicy 验证空文字话术不阻止有独立发送意图的原生交换按钮动作。
func TestNativeInfoIgnoresTextPhrasePolicy(t *testing.T) {
	f := newQueuedTextFixture(t, "", true)
	f.snapshot.Config.Phrases[0].Content = ""
	ctx := context.Background()
	key, content := "info-request:wechat", "平台资料索要：wechat"
	if _, claimed, err := f.run.client.ClaimAutoReplySend(ctx, f.run.cred, f.conversation.ID, key, content, 0); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	payload, _ := json.Marshal(replybridge.Request{InfoRequest: model.CandidateInfoRequest{RequestWechat: true}})
	cmd := replybridge.Command{ID: "native-info", RunID: f.run.id, PlatformID: f.run.platform, Action: "request_info", Deadline: time.Now().Add(time.Minute), Payload: payload}
	ctx = cloud.WithAutoReplySendIntent(ctx, cloud.AutoReplySendIntentID(f.conversation.ID, key, 0))
	for _, status := range []string{"pending", "executing"} {
		if err := f.run.persistCommand(ctx, cmd, status, replybridge.Result{}); err != nil {
			t.Fatal("文字空话术阻止原生动作", status, err)
		}
	}
	if f.snapshotGets != 0 {
		t.Fatal("原生资料动作被错误接入文字快照限制")
	}
}
