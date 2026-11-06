// Package auto_reply 本文件验证持久待处理消息、账号隔离和发送结果不明时的跨任务保护。
package auto_reply

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// reliabilityResponder 返回可控的AI结果，并记录完整未处理消息批次。
type reliabilityResponder struct {
	calls    int
	failure  error
	decision ReplyDecision
	pending  []cloud.AutoReplyMessage
}

// Reply 只生成测试提议，不访问真实模型或招聘页面。
func (r *reliabilityResponder) Reply(_ context.Context, input ReplyContext) (ReplyDecision, error) {
	r.calls++
	r.pending = append([]cloud.AutoReplyMessage(nil), input.PendingMessages...)
	return r.decision, r.failure
}

// newReliabilityFlow 使用临时SQLite和本地存储构建真实编排依赖。
func newReliabilityFlow(t *testing.T, baseURL string) (*Flow, shared.PreparedTask) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	localCloud := cloud.New(baseURL)
	localCloud.EnableLocalAutoReply(store, t.TempDir())
	prepared := shared.PreparedTask{Request: shared.StartRequest{TaskID: "task-1", Token: "token", PlatformAccountID: shared.AutoReplyAccountKey("boss", "账号甲")}, MachineID: "machine", Platform: model.Config{ID: "boss", Name: "BOSS"}}
	return &Flow{Browser: &autoReplyBrowserStub{}, Store: store, Cloud: localCloud}, prepared
}

// TestFailedMessageRetriesAfterSync 验证已经采集但AI超时的消息下一轮仍被恢复，连续入站消息一起交给AI。
func TestFailedMessageRetriesAfterSync(t *testing.T) {
	flow, prepared := newReliabilityFlow(t, "")
	responder := &reliabilityResponder{failure: fmt.Errorf("模拟AI请求超时")}
	flow.Responder = responder
	first := model.ConversationMessage{PlatformMessageID: "m1", Direction: "candidate", MessageType: "text", TextContent: "薪资多少？"}
	last := model.ConversationMessage{PlatformMessageID: "m2", Direction: "candidate", MessageType: "text", TextContent: "双休吗？"}
	runtime := &autoReplyRuntimeStub{opened: model.AutoReplyConversationSnapshot{CandidateName: "候选人甲", PlatformThreadID: "thread", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{first, last}}, latestBefore: last}
	positions := []cloud.AutoReplyPositionSnapshot{{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}}}
	current := &openAutoReplyConversation{conversation: model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "候选人甲", CommunicationPosition: "工程师"}}
	stats := &shared.Stats{}
	if err := flow.processConversation(context.Background(), prepared, runtime, positions, current.conversation, &current.snapshot, stats); err == nil {
		t.Fatal("应返回模拟AI错误")
	}
	if responder.calls != 1 || len(responder.pending) != 2 {
		t.Fatalf("首轮未传入完整消息批次：calls=%d messages=%d", responder.calls, len(responder.pending))
	}
	prepared.Request.TaskID = "task-after-restart"
	processed, _, err := flow.processCheckpoint(context.Background(), prepared, unreadScannerStub{}, runtime, positions, 3, stats, &shared.ConsecutiveErrorPolicy{}, nil)
	if err != nil || processed != 1 || responder.calls != 2 {
		t.Fatalf("已读会话未被持久待办恢复：processed=%d calls=%d err=%v", processed, responder.calls, err)
	}
	state, err := flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"})
	if err != nil || state.Candidate == nil || state.Candidate.Phone != "" {
		t.Fatalf("应建立无手机号的本地临时候选：state=%+v err=%v", state, err)
	}
}

