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

// TestResumeFailureAnswersThenRecoversWithoutNewMessage 验证读取附件失败仍回答岗位问题，重启后的到期待办只补审不重发。
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
	if err := flow.processConversation(context.Background(), prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, conversation, nil, stats); err != nil {
		t.Fatal(err)
	}
	lookup := cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"}
	state, err := flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), lookup)
	if err != nil || state.Conversation == nil || state.Conversation.ResumeRetry == nil || state.Conversation.ResumeRetry.Attempts != 1 || !strings.Contains(state.Conversation.ResumeRetry.LastError, "CLICK_FAILED") {
		t.Fatalf("简历失败未持久留待办：state=%+v err=%v", state.Conversation, err)
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
	if _, qualified := recommendationSource(position, *state.Candidate, items); qualified {
		t.Fatal("未结构化资料不应生成报告")
	}
	advanceResumeRetryFixture(t, flow, prepared, state.Conversation.ID)
	path := filepath.Join(t.TempDir(), "synthetic-resume.txt")
	if err = os.WriteFile(path, []byte("五年教学经验"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime.resumeErr = nil
	runtime.resumeBundle = model.AutoReplyResumeBundle{OnlineResumeText: "五年教学经验", AttachmentPaths: []string{path}, ResumeSourceMessageID: card.PlatformMessageID}
	prepared.Request.TaskID = "restarted-task"
	processed, _, err := flow.processCheckpoint(context.Background(), prepared, unreadScannerStub{}, runtime, []cloud.AutoReplyPositionSnapshot{position}, 1, stats, &shared.ConsecutiveErrorPolicy{}, nil)
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
	if _, qualified := recommendationSource(position, *state.Candidate, items); !qualified {
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
	if err != nil || state.Conversation.ResumeRetry == nil || state.Conversation.ResumeRetry.SourceMessageID != card.PlatformMessageID || state.Conversation.ResumeRetry.Attempts != 1 || len(runtime.replies) != 2 || responder.firstInput.CandidateState.Candidate.ResumeStructured {
		t.Fatalf("新来源被旧结构化资料绕过：retry=%+v replies=%v err=%v", state.Conversation.ResumeRetry, runtime.replies, err)
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
