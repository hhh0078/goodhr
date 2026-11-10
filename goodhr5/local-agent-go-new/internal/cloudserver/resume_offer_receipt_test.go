// Package cloudserver 本文件验证接收原生简历邀请沿用既有资料命令但绑定独立来源账本，失效来源不能派给设备。
package cloudserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/replybridge"
)

// TestResumeOfferCommandRequiresOfferBoundIntent 验证普通索要意图不能冒充同意，来源在排队后失效会在派发前取消。
func TestResumeOfferCommandRequiresOfferBoundIntent(t *testing.T) {
	r, fixture, conv, _, _, _ := serverGoalFixture(t)
	ctx := context.Background()
	at := time.Now().UTC()
	if _, err := r.client.SyncAutoReplyMessages(ctx, r.cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: []cloud.AutoReplyMessage{{Fingerprint: "offer", Direction: "candidate", MessageType: "card", TextContent: "对方想发送附件简历给您，您是否同意", CardContent: json.RawMessage(`{"info_type":"resume","info_status":"offered"}`), PlatformSentAt: &at}}}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(replybridge.Request{Snapshot: model.AutoReplyConversationSnapshot{ResumeOfferMessageID: "offer", ExpectedSendMessageKey: "offer"}, InfoRequest: model.CandidateInfoRequest{RequestResume: true}})
	cmd := replybridge.Command{ID: "offer-command", RunID: r.id, PlatformID: r.platform, Action: "request_info", Deadline: time.Now().Add(time.Minute), Payload: payload}
	ordinaryKey := "info-request:resume"
	if _, _, err := r.client.ClaimAutoReplySend(ctx, r.cred, conv.ID, ordinaryKey, cloud.AutoReplyResumeOfferContent, 0); err != nil {
		t.Fatal(err)
	}
	wrongCtx := cloud.WithAutoReplySendIntent(ctx, cloud.AutoReplySendIntentID(conv.ID, ordinaryKey, 0))
	if err := r.persistCommand(wrongCtx, cmd, "pending", replybridge.Result{}); err == nil {
		t.Fatal("普通索要意图被用于同意附件")
	}
	key := cloud.AutoReplyResumeOfferSendKey("offer")
	if _, _, err := r.client.ClaimAutoReplySend(ctx, r.cred, conv.ID, key, cloud.AutoReplyResumeOfferContent, 0); err != nil {
		t.Fatal(err)
	}
	boundCtx := cloud.WithAutoReplySendIntent(ctx, cloud.AutoReplySendIntentID(conv.ID, key, 0))
	if err := r.persistCommand(boundCtx, cmd, "pending", replybridge.Result{}); err != nil {
		t.Fatal(err)
	}
	var bound commandState
	if err := json.Unmarshal(fixture.data["runtime_commands|offer-command"].Payload, &bound); err != nil || bound.ResumeOfferKey != "offer" || bound.SourceKey != "offer" {
		t.Fatal("同意来源证明丢失", bound, err)
	}
	conv, _, _ = r.client.AutoReplyAnswerConversation(ctx, r.cred, conv.ID)
	conv.Status = "closed"
	if _, err := r.client.SaveAutoReplyConversation(ctx, r.cred, conv); err != nil {
		t.Fatal(err)
	}
	err := r.persistCommand(ctx, cmd, "executing", replybridge.Result{})
	var rejected *replybridge.CommandRejectedError
	if !errors.As(err, &rejected) {
		t.Fatal("会话关闭后仍派发同意", err)
	}
}