// TestUnknownSendSurvivesNewTask 验证点击结果不明确后，即使任务编号和AI措辞改变也不得再次发送。
func TestUnknownSendSurvivesNewTask(t *testing.T) {
	flow, prepared := newReliabilityFlow(t, "")
	conversation, err := flow.Cloud.SaveAutoReplyConversation(context.Background(), credentials(prepared), cloud.AutoReplyConversation{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread", PositionID: "position"})
	if err != nil {
		t.Fatal(err)
	}
	message := model.ConversationMessage{PlatformMessageID: "m1", Direction: "candidate", MessageType: "text", TextContent: "薪资多少？"}
	runtime := &autoReplyRuntimeStub{latestBefore: message, sendErr: fmt.Errorf("模拟发送按钮超时")}
	_, err = flow.sendVerifiedMessage(context.Background(), prepared, runtime, conversation, model.AutoReplyConversationSnapshot{}, "m1", "薪资面议", true, 1)
	if !errors.Is(err, errAutoReplySendNeedsReview) || !runtime.sent {
		t.Fatalf("应记录发送结果不明：sent=%t err=%v", runtime.sent, err)
	}
	prepared.Request.TaskID = "new-task"
	runtime.sent = false
	runtime.sendErr = nil
	_, err = flow.sendVerifiedMessage(context.Background(), prepared, runtime, conversation, model.AutoReplyConversationSnapshot{}, "m1", "薪资面议", true, 1)
	if !errors.Is(err, errAutoReplySendNeedsReview) || runtime.sent {
		t.Fatalf("换任务后不应重发：sent=%t err=%v", runtime.sent, err)
	}
	_, err = flow.sendVerifiedMessage(context.Background(), prepared, runtime, conversation, model.AutoReplyConversationSnapshot{}, "m1", "工资需要面谈", true, 1)
	if err == nil || runtime.sent {
		t.Fatalf("改措辞不应绕过去重：sent=%t err=%v", runtime.sent, err)
	}
}

// TestDisabledConfigPreventsProposalAndSend 验证AI返回后关闭岗位，临时问答不会继续提交或发送。
func TestDisabledConfigPreventsProposalAndSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/positions/position/snapshot" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"position":{"id":"position"},"config":{"position_id":"position","enabled":false,"version":2},"company_profile":{"id":"company"},"subscription":{"active":true,"allow_auto_reply":true}}`))
	}))
	defer server.Close()
	flow, prepared := newReliabilityFlow(t, server.URL)
	flow.Responder = &reliabilityResponder{decision: ReplyDecision{Messages: []ReplyMessage{{Type: "answer", Content: "双休"}}}}
	message := model.ConversationMessage{PlatformMessageID: "m1", Direction: "candidate", MessageType: "text", TextContent: "双休吗？"}
	runtime := &autoReplyRuntimeStub{opened: model.AutoReplyConversationSnapshot{CandidateName: "候选人甲", PlatformThreadID: "thread", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{message}}, latestBefore: message}
	positions := []cloud.AutoReplyPositionSnapshot{{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{Version: 1}}}
	err := flow.processConversation(context.Background(), prepared, runtime, positions, model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "候选人甲", CommunicationPosition: "工程师"}, nil, &shared.Stats{})
	if err == nil || runtime.sent {
		t.Fatalf("关闭后应保留待办且不发消息：sent=%t err=%v", runtime.sent, err)
	}
	pending, err := flow.Cloud.ListPendingAutoReplyConversations(context.Background(), credentials(prepared), "boss", prepared.Request.PlatformAccountID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("关闭后的消息未保留：pending=%d err=%v", len(pending), err)
	}
}

// TestStructuredResumeWithoutPhoneStaysLocal 验证缺手机号的已解析简历可本地复用，不重复解析且不进入云端正式简历。
func TestStructuredResumeWithoutPhoneStaysLocal(t *testing.T) {
	flow, prepared := newReliabilityFlow(t, "")
	ctx := context.Background()
	conversation, err := flow.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), cloud.AutoReplyConversation{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread", PositionID: "position", CandidateName: "候选人甲"})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := flow.Cloud.EnsureLocalAutoReplyCandidate(ctx, credentials(prepared), conversation)
	if err != nil {
		t.Fatal(err)
	}
	conversation.CandidateID = candidate.ID
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err = os.WriteFile(path, []byte("七年工作经验，没有手机号"), 0600); err != nil {
		t.Fatal(err)
	}
	flow.Responder = resumeResponderStub{structured: StructuredResume{Candidate: cloud.StructuredCandidate{WorkYears: "7"}}}
	runtime := &autoReplyRuntimeStub{resumeBundle: model.AutoReplyResumeBundle{CandidateName: "候选人甲", OnlineResumeText: "七年工作经验", AttachmentPaths: []string{path}}}
	position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}}
	snapshot := model.AutoReplyConversationSnapshot{CandidateName: "候选人甲", ResumeCardAvailable: true, PlatformAccountID: prepared.Request.PlatformAccountID}
	_, handled, err := flow.ensureResume(ctx, prepared, runtime, position, &conversation, snapshot, cloud.AutoReplyCandidateState{Candidate: &candidate}, "message", &shared.Stats{})
	if err != nil || handled {
		t.Fatalf("缺手机号不应挡住问答：handled=%v err=%v", handled, err)
	}
	state, err := flow.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"})
	if err != nil || state.Candidate == nil || !state.Candidate.ResumeStructured || state.Candidate.Phone != "" || state.Candidate.ID != candidate.ID {
		t.Fatalf("本地资料没有补齐同一临时候选：state=%+v err=%v", state, err)
	}
	flow.Responder = resumeResponderStub{err: fmt.Errorf("不应重复解析")}
	if _, _, err = flow.ensureResume(ctx, prepared, runtime, position, &conversation, snapshot, state, "next-message", &shared.Stats{}); err != nil || runtime.resumeCollects != 1 {
		t.Fatalf("已解析资料没有复用：collects=%d err=%v", runtime.resumeCollects, err)
	}
}

