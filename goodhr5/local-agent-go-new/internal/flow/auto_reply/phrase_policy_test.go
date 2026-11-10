// Package auto_reply 本文件用合成工具调用和真实临时事务验证空场景静默、资料目标继续处理、原生资格独立及重复轮询防重。
package auto_reply

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestEmptyPhrasePolicyKeepsModernSilenceAndLegacyDefaults 验证现代空正文、移除和删除不恢复默认，旧无库协议继续兼容。
func TestEmptyPhrasePolicyKeepsModernSilenceAndLegacyDefaults(t *testing.T) {
	legacy := cloud.PositionAutoReplyConfig{}
	if silentReplyScene(legacy, "receive_resume") || fixedReplyPhrase(legacy, "receive_resume") == "" {
		t.Fatal("旧服务无话术库时的兼容签收被改动")
	}
	modern := cloud.PositionAutoReplyConfig{PhrasesVersion: "v1", Phrases: []cloud.AutoReplyPhrase{{ID: "request_resume", Content: " \n "}, {ID: "greeting", Content: "旧正文不可恢复", Deleted: true}}}
	for _, id := range []string{"request_resume", "greeting", "receive_resume", "job_removed"} {
		if !silentReplyScene(modern, id) || fixedReplyPhrase(modern, id) != "" {
			t.Fatal("现代空、删除或移除的场景恢复了默认", id)
		}
	}
	projected := positionForPrompt(cloud.AutoReplyPositionSnapshot{Config: modern})
	raw, _ := json.Marshal(projected)
	if strings.Contains(string(raw), "旧正文不可恢复") || !strings.Contains(string(raw), `"id":"greeting"`) || !strings.Contains(string(raw), `"deleted":true`) {
		t.Fatal("模型未保留删除场景禁发元数据或仍收到旧正文")
	}
}

// TestSendToolEmptySceneCompletesWithoutRewrite 验证程序可判的拒绝、问候和显式自定义场景都静默，混合批次保留独立有据问答。
func TestSendToolEmptySceneCompletesWithoutRewrite(t *testing.T) {
	for _, mode := range []string{"end", "greeting", "resume_receipt", "wechat_receipt", "custom", "deleted", "mixed", "mixed_decline_question", "normal", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			input := responderInput("")
			input.Position.Config.PhrasesVersion = "modern"
			input.Position.Config.Phrases = []cloud.AutoReplyPhrase{{ID: "end_conversation"}, {ID: "greeting"}, {ID: "job_custom", Title: "自定义静默", Scene: "特定场景"}, {ID: "job_deleted", Deleted: true, Content: "旧话术"}}
			inbound := cloud.AutoReplyMessage{Fingerprint: "message-1", Direction: "candidate", MessageType: "text", TextContent: "工作地点在哪？"}
			messages := []ReplyMessage{{Type: "answer", Content: "模型改写的被禁场景", PhraseID: "job_custom"}}
			want := 0
			switch mode {
			case "end":
				inbound.TextContent = "暂时不考虑了"
				messages[0].PhraseID = ""
			case "greeting":
				inbound.TextContent = "你好呀"
				messages[0].PhraseID = ""
			case "resume_receipt":
				inbound.MessageType, inbound.TextContent = "resume", "本轮附件.pdf"
				inbound.CardContent = json.RawMessage(`{"standalone_resume":true}`)
				messages[0].PhraseID = ""
			case "wechat_receipt":
				messages[0].PhraseID = "receive_wechat"
			case "deleted":
				messages[0].PhraseID = "job_deleted"
			case "mixed":
				messages = []ReplyMessage{{Type: "answer", Content: "工作地点在成都。"}, {Type: "confirmation", PhraseID: "job_custom", Content: "换一种说法仍想发"}}
				want = 1
			case "mixed_decline_question":
				inbound.TextContent = "暂时不考虑了，但请说一下工作地址"
				messages = []ReplyMessage{{Type: "answer", Content: "工作地点在成都。"}}
				want = 1
			case "normal", "legacy":
				messages = []ReplyMessage{{Type: "answer", Content: "工作地点在成都。"}}
				want = 1
				if mode == "legacy" {
					input.Position.Config.Phrases, input.Position.Config.PhrasesVersion = nil, ""
				}
			}
			input.PendingMessages, input.Messages = []cloud.AutoReplyMessage{inbound}, []cloud.AutoReplyMessage{inbound}
			s := &toolExecutionState{input: input}
			raw, _ := json.Marshal(sendMessagesToolArgs{Messages: messages})
			_, err := s.prepareMessages(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: string(raw)}})
			if err != nil || !s.messagesPrepared || len(s.pendingMessages) != want || (want == 0 && !s.textSuppressed) {
				t.Fatalf("空场景被改写或独立问题被吞：count=%d silent=%v err=%v", len(s.pendingMessages), s.textSuppressed, err)
			}
		})
	}
}

