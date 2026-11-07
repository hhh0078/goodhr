// Package auto_reply 本文件以临时SQLite和页面替身验证附件失败仍能答疑、无新消息恢复及禁止重复发送。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

type resumeRetryResponder struct {
	answers, reviews, structures int
	firstInput                   ReplyContext
}

// Reply 分别模拟岗位回答和内部证据复核，测试不调用真实AI。
func (r *resumeRetryResponder) Reply(_ context.Context, input ReplyContext) (ReplyDecision, error) {
	if !input.ConditionsOnly {
		r.answers++
		r.firstInput = input
		return ReplyDecision{Messages: []ReplyMessage{{Type: "answer", Content: "工作地点在成都，负责高中数学教学"}}}, nil
	}
	r.reviews++
	updates := append([]cloud.CandidateConfirmationItem(nil), input.ConfirmationItems...)
	for index := range updates {
		updates[index].Status, updates[index].SourceRef = "matched", "resume"
		updates[index].EvidenceText, updates[index].StatusReason = "五年教学经验", "已核对简历原文"
	}
	return ReplyDecision{ConfirmationUpdates: updates}, nil
}

// StructureResume 检查独立预算并返回合成的结构化简历，不读取真实候选人资料。
func (r *resumeRetryResponder) StructureResume(ctx context.Context, _ ResumeStructureContext) (StructuredResume, error) {
	r.structures++
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > autoReplyResumeStructureTimeout {
		return StructuredResume{}, fmt.Errorf("简历结构化缺少有限预算")
	}
	return StructuredResume{Candidate: cloud.StructuredCandidate{CandidateName: "合成人选", Phone: "13800138000", WorkYears: "5"}, RawText: "五年教学经验"}, nil
}

// advanceResumeRetryFixture 只推进临时测试库的待办时间，模拟重启后到期而不实际等待一分钟。
func advanceResumeRetryFixture(t *testing.T, flow *Flow, prepared shared.PreparedTask, id string) {
	t.Helper()
	first := sha256.Sum256([]byte(prepared.Request.Token))
	second := sha256.Sum256([]byte(hex.EncodeToString(first[:])))
	owner := "auto_reply:v1:" + hex.EncodeToString(second[:])
	if err := flow.Store.AutoReplyTransaction(context.Background(), owner, func(tx *storage.AutoReplyTx) error {
		row, found, err := tx.Get("conversations", id)
		if err != nil || !found {
			return fmt.Errorf("测试会话不存在：%v", err)
		}
		var saved cloud.AutoReplyConversation
		if err = json.Unmarshal(row.Data, &saved); err != nil {
			return err
		}
		saved.ResumeRetry.NextAttemptAt = time.Now().Add(-time.Minute)
		return tx.Put("conversations", id, saved)
	}); err != nil {
		t.Fatal(err)
	}
}