// answeringRuntime 让测试发送后的页面回读反映当前实际消息，避免依赖真实浏览器。
type answeringRuntime struct {
	*autoReplyRuntimeStub
	replies []string
}

// SendAutoReplyMessage 模拟发送并产生可回读的本方消息编号。
func (r *answeringRuntime) SendAutoReplyMessage(_ context.Context, _ model.Browser, _ model.Config, _ model.AutoReplyConversationSnapshot, message string) error {
	r.replies = append(r.replies, message)
	r.sent = true
	r.latestAfter = model.ConversationMessage{PlatformMessageID: fmt.Sprintf("out-%d", len(r.replies)), Direction: "self", MessageType: "text", TextContent: message}
	return nil
}

// TestQuestionsAnsweredBeforeAndWhileWaitingResume 验证先回答问题再仅索要一次简历，等待简历期间下一轮仍正常回答。
func TestQuestionsAnsweredBeforeAndWhileWaitingResume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/positions/position/snapshot" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"position":{"id":"position"},"config":{"position_id":"position","enabled":true,"version":1},"company_profile":{"id":"company"},"subscription":{"active":true,"allow_auto_reply":true}}`))
	}))
	defer server.Close()
	flow, prepared := newReliabilityFlow(t, server.URL)
	flow.Responder = &reliabilityResponder{decision: ReplyDecision{Messages: []ReplyMessage{{Type: "answer", Content: "这个岗位双休"}}}}
	message := model.ConversationMessage{PlatformMessageID: "m1", Direction: "candidate", MessageType: "text", TextContent: "双休吗？"}
	runtime := &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{opened: model.AutoReplyConversationSnapshot{CandidateName: "候选人甲", PlatformThreadID: "thread", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{message}}, latestBefore: message}}
	positions := []cloud.AutoReplyPositionSnapshot{{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{Version: 1}}}
	conversation := model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "候选人甲", CommunicationPosition: "工程师"}
	if err := flow.processConversation(context.Background(), prepared, runtime, positions, conversation, nil, &shared.Stats{}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.replies) != 2 || runtime.replies[0] != "这个岗位双休" || runtime.resumeRequests != 1 {
		t.Fatalf("没有先回答再索要一次简历：replies=%v requests=%d", runtime.replies, runtime.resumeRequests)
	}
	message.PlatformMessageID = "m2"
	message.TextContent = "再确认一下是双休吧？"
	runtime.sent = false
	runtime.latestBefore = message
	runtime.opened.Messages = []model.ConversationMessage{message}
	if err := flow.processConversation(context.Background(), prepared, runtime, positions, conversation, nil, &shared.Stats{}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.replies) != 3 || runtime.resumeRequests != 1 {
		t.Fatalf("等待简历时未正常回答或重复索要：replies=%v requests=%d", runtime.replies, runtime.resumeRequests)
	}
}

// TestCompanyChangeInvalidatesPreparedReply 验证岗位版本未变时，公司资料变更仍废弃旧回答。
func TestCompanyChangeInvalidatesPreparedReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"position":{"id":"position"},"config":{"position_id":"position","enabled":true,"version":1},"company_profile":{"id":"company","address":"新地址"},"subscription":{"active":true,"allow_auto_reply":true}}`))
	}))
	defer server.Close()
	flow, prepared := newReliabilityFlow(t, server.URL)
	expected := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position"}, Config: cloud.PositionAutoReplyConfig{Version: 1}, CompanyProfile: cloud.CompanyProfile{ID: "company", Address: "旧地址"}}
	if err := flow.verifyCurrentReplyConfiguration(context.Background(), prepared, expected); err == nil {
		t.Fatal("公司资料变化后不得发送旧答案")
	}
}