// TestAIEmptySceneFinishesOneModelCall 验证合法空场景工具结果直接结束，不报参数错误或再次请求模型补发。
func TestAIEmptySceneFinishesOneModelCall(t *testing.T) {
	state := &responderTestState{aiResponses: []string{toolCallResponse("silent", toolSendMessages, `{"messages":[{"type":"answer","phrase_id":"job_custom","content":"","requests_resume":false}]}`, 1)}}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()
	input := responderInput(server.URL)
	input.PendingMessages = input.Messages
	input.Position.Config.PhrasesVersion = "v1"
	input.Position.Config.Phrases = []cloud.AutoReplyPhrase{{ID: "job_custom", Title: "静默", Scene: "特定场景", Content: ""}}
	decision, err := (&AIResponder{AI: newAIClient(), Cloud: cloud.New(server.URL)}).Reply(context.Background(), input)
	if err != nil || !decision.TextSuppressed || len(decision.Messages) != 0 || decision.ManualReason != "" || state.chatCallNumber != 1 {
		t.Fatalf("静默被变成失败、人工接管或额外AI请求：calls=%d silent=%v err=%v", state.chatCallNumber, decision.TextSuppressed, err)
	}
}

// TestCompletedSilentRunReportsNoManualHandoff 验证已持久化的静默结果展示完成态，不产生空转人工或额外通知，真实人工原因和普通回复仍保持原样。
func TestCompletedSilentRunReportsNoManualHandoff(t *testing.T) {
	for _, tc := range []struct {
		name, stage, reason string
		decision            ReplyDecision
	}{
		{"silent", "silent", "本场景按配置不发文字，已完成处理", ReplyDecision{TextSuppressed: true}},
		{"silent_whitespace_reason", "silent", "本场景按配置不发文字，已完成处理", ReplyDecision{TextSuppressed: true, ManualReason: " \n "}},
		{"real_manual", "manual", "转人工：岗位资料需要HR确认", ReplyDecision{TextSuppressed: true, ManualReason: "岗位资料需要HR确认"}},
		{"ordinary_manual", "manual", "转人工：原有人工原因", ReplyDecision{ManualReason: "原有人工原因"}},
		{"mixed_reply", "reply", "AI 准备回复：工作地点在成都。", ReplyDecision{TextSuppressed: true, Messages: []ReplyMessage{{Type: "answer", Content: "工作地点在成都。"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &responderTestState{}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests++
				if req.URL.Path != "/api/auto-reply/agent/ai-runs/finish" {
					t.Error("结果展示不应新增通知或AI请求", req.URL.Path)
					http.NotFound(w, req)
					return
				}
				state.serveHTTP(w, req)
			}))
			defer server.Close()
			progress := &publicationProgress{}
			responder := &AIResponder{Cloud: cloud.New(server.URL), Logger: progress}
			input := responderInput(server.URL)
			run := cloud.AutoReplyAIRun{ID: "run", ConversationID: input.Conversation.ID, PositionID: input.Position.Position.ID, TraceID: "test-run", BasedOnMessageKey: input.BasedOnMessageKey}
			decision, err := responder.completeRun(input, run, nil, 1, tc.decision)
			if err != nil || requests != 1 || len(state.finishedRuns) != 1 || state.finishedRuns[0].Status != "completed" {
				t.Fatal("展示分支改变了总记录完成或产生额外请求", requests, err)
			}
			result := progress.AnalysisStatus("")
			if result == nil || !result.Terminal || result.Phase != "result" || result.Stage != tc.stage || result.Reason != tc.reason || decision.ManualReason != tc.decision.ManualReason {
				t.Fatal("静默或真实人工结果展示错误", result)
			}
			var output aiReplyAuditOutput
			if err := json.Unmarshal(state.finishedRuns[0].OutputMessage, &output); err != nil || output.Decision.TextSuppressed != tc.decision.TextSuppressed || output.Decision.ManualReason != tc.decision.ManualReason {
				t.Fatal("展示修复改变了真实审计决定", err)
			}
		})
	}
}

