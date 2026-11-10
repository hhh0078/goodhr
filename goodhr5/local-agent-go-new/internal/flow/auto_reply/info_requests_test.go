// Package auto_reply 本文件用真实临时发送账本验证原生资料请求的先答后索、明确暂缓、未知防重与插话安全，不操作真实账号。
package auto_reply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type nativeRequestRuntime struct {
	*autoReplyRuntimeStub
	calls      []string
	anchors    []string
	result     model.AutoReplyInfoRequestResult
	results    map[string]model.AutoReplyInfoRequestResult
	requestErr error
}

// RequestAutoReplyCandidateInfo 记录动作类型与真实答案锚点，系统回执不会改变最后一条双方消息。
func (r *nativeRequestRuntime) RequestAutoReplyCandidateInfo(_ context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot, request model.CandidateInfoRequest) (model.AutoReplyInfoRequestResult, error) {
	r.calls = append(r.calls, model.CandidateInfoRequestKind(request))
	r.anchors = append(r.anchors, snapshot.ExpectedSendMessageKey)
	if result, ok := r.results[model.CandidateInfoRequestKind(request)]; ok {
		return result, r.requestErr
	}
	return r.result, r.requestErr
}

// nativeRequestFixture 创建已实际答复一次的独立会话与三个动态场景话术，不调用AI或招聘浏览器。
func nativeRequestFixture(t *testing.T) (*Flow, shared.PreparedTask, cloud.AutoReplyPositionSnapshot, cloud.AutoReplyConversation, model.AutoReplyConversationSnapshot, cloud.AutoReplyCandidateState, *nativeRequestRuntime) {
	t.Helper()
	position := cloud.AutoReplyPositionSnapshot{OK: true, Position: cloud.AutoReplyPosition{ID: "position"}, Config: cloud.PositionAutoReplyConfig{PositionID: "position", Enabled: true, Version: 1, PhrasesVersion: "native", Phrases: []cloud.AutoReplyPhrase{{ID: "request_resume", Content: "方便发一份简历吗？"}, {ID: "request_phone", Content: "方便留个手机号吗？"}, {ID: "request_wechat", Content: "方便交换微信吗？"}}}, CompanyProfile: cloud.CompanyProfile{ID: "company"}, Subscription: cloud.AutoReplySubscription{Active: true, AllowAutoReply: true}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(position)
	}))
	t.Cleanup(server.Close)
	flow, prepared := newReliabilityFlow(t, server.URL)
	prepared.Platform.Behavior.AutoReplyNativeInfoRequests = true
	ctx := context.Background()
	conversation, err := flow.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), cloud.AutoReplyConversation{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread", CandidateName: "测试候选人", PositionID: "position", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = flow.Cloud.EnsureLocalAutoReplyCandidate(ctx, credentials(prepared), conversation); err != nil {
		t.Fatal(err)
	}
	answer := model.ConversationMessage{PlatformMessageID: "answer-1", Direction: "self", MessageType: "text", TextContent: "工作地点在成都。"}
	if _, err = flow.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: []cloud.AutoReplyMessage{{PlatformMessageID: "in-1", Direction: "candidate", MessageType: "text", TextContent: "在哪里上班？"}, {PlatformMessageID: answer.PlatformMessageID, Direction: "self", MessageType: "text", TextContent: answer.TextContent}}}); err != nil {
		t.Fatal(err)
	}
	state, err := flow.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"})
	if err != nil {
		t.Fatal(err)
	}
	conversation = *state.Conversation
	snapshot := model.AutoReplyConversationSnapshot{CandidateName: conversation.CandidateName, PlatformThreadID: "thread", PlatformCandidateID: "page-id", PlatformAccountID: prepared.Request.PlatformAccountID}
	runtime := &nativeRequestRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{latestBefore: answer}, result: model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoRequested, EvidenceMessageID: "system-info-receipt"}}
	return flow, prepared, position, conversation, snapshot, state, runtime
}