// TestResumeFailureAnswersThenRecoversWithoutNewMessage 验证先答问题再尝试附件，采集失败留待办，重启后只补审不重发。
func TestResumeFailureAnswersThenRecoversWithoutNewMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/positions/position/snapshot" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"position":{"id":"position"},"config":{"position_id":"position","enabled":true,"version":1},"company_profile":{"id":"company"},"subscription":{"active":true,"allow_auto_reply":true}}`))
	}))
	defer server.Close()
	flow, prepared := newReliabilityFlow(t, server.URL)
	responder := &resumeRetryResponder{}
	flow.Responder = responder
	card := model.ConversationMessage{PlatformMessageID: "resume-source", Direction: "candidate", MessageType: "resume", TextContent: "合成附件"}
	question := model.ConversationMessage{PlatformMessageID: "question", Direction: "candidate", MessageType: "text", TextContent: "工作地点和内容是什么？"}
	runtime := &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{
		resumeErr: fmt.Errorf("合成附件入口 CLICK_FAILED"), latestBefore: question,
		opened: model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformThreadID: "thread", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{card, question}, ResumeCardAvailable: true, ResumeSourceMessageID: card.PlatformMessageID},
	}}
	position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{Version: 1, Conditions: []cloud.PositionReplyCondition{{ID: "condition", Type: "required", Content: "至少五年教学经验", Enabled: true}}}}
	conversation := model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "合成人选", CommunicationPosition: "工程师"}
	stats := &shared.Stats{}
	current := &openAutoReplyConversation{conversation: conversation}
	if err := flow.processConversation(context.Background(), prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, conversation, &current.snapshot, stats); err != nil {
		t.Fatal(err)
	}
	lookup := cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"}
	state, err := flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), lookup)
	if err != nil || state.Conversation == nil || state.Conversation.ResumeRetry == nil || state.Conversation.ResumeRetry.Attempts != 0 || runtime.resumeCollects != 0 || responder.structures != 0 {
		t.Fatalf("首条答疑提前采集或消费重试次数：state=%+v collects=%d structures=%d err=%v", state.Conversation, runtime.resumeCollects, responder.structures, err)
	}
	if len(runtime.replies) != 1 || runtime.resumeRequests != 0 || responder.answers != 1 || responder.firstInput.Resume != nil || state.Candidate.ResumeStructured || stats.Failed != 0 {
		t.Fatalf("失败阻断答疑、重复索简历或伪造资料：replies=%v requests=%d answers=%d candidate=%+v stats=%+v", runtime.replies, runtime.resumeRequests, responder.answers, state.Candidate, stats)
	}
	if len(runtime.readKnownKeys) != 0 {
		t.Fatalf("本轮已加载来源卡仍重复全量读取：%v", runtime.readKnownKeys)
	}
	items, err := flow.Cloud.AutoReplyConfirmationItems(context.Background(), credentials(prepared), state.Conversation.ID)
	if err != nil || len(items) != 1 || items[0].Status != "pending" {
		t.Fatalf("无简历时擅自满足条件：items=%+v err=%v", items, err)
	}
	if _, qualified := recommendationSource(ReplyContext{Position: position, CandidateState: state}, items); qualified {
		t.Fatal("未结构化资料不应生成报告")
	}
	scannerCalls := 0
	processed, next, err := flow.processCheckpoint(context.Background(), prepared, unreadScannerStub{calls: &scannerCalls}, runtime, []cloud.AutoReplyPositionSnapshot{position}, 1, stats, &shared.ConsecutiveErrorPolicy{}, current)
	if scannerCalls != 0 || next != current || !current.scanNext {
		t.Fatalf("同窗已到期简历工作绕路：扫描 %d 次，复用 %t", scannerCalls, next == current)
	}
	state, stateErr := flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), lookup)
	if err != nil || stateErr != nil || processed != 1 || state.Conversation.ResumeRetry == nil || state.Conversation.ResumeRetry.Attempts != 1 || !strings.Contains(state.Conversation.ResumeRetry.LastError, "CLICK_FAILED") || len(runtime.replies) != 1 || responder.answers != 1 {
		t.Fatalf("下一轮附件失败未持久记录或重发答案：processed=%d state=%+v replies=%v err=%v/%v", processed, state.Conversation, runtime.replies, err, stateErr)
	}
	advanceResumeRetryFixture(t, flow, prepared, state.Conversation.ID)
	path := filepath.Join(t.TempDir(), "synthetic-resume.txt")
	if err = os.WriteFile(path, []byte("五年教学经验"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime.resumeErr = nil
	runtime.resumeBundle = model.AutoReplyResumeBundle{OnlineResumeText: "五年教学经验", AttachmentPaths: []string{path}, ResumeSourceMessageID: card.PlatformMessageID}
	prepared.Request.TaskID = "restarted-task"
	processed, _, err = flow.processCheckpoint(context.Background(), prepared, unreadScannerStub{}, runtime, []cloud.AutoReplyPositionSnapshot{position}, 1, stats, &shared.ConsecutiveErrorPolicy{}, nil)
	if err != nil || processed != 1 || responder.reviews != 1 || responder.structures != 1 || responder.answers != 1 || len(runtime.replies) != 1 {
		t.Fatalf("无新消息恢复未完成或重发：processed=%d answers=%d reviews=%d structures=%d replies=%v err=%v", processed, responder.answers, responder.reviews, responder.structures, runtime.replies, err)
	}
	state, err = flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), lookup)
	if err != nil || state.Conversation.ResumeRetry != nil || !state.Candidate.ResumeStructured || state.Candidate.ResumeSourceMessageID != card.PlatformMessageID {
		t.Fatalf("恢复后资料/待办未正确提交：state=%+v err=%v", state, err)
	}
	items, err = flow.Cloud.AutoReplyConfirmationItems(context.Background(), credentials(prepared), state.Conversation.ID)
	if err != nil || items[0].Status != "matched" {
		t.Fatalf("内部条件未补审：%+v %v", items, err)
	}
	if _, qualified := recommendationSource(ReplyContext{Position: position, CandidateState: state}, items); !qualified {
		t.Fatal("成功补审后没有恢复推荐资格")
	}
	if len(state.Attachments) != 1 || state.Attachments[0].SourceMessageID == "" || state.Attachments[0].SourceMessageID == card.PlatformMessageID {
		t.Fatalf("附件没有绑定真实内部来源编号：%+v", state.Attachments)
	}
	pending, err := flow.Cloud.PendingAutoReplyMessages(context.Background(), credentials(prepared), state.Conversation.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("补审改动已回复消息水位：%+v %v", pending, err)
	}
	// 已整理旧附件后又来新附件：新来源必须领取新待办，失败不复用旧资料继续生成报告。
	card.PlatformMessageID = "new-resume-source"
	question.PlatformMessageID = "new-question"
	runtime.opened.Messages = []model.ConversationMessage{card, question}
	runtime.opened.ResumeSourceMessageID = card.PlatformMessageID
	runtime.sent, runtime.latestBefore = false, question
	runtime.resumeErr = fmt.Errorf("新附件入口 CLICK_FAILED")
	if err = flow.processConversation(context.Background(), prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, conversation, nil, stats); err != nil {
		t.Fatal("新附件失败不应阻断问答", err)
	}
	state, err = flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), lookup)
	if err != nil || state.Conversation.ResumeRetry == nil || state.Conversation.ResumeRetry.SourceMessageID != card.PlatformMessageID || state.Conversation.ResumeRetry.Attempts != 0 || runtime.resumeCollects != 2 || len(runtime.replies) != 2 || responder.firstInput.CandidateState.Candidate.ResumeStructured {
		t.Fatalf("新来源被旧结构化资料绕过：retry=%+v replies=%v err=%v", state.Conversation.ResumeRetry, runtime.replies, err)
	}
	processed, _, err = flow.processCheckpoint(context.Background(), prepared, unreadScannerStub{}, runtime, []cloud.AutoReplyPositionSnapshot{position}, 1, stats, &shared.ConsecutiveErrorPolicy{}, nil)
	state, stateErr = flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), lookup)
	if err != nil || stateErr != nil || processed != 1 || state.Conversation.ResumeRetry.Attempts != 1 || runtime.resumeCollects != 3 || len(runtime.replies) != 2 {
		t.Fatalf("新来源下一轮采集失败被吞掉或重复回答：state=%+v replies=%v err=%v/%v", state.Conversation, runtime.replies, err, stateErr)
	}
	if err = (&AIResponder{Cloud: flow.Cloud}).QueueRecommendation(context.Background(), ReplyContext{Credentials: credentials(prepared), Position: position, Conversation: *state.Conversation, CandidateState: state, BasedOnMessageKey: question.PlatformMessageID}); err != nil {
		t.Fatal(err)
	}
	jobs, err := flow.Cloud.PendingAutoReplyRecommendations(context.Background(), credentials(prepared))
	if err != nil || len(jobs) != 0 {
		t.Fatalf("新附件未处理完仍使用旧简历排报告：%+v %v", jobs, err)
	}
}

