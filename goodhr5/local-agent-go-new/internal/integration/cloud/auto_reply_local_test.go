// Package cloud 本文件验证自动回复全文仅在本机持久化、账号隔离、失败关闭和审计保留上限。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// TestLocalAutoReplyPersistsWithoutSensitiveRequests 验证聊天、简历和AI全文重启后仍可读取，且不产生云端请求。
func TestLocalAutoReplyPersistsWithoutSensitiveRequests(t *testing.T) {
	ctx := context.Background()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "敏感请求不得上云", 500) }))
	defer server.Close()
	directory := t.TempDir()
	store, err := storage.Open(filepath.Join(directory, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	credentials := AgentCredentials{Token: "token", MachineID: "machine"}
	client := New(server.URL)
	client.EnableLocalAutoReply(store, directory)
	identity, err := client.SaveAutoReplyIdentity(ctx, credentials, CandidatePlatformIdentity{PlatformID: "test", PlatformCandidateID: "person"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := client.SaveAutoReplyConversation(ctx, credentials, AutoReplyConversation{PlatformID: "test", PlatformThreadID: "thread", PlatformIdentityID: identity.ID, PositionID: "position"})
	if err != nil {
		t.Fatal(err)
	}
	syncRequest := AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: []AutoReplyMessage{{PlatformMessageID: "message", Direction: "candidate", TextContent: "本机聊天全文"}}}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err = client.SyncAutoReplyMessages(ctx, credentials, syncRequest); err != nil {
			t.Fatal(err)
		}
	}
	candidate, err := client.SaveAutoReplyCandidate(ctx, credentials, AutoReplyCandidateInput{StructuredCandidate: StructuredCandidate{CandidateName: "测试甲", Phone: "13800138000", RawText: "本机简历原文"}, PlatformID: "test", PlatformCandidateID: "person", PositionID: "position"})
	if err != nil {
		t.Fatal(err)
	}
	conversation.CandidateID = candidate.CandidateID
	if _, err = client.SaveAutoReplyConversation(ctx, credentials, conversation); err != nil {
		t.Fatal(err)
	}
	if _, err = client.StartAutoReplyAIRun(ctx, credentials, AutoReplyAIRun{ConversationID: conversation.ID, TraceID: "trace", BasedOnMessageKey: "message", InputMessages: json.RawMessage(`[{"content":"本机AI全文"}]`)}); err != nil {
		t.Fatal(err)
	}
	restarted := New(server.URL)
	restarted.EnableLocalAutoReply(store, directory)
	state, err := restarted.AutoReplyCandidateState(ctx, credentials, AutoReplyCandidateLookup{PlatformID: "test", PlatformCandidateID: "person", PlatformThreadID: "thread"})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := restarted.AutoReplyMessages(ctx, credentials, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Found || state.Candidate == nil || state.Candidate.RawText != "本机简历原文" || len(messages) != 1 || messages[0].TextContent != "本机聊天全文" || calls != 0 {
		t.Fatalf("本地持久化错误：%+v messages=%+v calls=%d", state, messages, calls)
	}
	other, err := restarted.AutoReplyCandidateState(ctx, AgentCredentials{Token: "other", MachineID: "machine"}, AutoReplyCandidateLookup{PlatformID: "test", PlatformCandidateID: "person"})
	if err != nil || other.Found {
		t.Fatalf("账号隔离失效：%+v %v", other, err)
	}
}

// TestLocalAutoReplyNullMapsAndMissingStoreFailClosed 验证旧空字段可恢复，正式存储绑定失败绝不回退网络。
func TestLocalAutoReplyNullMapsAndMissingStoreFailClosed(t *testing.T) {
	ctx := context.Background()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) }))
	defer server.Close()
	directory := t.TempDir()
	store, err := storage.Open(filepath.Join(directory, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	credentials := AgentCredentials{Token: "token", MachineID: "machine"}
	client := New(server.URL)
	client.EnableLocalAutoReply(store, directory)
	if err = store.SetSetting(ctx, client.localAutoReplyKey(credentials), `{"identities":null,"conversations":null,"messages":null,"candidates":null,"confirmations":null,"attachments":null,"runs":null,"tools":null,"suggestions":null,"notifications":null,"cloud_candidates":null,"reports":null}`); err != nil {
		t.Fatal(err)
	}
	if _, err = client.SaveAutoReplyIdentity(ctx, credentials, CandidatePlatformIdentity{PlatformID: "test", PlatformCandidateID: "person"}); err != nil {
		t.Fatal(err)
	}
	client.EnableLocalAutoReply(nil, directory)
	if _, err = client.SaveAutoReplyIdentity(ctx, credentials, CandidatePlatformIdentity{PlatformID: "test", PlatformCandidateID: "person"}); err == nil {
		t.Fatal("未绑定存储应拒绝操作")
	}
	if _, err = client.SaveAutoReplyCandidate(ctx, credentials, AutoReplyCandidateInput{PositionID: "position", PlatformID: "test", StructuredCandidate: StructuredCandidate{Phone: "13800138000"}}); err == nil {
		t.Fatal("未绑定存储不能上传简历")
	}
	if calls != 0 {
		t.Fatalf("存储失败后意外发送了%d个云端请求", calls)
	}
}

// TestLocalAutoReplyAuditLimitKeepsReferences 验证按完整AI运行裁剪，不遗留孤立工具记录。
func TestLocalAutoReplyAuditLimitKeepsReferences(t *testing.T) {
	data := localAutoReplyData{Runs: map[string]AutoReplyAIRun{}, Tools: map[string]AutoReplyToolCall{}}
	for index := 0; index < 1002; index++ {
		id := fmt.Sprint(index)
		data.Runs[id] = AutoReplyAIRun{ID: id, StartedAt: time.Unix(int64(index), 0)}
		data.Tools[id] = AutoReplyToolCall{ID: id, AIRunID: id}
	}
	trimLocalAutoReplyAudits(&data)
	if len(data.Runs) > 1000 || len(data.Tools) > 1000 {
		t.Fatalf("审计超限：%d/%d", len(data.Runs), len(data.Tools))
	}
	for _, tool := range data.Tools {
		if _, ok := data.Runs[tool.AIRunID]; !ok {
			t.Fatal("工具引用已删除的AI运行")
		}
	}
}

// TestLocalAutoReplyReportClaimsSurviveRestart 验证报告领取租约、最多三次重试和完成后复用均持久化。
func TestLocalAutoReplyReportClaimsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	store, err := storage.Open(filepath.Join(directory, "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	credentials := AgentCredentials{Token: "token", MachineID: "machine"}
	client := New("")
	client.EnableLocalAutoReply(store, directory)
	for attempt := 0; attempt < 3; attempt++ {
		_, claimed, err := client.ClaimAutoReplyReport(ctx, credentials, "retry")
		if err != nil || !claimed {
			t.Fatalf("第%d次未领取：%v %t", attempt, err, claimed)
		}
		restarted := New("")
		restarted.EnableLocalAutoReply(store, directory)
		if _, again, err := restarted.ClaimAutoReplyReport(ctx, credentials, "retry"); err != nil || again {
			t.Fatalf("运行中租约被重复领取：%t %v", again, err)
		}
		if err = client.SaveAutoReplyReport(ctx, credentials, "retry", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, claimed, err := client.ClaimAutoReplyReport(ctx, credentials, "retry"); err != nil || claimed {
		t.Fatalf("超过三次仍重复调用AI：%t %v", claimed, err)
	}
	report := AutoReplyRecommendationReport{ExecutiveSummary: "本机报告"}
	if err = client.SaveAutoReplyReport(ctx, credentials, "completed", &report); err != nil {
		t.Fatal(err)
	}
	encoded, claimed, err := client.ClaimAutoReplyReport(ctx, credentials, "completed")
	if err != nil || claimed || !json.Valid(encoded) {
		t.Fatalf("已完成报告没有复用：%t %s %v", claimed, encoded, err)
	}
}