// silentSceneResponder 在编排测试中模拟自由改写和有真实反证的HR必要条件判断，内部目标轮不产生新答复。
type silentSceneResponder struct {
	mode  string
	calls int
}

// Reply 仅返回本用例明确的提议，不连接真实模型，也不直接写候选人或发送消息。
func (r *silentSceneResponder) Reply(_ context.Context, input ReplyContext) (ReplyDecision, error) {
	r.calls++
	if input.ConditionsOnly {
		return ReplyDecision{}, nil
	}
	decision := ReplyDecision{Messages: []ReplyMessage{{Type: "answer", Content: "模型自行写的一段拒绝"}}}
	switch r.mode {
	case "custom":
		decision.Messages[0].PhraseID = "job_custom"
	case "normal":
		decision.Messages[0].Content = "工作地点在成都。"
	case "mismatch":
		for _, item := range input.ConfirmationItems {
			if item.PositionConditionID == "certificate" {
				item.Status, item.StatusReason, item.SourceRef, item.EvidenceText = "unmatched", "候选人明确未取得", input.BasedOnMessageKey, "我没有岗位资格证"
				decision.ConfirmationUpdates = append(decision.ConfirmationUpdates, item)
			}
		}
	}
	return decision, nil
}

// TestSilentSceneFlowCompletesAndKeepsFacts 验证静默拒绝和自定义场景会正常推进水位，岗位反证仍保存，普通有据问题继续回复且不重复处理。
func TestSilentSceneFlowCompletesAndKeepsFacts(t *testing.T) {
	for _, mode := range []string{"end", "custom", "mismatch", "normal"} {
		t.Run(mode, func(t *testing.T) {
			position := cloud.AutoReplyPositionSnapshot{OK: true, Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{PositionID: "position", Enabled: true, Version: 1, PhrasesVersion: "v1", PositionDescription: "工作地点成都。", Phrases: []cloud.AutoReplyPhrase{{ID: "end_conversation"}, {ID: "required_mismatch"}, {ID: "job_custom"}}}, CompanyProfile: cloud.CompanyProfile{ID: "company"}, Subscription: cloud.AutoReplySubscription{Active: true, AllowAutoReply: true}}
			text := "暂时不考虑了"
			if mode == "normal" || mode == "custom" {
				text = "工作地点在哪？"
			} else if mode == "mismatch" {
				text = "我没有岗位资格证"
				position.Config.Conditions = []cloud.PositionReplyCondition{{ID: "certificate", Type: "required", Content: "已持岗位资格证", Enabled: true}}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(position) }))
			defer server.Close()
			flow, prepared := newReliabilityFlow(t, server.URL)
			progress := &publicationProgress{}
			flow.Logger = progress
			responder := &silentSceneResponder{mode: mode}
			flow.Responder = responder
			message := model.ConversationMessage{PlatformMessageID: "source", Direction: "candidate", MessageType: "text", TextContent: text}
			page := model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformThreadID: "thread", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{message}}
			runtime := &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{opened: page, latestBefore: message}}
			item := model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: page.CandidateName, CommunicationPosition: "工程师"}
			stats := &shared.Stats{}
			if err := flow.processConversation(context.Background(), prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, item, nil, stats); err != nil {
				t.Fatal(err)
			}
			want := 0
			if mode == "normal" {
				want = 1
			}
			state, err := flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"})
			if err != nil || state.Candidate == nil || len(runtime.replies) != want {
				t.Fatalf("文字门禁影响建档或误发：replies=%d err=%v", len(runtime.replies), err)
			}
			pending, err := flow.Cloud.PendingAutoReplyMessages(context.Background(), credentials(prepared), state.Conversation.ID)
			if err != nil || len(pending) != 0 {
				t.Fatal("静默没有推进处理水位", len(pending), err)
			}
			if mode != "normal" {
				result := progress.AnalysisStatus("")
				if result == nil || !result.Terminal || result.Phase != "result" || result.Stage != "silent" || result.Reason != "按话术设置，本轮不发文字" {
					t.Fatal("静默或必要条件反证没有最终正常完成展示", result)
				}
			}
			if mode == "mismatch" {
				goals, err := flow.Cloud.AutoReplyConfirmationItems(context.Background(), credentials(prepared), state.Conversation.ID)
				if err != nil || len(goals) != 1 || goals[0].Status != "unmatched" || goals[0].EvidenceText == "" {
					t.Fatal("静默丢失真实条件反证", err)
				}
			}
			calls := responder.calls
			if _, _, err := flow.processCheckpoint(context.Background(), prepared, unreadScannerStub{}, runtime, []cloud.AutoReplyPositionSnapshot{position}, 1, stats, &shared.ConsecutiveErrorPolicy{}, nil); err != nil || responder.calls != calls || len(runtime.replies) != want {
				t.Fatal("静默批次在后续扫描重复调用AI或发送", err)
			}
		})
	}
}