// TestConditionsOnlyRejectsSendingAndUnparsedResume 验证补审不向模型暴露发送工具，也不信任未结构化的候选人字段。
func TestConditionsOnlyRejectsSendingAndUnparsedResume(t *testing.T) {
	input := ReplyContext{ConditionsOnly: true, BasedOnMessageKey: "question", Messages: []cloud.AutoReplyMessage{{PlatformMessageID: "question", Direction: "candidate", TextContent: "工作地点？"}}, CandidateState: cloud.AutoReplyCandidateState{Candidate: &cloud.AutoReplyStoredCandidate{RawText: "五年教学经验"}}}
	bounded, err := boundedReplyContext(input)
	if err != nil || len(bounded.PendingMessages) != 0 {
		t.Fatalf("补审把历史问题重新当作待答：%+v %v", bounded.PendingMessages, err)
	}
	tools := autoReplyToolsForContext(input)
	if len(tools) != 2 || tools[0].Function.Name != toolUpsertConfirmations || tools[1].Function.Name != toolFinishReview {
		t.Fatalf("补审工具范围不正确：%+v", tools)
	}
	state := toolExecutionState{input: input}
	if _, err = state.execute(context.Background(), ai.ToolCall{Type: "function", Function: ai.ToolCallFunction{Name: toolSendMessages, Arguments: `{"messages":[{"type":"answer","content":"历史答案"}]}`}}); err == nil {
		t.Fatal("补审允许发送工具")
	}
	if err = state.validateConfirmationEvidence(cloud.CandidateConfirmationItem{Status: "matched", SourceRef: "resume", EvidenceText: "五年教学经验"}); err == nil {
		t.Fatal("未结构化资料被当作正式简历证据")
	}
	if _, err = state.execute(context.Background(), ai.ToolCall{Type: "function", Function: ai.ToolCallFunction{Name: toolFinishReview, Arguments: `{}`}}); err != nil || !state.reviewFinished {
		t.Fatal("补审无法正常结束", err)
	}
}

