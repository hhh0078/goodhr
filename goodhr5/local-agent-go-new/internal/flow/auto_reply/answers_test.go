// Package auto_reply 本文件验证 HR 公开补答沿原会话送达、断线后只补回执、候选人插话时先处理新消息。
package auto_reply

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestResolvedAnswerSendsOnceAndRestoresReceipt 实际调用补答HTTP和发送账本，制造发送成功后服务器收据暂时失效。
func TestResolvedAnswerSendsOnceAndRestoresReceipt(t *testing.T) {
	ctx := context.Background()
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", "private-fixture")
	var claim, receipt int
	p := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "p", PlatformID: "boss"}, Config: cloud.PositionAutoReplyConfig{PositionID: "p", Enabled: true}, CompanyProfile: cloud.CompanyProfile{ID: "co"}, Subscription: cloud.AutoReplySubscription{Active: true, AllowAutoReply: true}}
	a := cloud.AutoReplyAnswer{ID: "delivery-1", PositionID: "p", PlatformID: "boss", PlatformThreadID: "thread", SourceMessageKey: "source", Answer: "岗位餐补每月三百元。"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/snapshot") {
			json.NewEncoder(w).Encode(p)
			return
		}
		if r.Header.Get("X-GoodHR-Runtime-Service") != "private-fixture" {
			http.Error(w, "forbidden", 403)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "pending-answers"):
			json.NewEncoder(w).Encode(map[string]any{"answers": []cloud.AutoReplyAnswer{a}})
		case strings.HasSuffix(r.URL.Path, "answer-claim"):
			claim++
			json.NewEncoder(w).Encode(map[string]any{"claimed": true})
		case strings.HasSuffix(r.URL.Path, "answer-receipt"):
			receipt++
			if receipt == 1 {
				http.Error(w, "lost receipt", 503)
				return
			}
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["outbound_message_key"] != "actual-out" || body["status"] != "sent" {
				t.Errorf("missing actual delivery proof: %+v", body)
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	flow, prepared := newReliabilityFlow(t, server.URL)
	cred := credentials(prepared)
	a.PlatformAccountID = prepared.Request.PlatformAccountID
	c, err := flow.Cloud.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PlatformID: "boss", PlatformAccountID: a.PlatformAccountID, PlatformThreadID: "thread", PositionID: "p", CandidateName: "测试甲", CandidateID: "candidate"})
	if err != nil {
		t.Fatal(err)
	}
	a.ConversationID = c.ID
	_, err = flow.Cloud.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: c.ID, Messages: []cloud.AutoReplyMessage{{PlatformMessageID: "source", Direction: "candidate", MessageType: "text", TextContent: "有餐补吗？"}, {PlatformMessageID: "ack", Direction: "self", MessageType: "text", TextContent: "我确认下。"}}})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := flow.readAnswerQueue(ctx, prepared, []cloud.AutoReplyPositionSnapshot{p})
	if err != nil || len(queued) != 1 {
		t.Fatalf("补答无法进入队列 %v %+v", err, queued)
	}
	rt := &autoReplyRuntimeStub{latestBefore: model.ConversationMessage{PlatformMessageID: "interrupt", Direction: "candidate", TextContent: "新问题"}, latestAfter: model.ConversationMessage{PlatformMessageID: "actual-out", Direction: "self", MessageType: "text", TextContent: a.Answer}}
	page := model.AutoReplyConversationSnapshot{Conversation: model.Conversation{PlatformThreadID: "thread", PlatformAccountID: a.PlatformAccountID}, PlatformThreadID: "thread", PlatformAccountID: a.PlatformAccountID}
	send := func() error {
		return flow.deliverAnswer(ctx, prepared, rt, p, c, page, cloud.AutoReplyCandidateState{}, a, &shared.Stats{})
	}
	if err = send(); err != nil || rt.sent || claim != 0 {
		t.Fatalf("候选人插话时仍然发送: %v", err)
	}
	rt.latestBefore = model.ConversationMessage{PlatformMessageID: "ack", Direction: "self", MessageType: "text", TextContent: "我确认下。"}
	if err = send(); err == nil || !rt.sent || claim != 1 || receipt != 1 {
		t.Fatalf("未制造回执断线: %v %v %d %d", err, rt.sent, claim, receipt)
	}
	rt.sent = false
	if err = send(); err != nil || rt.sent || claim != 1 || receipt != 2 {
		t.Fatalf("已发消息不应重发: %v %v %d %d", err, rt.sent, claim, receipt)
	}
}