// TestEmptyPhoneReceiptStillSavesContactAndGoal 验证纯手机号不发签收，但真实号码和动态目标继续保存，独立目标可按原规则追问。
func TestEmptyPhoneReceiptStillSavesContactAndGoal(t *testing.T) {
	position := cloud.AutoReplyPositionSnapshot{OK: true, Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{PositionID: "position", Enabled: true, Version: 1, PhrasesVersion: "v1", PositionDescription: "安排沟通前了解候选人方便通话的时间段。", Phrases: []cloud.AutoReplyPhrase{{ID: "receive_phone", Content: ""}}}, CompanyProfile: cloud.CompanyProfile{ID: "company"}, Subscription: cloud.AutoReplySubscription{Active: true, AllowAutoReply: true}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(position) }))
	defer server.Close()
	flow, prepared := newReliabilityFlow(t, server.URL)
	flow.Responder = &goalPlanningResponder{}
	message := model.ConversationMessage{PlatformMessageID: "phone-source", Direction: "candidate", MessageType: "text", TextContent: "13800138000"}
	runtime := &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{latestBefore: message, opened: model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformThreadID: "thread", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{message}}}}
	if err := flow.processConversation(context.Background(), prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "合成人选", CommunicationPosition: "工程师"}, nil, &shared.Stats{}); err != nil {
		t.Fatal(err)
	}
	state, err := flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"})
	if err != nil || state.Candidate == nil || state.Candidate.Phone == "" || len(runtime.replies) != 1 || runtime.replies[0] != "你方便什么时间通话？" {
		t.Fatal("静默签收影响资料保存或多发签收", err, len(runtime.replies))
	}
	goals, err := flow.Cloud.AutoReplyConfirmationItems(context.Background(), credentials(prepared), state.Conversation.ID)
	if err != nil || len(goals) != 1 || goals[0].AskCount != 1 {
		t.Fatal("资料收讫后的目标进度未正常保存", err)
	}
}