// TestResumeSourcePreparationUsesPlatformOwnership 验证已加载来源或平台自行寻源时跳过通用完整聊天预读。
func TestResumeSourcePreparationUsesPlatformOwnership(t *testing.T) {
	for _, platformLoads := range []bool{false, true} {
		t.Run(fmt.Sprintf("platform_loads=%t", platformLoads), func(t *testing.T) {
			flow := &Flow{}
			runtime := &autoReplyRuntimeStub{resumeErr: fmt.Errorf("合成采集失败")}
			prepared := shared.PreparedTask{Platform: model.Config{Behavior: model.Behavior{AutoReplyResumeLoadsHistory: platformLoads}}}
			snapshot := model.AutoReplyConversationSnapshot{ResumeCardAvailable: true, ResumeSourceMessageID: "source"}
			if !platformLoads {
				snapshot.Messages = []model.ConversationMessage{{PlatformMessageID: "source", Direction: "candidate", MessageType: "resume"}}
			}
			_, _, err := flow.ensureResume(context.Background(), prepared, runtime, cloud.AutoReplyPositionSnapshot{}, &cloud.AutoReplyConversation{}, snapshot, cloud.AutoReplyCandidateState{}, "question", &shared.Stats{})
			if !errors.Is(err, errAutoReplyResumeDeferred) || len(runtime.readKnownKeys) != 0 || runtime.resumeCollects != 1 {
				t.Fatalf("重复预读或跳过平台采集：keys=%v collects=%d err=%v", runtime.readKnownKeys, runtime.resumeCollects, err)
			}
		})
	}
}