// TestReceivedSameContactSettlesUnknownProgress 验证已知同值不能挡住新原生卡把未知交换结果收敛为已收到。
func TestReceivedSameContactSettlesUnknownProgress(t *testing.T) {
	f, p, _, conv, snapshot, state, _ := nativeRequestFixture(t)
	ctx := context.Background()
	if err := f.Cloud.SetAutoReplyInfoRequestProgress(ctx, credentials(p), conv.ID, "phone", "unknown", "in-1"); err != nil {
		t.Fatal(err)
	}
	conv, _, _ = f.Cloud.AutoReplyAnswerConversation(ctx, credentials(p), conv.ID)
	state.Candidate.Phone = "13800138001"
	snapshot.Messages = []model.ConversationMessage{{Key: "received-card", Direction: "system", MessageType: "system", CardContent: json.RawMessage(`{"info_type":"phone","info_status":"received","phone":"13800138001"}`)}}
	updated, err := f.syncNativeChatContacts(ctx, p, conv, snapshot, state)
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := f.Cloud.AutoReplyAnswerConversation(ctx, credentials(p), conv.ID)
	if err != nil || updated.Candidate == nil || updated.Candidate.Phone != "13800138001" || current.InfoRequests["phone"].Status != model.AutoReplyInfoReceived {
		t.Fatal("相同号码使未知交换结果一直悬空", err)
	}
}