// TestNativeRequestIgnoresEmptyTextButKeepsQualification 验证索号文字留空不禁用已明确需要的原生动作，已得或被拒绝仍禁止。
func TestNativeRequestIgnoresEmptyTextButKeepsQualification(t *testing.T) {
	input := proposalState().input
	input.NativeInfoRequests = true
	input.Position.Config.PhrasesVersion = "v1"
	input.Position.Config.Phrases = []cloud.AutoReplyPhrase{{ID: "request_phone", Content: ""}}
	request := model.CandidateInfoRequest{RequestPhone: true}
	answer := []ReplyMessage{{Type: "answer", Content: "工作地点在成都。"}}
	if err := validateNativeInfoProposal(input, request, answer); err != nil {
		t.Fatal("空文字错误关闭原生资格", err)
	}
	for _, state := range []infoContactState{{Received: true}, {Asked: true}, {Declined: true}} {
		input.InfoContact = map[string]infoContactState{"phone": state}
		if validateNativeInfoProposal(input, request, answer) == nil {
			t.Fatal("空文字修正绕过已得、已问或拒绝防重")
		}
	}
}

// TestEmptyRequestTextStillExecutesNativeOnce 验证已送达答案和资料need成立时，空文字不阻断原生请求且不重复点击。
func TestEmptyRequestTextStillExecutesNativeOnce(t *testing.T) {
	f, p, pos, conv, snapshot, state, runtime := nativeRequestFixture(t, []cloud.AutoReplyPhrase{{ID: "request_phone", Content: ""}})
	request := model.CandidateInfoRequest{RequestPhone: true}
	for i := 0; i < 2; i++ {
		if _, err := f.requestNativeInfoAfterAnswer(context.Background(), p, runtime, pos, &conv, snapshot, state, "in-1", runtime.latestBefore.TextContent, request); err != nil {
			t.Fatal(err)
		}
	}
	if len(runtime.calls) != 1 || runtime.calls[0] != "phone" || runtime.sent {
		t.Fatal("空文字停掉原生动作、补发文字或绕过防重", len(runtime.calls))
	}
}

// TestLegacyNativeResumeRequestIgnoresEmptyText 验证旧平台原生简历按钮仍可用，明确不可用时空备用文字不发且不误记等待成功。
func TestLegacyNativeResumeRequestIgnoresEmptyText(t *testing.T) {
	for _, mode := range []string{"requested", "unavailable", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			f, p, pos, conv, snapshot, state, runtime := nativeRequestFixture(t, []cloud.AutoReplyPhrase{{ID: "request_resume", Content: ""}})
			p.Platform.Behavior.AutoReplyNativeInfoRequests = false
			previousStatus := conv.Status
			if mode == "unavailable" {
				runtime.resumeRequestErr = model.ErrAutoReplyResumeRequestUnavailable
			} else if mode == "unknown" {
				runtime.resumeRequestErr = errors.New("原生回执未知")
			}
			err := f.requestResumeAfterAnswer(context.Background(), p, runtime, pos, &conv, snapshot, state, "in-1", runtime.latestBefore.TextContent, false)
			if (err != nil) != (mode == "unknown") || runtime.resumeRequests != 1 || runtime.sent {
				t.Fatal("空文字阻断原生索简历或被备用文案补发", runtime.resumeRequests, err)
			}
			wantStatus := "waiting_resume"
			if mode == "unavailable" {
				wantStatus = previousStatus
			}
			if conv.Status != wantStatus {
				t.Fatal("未执行的原生动作误记等待，或未知状态被释放", conv.Status)
			}
			if mode == "requested" {
				if err = f.requestResumeAfterAnswer(context.Background(), p, runtime, pos, &conv, snapshot, state, "in-1", runtime.latestBefore.TextContent, false); err != nil || runtime.resumeRequests != 1 {
					t.Fatal("空文字修正破坏原生请求防重", err)
				}
			}
		})
	}
}