// TestResumeCancellationDoesNotContinueAnswer 验证用户停止任务后即使附件失败也不能进入AI问答。
func TestResumeCancellationDoesNotContinueAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	flow, prepared := newReliabilityFlow(t, "")
	responder := &resumeRetryResponder{}
	flow.Responder = responder
	err := flow.processConversation(ctx, prepared, &autoReplyRuntimeStub{}, nil, model.Conversation{Key: "thread"}, nil, &shared.Stats{})
	if err == nil || !errors.Is(err, context.Canceled) || responder.answers != 0 {
		t.Fatalf("取消后仍继续问答：calls=%d err=%v", responder.answers, err)
	}
}

// TestLastInboundWasResume 验证只有刚收到的简历卡会免去签收后的空等，历史卡和普通消息保持原轮询间隔。
func TestLastInboundWasResume(t *testing.T) {
	card := model.ConversationMessage{Direction: "candidate", MessageType: "resume"}
	system := model.ConversationMessage{Direction: "system"}
	for _, tc := range []struct {
		messages []model.ConversationMessage
		want     bool
	}{
		{nil, false}, {[]model.ConversationMessage{system}, false}, {[]model.ConversationMessage{card, system}, true},
		{[]model.ConversationMessage{card, {Direction: "candidate", MessageType: "text"}}, false},
		{[]model.ConversationMessage{card, {Direction: "self", MessageType: "text"}}, false},
	} {
		if got := lastInboundWasResume(tc.messages); got != tc.want {
			t.Fatalf("收到简历后是否立即核对待办：got=%v want=%v", got, tc.want)
		}
	}
}

// TestStandaloneResumeReceiptScope 验证只有既有沟通中的纯卡才固定签收，候选人的文字、尚未讨论的目标及不明旧卡均保留正常问答。
func TestStandaloneResumeReceiptScope(t *testing.T) {
	card := cloud.AutoReplyMessage{PlatformMessageID: "resume", Direction: "candidate", MessageType: "resume", TextContent: "平台说明", CardContent: json.RawMessage(`{"standalone_resume":true}`)}
	question := cloud.AutoReplyMessage{PlatformMessageID: "question", Direction: "candidate", MessageType: "text", TextContent: "工作地点在哪？"}
	oldReply := cloud.AutoReplyMessage{Direction: "self", MessageType: "text", TextContent: "工作地点在成都。"}
	matched := []cloud.CandidateConfirmationItem{{Status: "matched", Content: "成都"}}
	unmarked := card
	unmarked.CardContent = json.RawMessage(`{"summary":"附件简历"}`)
	observed := model.AutoReplyConversationSnapshot{Messages: []model.ConversationMessage{{PlatformMessageID: "resume", Direction: "candidate", MessageType: "resume", TextContent: card.TextContent, CardContent: card.CardContent}}}
	for _, tc := range []struct {
		name  string
		input ReplyContext
		want  bool
	}{
		{"已匹配地点", ReplyContext{PendingMessages: []cloud.AutoReplyMessage{card}, ConfirmationItems: matched}, true},
		{"已有答疑", ReplyContext{PendingMessages: []cloud.AutoReplyMessage{card}, Messages: []cloud.AutoReplyMessage{question, oldReply, card}}, true},
		{"并发问题", ReplyContext{PendingMessages: []cloud.AutoReplyMessage{card, question}, ConfirmationItems: matched}, false},
		{"已有地点但卡内有留言", ReplyContext{PendingMessages: []cloud.AutoReplyMessage{unmarked}, ConfirmationItems: matched}, false},
		{"首次来信有未核实条件", ReplyContext{PendingMessages: []cloud.AutoReplyMessage{card}, ConfirmationItems: []cloud.CandidateConfirmationItem{{Status: "pending"}}}, false},
		{"旧卡由页面核实", ReplyContext{PendingMessages: []cloud.AutoReplyMessage{unmarked}, ConfirmationItems: matched, PageSnapshot: observed}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canAcknowledgeResumeCard(tc.input); got != tc.want {
				t.Fatalf("固定签收=%t want=%t", got, tc.want)
			}
		})
	}
}