// TestNativeInfoEvidenceRejectsMismatchedReceipt 验证错来源、错种类、错状态或冲突值不能混入真实资料回执，泛申请仍保留兼容。
func TestNativeInfoEvidenceRejectsMismatchedReceipt(t *testing.T) {
	for _, tc := range []struct {
		name, kind, status, key, card, phone string
		want                                 bool
	}{
		{"真实手机号", "phone", "received", "card", `{"info_type":"phone","info_status":"received","phone":"13800138001"}`, "13800138001", true},
		{"错来源", "phone", "received", "other", `{"info_type":"phone","info_status":"received","phone":"13800138001"}`, "13800138001", false},
		{"错状态", "phone", "received", "card", `{"info_type":"phone","info_status":"requested","phone":"13800138001"}`, "13800138001", false},
		{"冲突值", "phone", "received", "card", `{"info_type":"phone","info_status":"received","phone":"13800138002"}`, "13800138001", false},
		{"错种类", "phone", "received", "card", `{"info_type":"wechat","info_status":"received","phone":"13800138001"}`, "13800138001", false},
		{"本次泛申请", "wechat", "requested", "card", `{"info_type":"contact","info_status":"requested"}`, "", true},
		{"旧申请回执", "resume", "already_requested", "card", `{"info_type":"resume","info_status":"requested"}`, "", true},
		{"泛申请不是简历", "resume", "requested", "card", `{"info_type":"contact","info_status":"requested"}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := model.AutoReplyInfoRequestResult{Status: tc.status, EvidenceMessageID: "card", Phone: tc.phone, EvidenceMessage: &model.ConversationMessage{Key: tc.key, Direction: "system", MessageType: "system", CardContent: json.RawMessage(tc.card)}}
			if got := nativeInfoEvidenceMatches(tc.kind, result); got != tc.want {
				t.Fatal("回执来源核验与预期不同", got)
			}
		})
	}
}

// TestNativeInfoDeferredSurvivesNextMessage 验证明确定未执行不记waiting、不留未知账本，同一入站不重复尝试，新入站才重新评估。
func TestNativeInfoDeferredSurvivesNextMessage(t *testing.T) {
	f, p, pos, conv, snapshot, state, r := nativeRequestFixture(t)
	ctx := context.Background()
	r.result.Status = model.AutoReplyInfoDeferred
	request := model.CandidateInfoRequest{RequestResume: true}
	if _, err := f.requestNativeInfoAfterAnswer(ctx, p, r, pos, &conv, snapshot, state, "in-1", r.latestBefore.TextContent, request); err != nil {
		t.Fatal(err)
	}
	current, _, _ := f.Cloud.AutoReplyAnswerConversation(ctx, credentials(p), conv.ID)
	if current.Status == "waiting_resume" || current.InfoRequests["resume"].Status != model.AutoReplyInfoDeferred || len(r.calls) != 1 {
		t.Fatal("未执行的资料请求被误记成功")
	}
	if status, _, _ := f.Cloud.AutoReplySendReceipt(ctx, credentials(p), conv.ID, "info-request:resume", 0); status != "" {
		t.Fatal("暂缓留下不可恢复账本", status)
	}
	if _, err := f.requestNativeInfoAfterAnswer(ctx, p, r, pos, &conv, snapshot, state, "in-1", r.latestBefore.TextContent, model.CandidateInfoRequest{}); err != nil || len(r.calls) != 1 {
		t.Fatal("同一来信重复请求", err)
	}
	_, err := f.Cloud.SyncAutoReplyMessages(ctx, credentials(p), cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: []cloud.AutoReplyMessage{{PlatformMessageID: "in-2", Direction: "candidate", MessageType: "text", TextContent: "好的"}, {PlatformMessageID: "answer-2", Direction: "self", MessageType: "text", TextContent: "可以的。"}}})
	if err != nil {
		t.Fatal(err)
	}
	r.latestBefore = model.ConversationMessage{PlatformMessageID: "answer-2", Direction: "self", MessageType: "text", TextContent: "可以的。"}
	r.result.Status = model.AutoReplyInfoRequested
	if _, err = f.requestNativeInfoAfterAnswer(ctx, p, r, pos, &conv, snapshot, state, "in-2", r.latestBefore.TextContent, model.CandidateInfoRequest{}); err != nil || len(r.calls) != 2 {
		t.Fatal("新沟通没有恢复暂缓请求", err)
	}
}

// TestNativeInfoUnknownAndInterruptNeverRepeat 验证点击结果未知不补点，明确发送前插话只暂缓，普通答案已完成不会重答。
func TestNativeInfoUnknownAndInterruptNeverRepeat(t *testing.T) {
	for _, beforeSend := range []bool{false, true} {
		t.Run(fmt.Sprint(beforeSend), func(t *testing.T) {
			f, p, pos, conv, snapshot, state, r := nativeRequestFixture(t)
			ctx := context.Background()
			r.requestErr = errors.New("模拟回执断开")
			if beforeSend {
				r.requestErr = model.ErrAutoReplySupersededBeforeSend
			}
			_, err := f.requestNativeInfoAfterAnswer(ctx, p, r, pos, &conv, snapshot, state, "in-1", r.latestBefore.TextContent, model.CandidateInfoRequest{RequestPhone: true})
			if beforeSend && err != nil || !beforeSend && !errors.Is(err, errAutoReplySendNeedsReview) {
				t.Fatal("未知与未发送被混淆", err)
			}
			pending, _ := f.Cloud.PendingAutoReplyMessages(ctx, credentials(p), conv.ID)
			if len(pending) != 0 {
				t.Fatal("已经答复的旧消息会重新进入AI")
			}
			_, _ = f.requestNativeInfoAfterAnswer(ctx, p, r, pos, &conv, snapshot, state, "in-1", r.latestBefore.TextContent, model.CandidateInfoRequest{RequestPhone: true})
			if len(r.calls) != 1 {
				t.Fatal("未知结果或相同插话轮次被重复执行")
			}
		})
	}
}

// TestNativeInfoSystemReceiptKeepsConversationAnchor 验证系统申请回执不冒充HR气泡，多种资料仍复用普通答案锚点且之后不重复请求。
func TestNativeInfoSystemReceiptKeepsConversationAnchor(t *testing.T) {
	f, p, pos, conv, snapshot, state, r := nativeRequestFixture(t)
	ctx := context.Background()
	r.result.EvidenceMessage = &model.ConversationMessage{Key: r.result.EvidenceMessageID, Direction: "system", MessageType: "system", TextContent: "请求交换联系方式已发送", CardContent: json.RawMessage(`{"info_type":"contact","info_status":"requested"}`)}
	resume := model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoRequested, EvidenceMessageID: "resume-receipt", EvidenceMessage: &model.ConversationMessage{Key: "resume-receipt", Direction: "system", MessageType: "card", TextContent: "简历请求已发送", CardContent: json.RawMessage(`{"info_type":"resume","info_status":"requested"}`)}}
	wechat := r.result
	wechat.EvidenceMessageID = "wechat-receipt"
	wechatMessage := *r.result.EvidenceMessage
	wechatMessage.Key = wechat.EvidenceMessageID
	wechat.EvidenceMessage = &wechatMessage
	r.results = map[string]model.AutoReplyInfoRequestResult{"resume": resume, "wechat": wechat}
	request := model.CandidateInfoRequest{RequestResume: true, RequestPhone: true, RequestWechat: true}
	if _, err := f.requestNativeInfoAfterAnswer(ctx, p, r, pos, &conv, snapshot, state, "in-1", r.latestBefore.TextContent, request); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 3 {
		t.Fatal("三类请求没有独立执行", r.calls)
	}
	for _, anchor := range r.anchors {
		if anchor != "answer-1" {
			t.Fatal("系统回执替代了真实沟通锚点", anchor)
		}
	}
	_, _ = f.requestNativeInfoAfterAnswer(ctx, p, r, pos, &conv, snapshot, state, "in-1", r.latestBefore.TextContent, request)
	if len(r.calls) != 3 {
		t.Fatal("已索取的类型再次执行")
	}
	history, err := f.Cloud.AutoReplyMessages(ctx, credentials(p), conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	receipts := 0
	expected := map[string]*model.ConversationMessage{r.result.EvidenceMessageID: r.result.EvidenceMessage, resume.EvidenceMessageID: resume.EvidenceMessage, wechat.EvidenceMessageID: wechat.EvidenceMessage}
	for _, message := range history {
		if original := expected[message.Fingerprint]; original != nil {
			receipts++
			if message.Direction != original.Direction || message.MessageType != original.MessageType || message.TextContent != original.TextContent || string(message.CardContent) != string(original.CardContent) {
				t.Fatal("同步回执时改写了原生消息")
			}
		}
	}
	if receipts != 3 {
		t.Fatal("真实回执未及时入库或重复入库", receipts)
	}
}

// TestNativeProposalCannotUseTextOrKnownContact 绕过旧提示词的索号文字被拒绝，合法结构化提议不触发平台副作用。
func TestNativeProposalCannotUseTextOrKnownContact(t *testing.T) {
	s := proposalState()
	s.input.NativeInfoRequests = true
	s.input.Position.Config.PhrasesVersion = "native"
	s.input.Position.Config.Phrases = []cloud.AutoReplyPhrase{{ID: "request_phone", Content: "方便留个手机号吗？"}}
	raw, _ := json.Marshal(sendMessagesToolArgs{Messages: []ReplyMessage{{Type: "answer", Content: "工作地点在成都。"}}, InfoRequests: model.CandidateInfoRequest{RequestPhone: true}})
	if _, err := s.prepareMessages(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: string(raw)}}); err != nil || !s.infoRequests.RequestPhone {
		t.Fatal("合法原生提议未保留", err)
	}
	if validateNativeInfoProposal(s.input, model.CandidateInfoRequest{}, []ReplyMessage{{Type: "answer", Content: "方便留个手机号吗？"}}) == nil {
		t.Fatal("旧提示词文字绕过了平台条件")
	}
	s.input.PhoneContact.Received = true
	s.input.InfoContact = map[string]infoContactState{"phone": {Received: true}}
	if validateNativeInfoProposal(s.input, model.CandidateInfoRequest{RequestPhone: true}, []ReplyMessage{{Type: "answer", Content: "可以的。"}}) == nil {
		t.Fatal("已经获得的电话被再次提议索要")
	}
}
