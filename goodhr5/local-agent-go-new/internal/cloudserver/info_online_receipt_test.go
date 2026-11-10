// Package cloudserver 本文件验证原生资料动作必须有服务器发送意图，以及在线读取只保存无图片来源证明。
package cloudserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/replybridge"
)

// TestNativeInfoCommandRequiresExactSendIntent 防止设备资料动作绕过公共账本；已撤销及未知命令不能重新执行。
func TestNativeInfoCommandRequiresExactSendIntent(t *testing.T) {
	r, fixture, conv, _, _, _ := serverGoalFixture(t)
	payload, _ := json.Marshal(replybridge.Request{InfoRequest: model.CandidateInfoRequest{RequestPhone: true}})
	command := replybridge.Command{ID: "info-command", RunID: r.id, PlatformID: r.platform, Action: "request_info", Deadline: time.Now().Add(time.Minute), Payload: payload}
	if err := r.persistCommand(context.Background(), command, "pending", replybridge.Result{}); err == nil {
		t.Fatal("没有意图仍接受原生资料动作")
	}
	key, content := "info-request:phone", "平台资料索要：phone"
	if _, claimed, err := r.client.ClaimAutoReplySend(context.Background(), r.cred, conv.ID, key, content, 0); err != nil || !claimed {
		t.Fatal("申请动作没有进入服务器账本", claimed, err)
	}
	ctx := cloud.WithAutoReplySendIntent(context.Background(), cloud.AutoReplySendIntentID(conv.ID, key, 0))
	wrong := command
	wrong.ID = "wrong-info-command"
	wrong.Payload, _ = json.Marshal(replybridge.Request{InfoRequest: model.CandidateInfoRequest{RequestWechat: true}})
	if err := r.persistCommand(ctx, wrong, "pending", replybridge.Result{}); err == nil {
		t.Fatal("手机号意图被用于微信动作")
	}
	if err := r.persistCommand(ctx, command, "pending", replybridge.Result{}); err != nil {
		t.Fatal(err)
	}
	if err := r.persistCommand(ctx, command, "executing", replybridge.Result{}); err != nil {
		t.Fatal(err)
	}
	if err := r.persistCommand(ctx, command, "unknown", replybridge.Result{Code: "RESULT_UNKNOWN"}); err != nil {
		t.Fatal(err)
	}
	if err := r.persistCommand(ctx, command, "executing", replybridge.Result{}); err == nil {
		t.Fatal("未知动作被再次执行")
	}
	var bound commandState
	if err := json.Unmarshal(fixture.data["runtime_commands|info-command"].Payload, &bound); err != nil || bound.Status != "unknown" || bound.SendIntentID == "" {
		t.Fatal("未知动作证明丢失", err)
	}
}

// TestOnlineProfileCommandStoresSourceProofOnly 验证在线资料回执拒绝图片并保存真实来源摘要，供服务器删除后新资料核验。
func TestOnlineProfileCommandStoresSourceProofOnly(t *testing.T) {
	r, fixture, _, _, _, _ := serverGoalFixture(t)
	payload, _ := json.Marshal(replybridge.Request{Snapshot: model.AutoReplyConversationSnapshot{PlatformThreadID: "thread", PlatformAccountID: "account"}})
	command := replybridge.Command{ID: "online-command", RunID: r.id, PlatformID: r.platform, Action: "read_online_profile", Deadline: time.Now().Add(time.Minute), Payload: payload}
	for _, status := range []string{"pending", "executing"} {
		if err := r.persistCommand(context.Background(), command, status, replybridge.Result{}); err != nil {
			t.Fatal(err)
		}
	}
	value, _ := json.Marshal(replybridge.Response{OnlineProfile: model.AutoReplyOnlineProfile{Mode: "ai", SourceKey: "online-image:current-bound-digest", Screenshots: [][]byte{[]byte("not-persistable")}}})
	if err := r.persistCommand(context.Background(), command, "completed", replybridge.Result{Value: value}); err == nil {
		t.Fatal("原图进入了持久化回执")
	}
	value, _ = json.Marshal(replybridge.Response{OnlineProfile: model.AutoReplyOnlineProfile{Mode: "ai", SourceKey: "online-image:current-bound-digest"}})
	if err := r.persistCommand(context.Background(), command, "completed", replybridge.Result{Value: value}); err != nil {
		t.Fatal(err)
	}
	var bound commandState
	if err := json.Unmarshal(fixture.data["runtime_commands|online-command"].Payload, &bound); err != nil || bound.Status != "completed" || bound.SourceKey != "online-image:current-bound-digest" {
		t.Fatal("实际来源摘要未保存", err)
	}
}