// TestStandaloneResumeReceiptKeepsResumeReview 验证先前已答过问题时只签收新卡一次，之后仍结构化、复核条件并交给后台推荐。
func TestStandaloneResumeReceiptKeepsResumeReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/positions/position/snapshot" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"position":{"id":"position"},"config":{"position_id":"position","enabled":true,"version":1},"company_profile":{"id":"company"},"subscription":{"active":true,"allow_auto_reply":true}}`))
	}))
	defer server.Close()
	flow, prepared := newReliabilityFlow(t, server.URL)
	worker := &resumeRetryResponder{}
	flow.Responder = worker
	ctx := context.Background()
	saved, err := flow.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), cloud.AutoReplyConversation{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread", CandidateName: "合成人选", PositionID: "position", PagePositionText: "工程师"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = flow.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{ConversationID: saved.ID, Messages: []cloud.AutoReplyMessage{
		{PlatformMessageID: "old-q", Direction: "candidate", MessageType: "text", TextContent: "工作地点在哪里？"},
		{PlatformMessageID: "old-answer", Direction: "self", MessageType: "text", TextContent: "工作地点在成都。"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	card := model.ConversationMessage{PlatformMessageID: "new-resume", Direction: "candidate", MessageType: "resume", TextContent: "这是我的简历，合适的话可以随时联系我～", CardContent: json.RawMessage(`{"summary":"合成人选的简历","standalone_resume":true}`)}
	attachment := filepath.Join(t.TempDir(), "synthetic.txt")
	if err = os.WriteFile(attachment, []byte("五年教学经验"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime := &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{latestBefore: card, opened: model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformThreadID: "thread", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{card}, ResumeCardAvailable: true, ResumeSourceMessageID: card.PlatformMessageID}, resumeBundle: model.AutoReplyResumeBundle{OnlineResumeText: "五年教学经验", AttachmentPaths: []string{attachment}, ResumeSourceMessageID: card.PlatformMessageID}}}
	position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{Version: 1, Conditions: []cloud.PositionReplyCondition{{ID: "experience", Type: "required", Content: "五年教学经验", Enabled: true}}}}
	conversation := model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "合成人选", CommunicationPosition: "工程师"}
	if err = flow.processConversation(ctx, prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, conversation, nil, &shared.Stats{}); err != nil {
		t.Fatal(err)
	}
	if worker.answers != 0 || runtime.resumeCollects != 0 || len(runtime.replies) != 1 || runtime.replies[0] != "简历收到了，谢谢你发过来。" {
		t.Fatalf("签收不应重复问答或抢先采集：answers=%d collects=%d replies=%v", worker.answers, runtime.resumeCollects, runtime.replies)
	}
	firstReceipts, err := flow.Cloud.PendingAutoReplyProposals(ctx, credentials(prepared))
	if err != nil || len(firstReceipts) != 1 {
		t.Fatal("签收决策未单独留账", firstReceipts, err)
	}
	if err = flow.Cloud.CompleteAutoReplyProposal(ctx, credentials(prepared), firstReceipts[0].ID); err != nil {
		t.Fatal(err)
	}
	processed, _, err := flow.processCheckpoint(ctx, prepared, unreadScannerStub{}, runtime, []cloud.AutoReplyPositionSnapshot{position}, 1, &shared.Stats{}, &shared.ConsecutiveErrorPolicy{}, nil)
	if err != nil || processed != 1 || worker.structures != 1 || worker.reviews != 1 || runtime.resumeCollects != 1 || len(runtime.replies) != 1 {
		t.Fatalf("采集和推荐路径不完整或重复发信：processed=%d structure=%d review=%d collects=%d replies=%v err=%v", processed, worker.structures, worker.reviews, runtime.resumeCollects, runtime.replies, err)
	}
	receipts, err := flow.Cloud.PendingAutoReplyProposals(ctx, credentials(prepared))
	if err != nil || len(receipts) != 1 || receipts[0].ID == firstReceipts[0].ID || receipts[0].Conversation.ResumeRetry != nil {
		t.Fatal("补审未留下独立的后台推荐收据或简历待办未完成", receipts, err)
	}

}
