// Package auto_reply 本文件验证动态目标增删改、提问顺序及与岗位必要条件的权限边界。
package auto_reply

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// dynamicGoalState 提供没有固定通话条件但岗位说明确实需要沟通时间的合成上下文。
func dynamicGoalState() *toolExecutionState {
	s := proposalState()
	s.input.Position.Config.PositionDescription = "工作地点成都。安排沟通前了解候选人方便通话的时间段。"
	return s
}

type goalPlanningResponder struct {
	resumeRetryResponder
	afterResume  bool
	afterAnswer  bool
	noGoal       bool
	beforeReview func(ReplyContext)
}

// Reply 为编排测试提议一个新目标；可选择只在简历结构化完成后发现这个缺失信息。
func (r *goalPlanningResponder) Reply(_ context.Context, input ReplyContext) (ReplyDecision, error) {
	answer := ReplyMessage{Type: "answer", Content: "可以的，我们继续聊。"}
	if input.ConditionsOnly {
		r.reviews++
		if r.beforeReview != nil {
			r.beforeReview(input)
		}
		if r.noGoal {
			return ReplyDecision{}, nil
		}
	} else {
		r.answers++
	}
	if !input.ConditionsOnly && (r.afterResume || r.afterAnswer) {
		return ReplyDecision{Messages: []ReplyMessage{answer}}, nil
	}
	goal := cloud.CandidateConfirmationItem{ID: "dynamic-call-time", ConversationID: input.Conversation.ID, CandidateID: input.Conversation.CandidateID, PositionID: input.Position.Position.ID, SourceType: "ai", CreatedByKind: "ai", ItemType: "confirm", Content: "方便通话的时间", Status: "pending", StatusReason: "安排沟通前需要了解时间", RequireAbsent: true, AIProposal: true}
	question := ReplyMessage{Type: "confirmation", Content: "你方便什么时间通话？", ConfirmationItemID: goal.ID, GoalContentKey: cloud.AutoReplyGoalContentKey(goal)}
	messages := []ReplyMessage{answer, question}
	if input.ConditionsOnly {
		messages = []ReplyMessage{question}
	}
	return ReplyDecision{Messages: messages, ConfirmationUpdates: []cloud.CandidateConfirmationItem{goal}}, nil
}

// TestAnswerOnlyGetsOneSeparateGoalReview 验证首答先送达，再单独检查目标；无任务、空闲轮询和同事件重试都不多收费。
func TestAnswerOnlyGetsOneSeparateGoalReview(t *testing.T) {
	for _, noGoal := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true,"position":{"id":"position"},"config":{"position_id":"position","enabled":true,"version":1},"company_profile":{"id":"company"},"subscription":{"active":true,"allow_auto_reply":true}}`))
		}))
		flow, prepared := newReliabilityFlow(t, server.URL)
		message := model.ConversationMessage{PlatformMessageID: "m1", Direction: "candidate", MessageType: "text", TextContent: "我有兴趣，办公地址在哪？"}
		page := model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformThreadID: "thread", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{message}}
		runtime := &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{opened: page, latestBefore: message}}
		responder := &goalPlanningResponder{afterAnswer: true, noGoal: noGoal, beforeReview: func(input ReplyContext) {
			if len(runtime.replies) != 1 || !input.MessageFollowup || len(input.PendingMessages) != 0 {
				t.Fatal("目标复核阻塞了首答或仍可补答历史", runtime.replies)
			}
		}}
		flow.Responder = responder
		position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{Version: 1, Phrases: []cloud.AutoReplyPhrase{}, PositionDescription: "正式安排下一步前，先了解候选人方便联系的时间段。"}}
		if err := flow.processConversation(context.Background(), prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "合成人选", CommunicationPosition: "工程师"}, nil, &shared.Stats{}); err != nil {
			t.Fatal(err)
		}
		want := 2
		if noGoal {
			want = 1
		}
		if len(runtime.replies) != want || responder.answers != 1 || responder.reviews != 1 {
			t.Fatal("未执行一次独立目标复核", runtime.replies, responder.answers, responder.reviews)
		}
		state, err := flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := flow.reviewGoalsAfterAnswer(context.Background(), prepared, runtime, ReplyContext{Credentials: credentials(prepared), Position: position, Conversation: *state.Conversation, PageSnapshot: page, BasedOnMessageKey: "m1"}, "可以的，我们继续聊。"); err != nil || responder.reviews != 1 {
			t.Fatal("同一新消息事件重复复核", responder.reviews, err)
		}
		if _, _, err := flow.processCheckpoint(context.Background(), prepared, unreadScannerStub{}, runtime, []cloud.AutoReplyPositionSnapshot{position}, 1, &shared.Stats{}, &shared.ConsecutiveErrorPolicy{}, nil); err != nil || len(runtime.replies) != want || responder.reviews != 1 {
			t.Fatal("无新证据时仍复核或催问", runtime.replies, responder.reviews, err)
		}
		server.Close()
	}
}

// TestGoalReviewSkipsPlannedOrUnansweredWork 验证已有明确计划和已问未答时不增加第二次模型检查。
func TestGoalReviewSkipsPlannedOrUnansweredWork(t *testing.T) {
	answer := ReplyDecision{Messages: []ReplyMessage{{Type: "answer", Content: "可以的。"}}}
	asked := cloud.CandidateConfirmationItem{Status: "pending", ItemType: "confirm", AskCount: 1}
	if needsMessageGoalReview(answer, []cloud.CandidateConfirmationItem{asked}) {
		t.Fatal("已问未答仍追加目标检查")
	}
	answer.ConfirmationUpdates = []cloud.CandidateConfirmationItem{{ID: "goal"}}
	if needsMessageGoalReview(answer, nil) {
		t.Fatal("已有目标规划仍追加检查")
	}
}

// TestPersistedGoalDraftDropsEditedRecruitmentFacts 验证重启恢复时岗位或公司事实变化会清掉旧草稿，无版本的旧草稿也不能发送。
func TestPersistedGoalDraftDropsEditedRecruitmentFacts(t *testing.T) {
	for _, change := range []string{"company", "position", "legacy_without_revision"} {
		flow, prepared := newReliabilityFlow(t, "")
		ctx, cred := context.Background(), credentials(prepared)
		conv, err := flow.Cloud.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread", PositionID: "position"})
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := flow.Cloud.EnsureLocalAutoReplyCandidate(ctx, cred, conv)
		if err != nil {
			t.Fatal(err)
		}
		conv.CandidateID = candidate.ID
		if _, err := flow.Cloud.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: []cloud.AutoReplyMessage{{PlatformMessageID: "m1", Direction: "candidate", TextContent: "我有兴趣"}}}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := flow.Cloud.ClaimAutoReplySend(ctx, cred, conv.ID, "m1", "可以的。", 1); err != nil {
			t.Fatal(err)
		}
		if err := flow.Cloud.FinishAutoReplySend(ctx, cred, conv.ID, "m1", 1, "sent", "out"); err != nil {
			t.Fatal(err)
		}
		if _, err := flow.Cloud.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: []cloud.AutoReplyMessage{{PlatformMessageID: "out", Direction: "self", TextContent: "可以的。"}}}); err != nil {
			t.Fatal(err)
		}
		goal, err := flow.Cloud.SaveAutoReplyConfirmationItem(ctx, cred, cloud.CandidateConfirmationItem{ID: "goal", ConversationID: conv.ID, CandidateID: candidate.ID, PositionID: "position", ItemType: "confirm", SourceType: "ai", CreatedByKind: "ai", Content: "方便联系时间", Status: "pending"})
		if err != nil {
			t.Fatal(err)
		}
		position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position"}, Config: cloud.PositionAutoReplyConfig{Version: 1, PositionDescription: "安排沟通前确认方便联系时间"}, CompanyProfile: cloud.CompanyProfile{ID: "company", ExtraInfo: "周末双休"}}
		if claimed, err := flow.Cloud.ClaimAutoReplyMessageGoalReview(ctx, cred, conv.ID, "m1", "out", 1); err != nil || !claimed {
			t.Fatal("未领取事件", claimed, err)
		}
		draft := cloud.AutoReplyGoalFollowup{ContextRevision: cloud.AutoReplyGoalContextRevision(position), ConfirmationItemID: goal.ID, Content: "我们周末双休，你方便什么时间通话？", ExpectedContentKey: cloud.AutoReplyGoalContentKey(goal)}
		if err := flow.Cloud.CommitAutoReplyMessageGoalReview(ctx, cred, conv.ID, "m1", "out", nil, draft); err != nil {
			t.Fatal(err)
		}
		queued, _, valid, err := flow.Cloud.AutoReplyGoalFollowupState(ctx, cred, conv.ID)
		if err != nil || !valid || queued.GoalFollowup == nil {
			t.Fatal("草稿未持久保存", valid, err)
		}
		switch change {
		case "company":
			position.CompanyProfile.ExtraInfo = "排班待确认"
		case "position":
			position.Config.Version++
		case "legacy_without_revision":
			queued.GoalFollowup.ContextRevision = ""
			// 旧版队列的正文仍保留在服务器，但缺版本时只能放下，不允许猜测仍然有效。
			if err := flow.Cloud.ServerAutoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error { return tx.Put("conversations", queued.ID, queued) }); err != nil {
				t.Fatal(err)
			}
		}
		runtime := &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{latestBefore: model.ConversationMessage{PlatformMessageID: "out", Direction: "self", TextContent: "可以的。"}}}
		if err := flow.deliverGoalFollowup(ctx, prepared, runtime, position, queued, model.AutoReplyConversationSnapshot{}); err != nil {
			t.Fatal(change, err)
		}
		current, _, _, err := flow.Cloud.AutoReplyGoalFollowupState(ctx, cred, conv.ID)
		if err != nil || current.GoalFollowup != nil || len(runtime.replies) != 0 {
			t.Fatal("旧事实草稿在重启后复活", change, runtime.replies, err)
		}
	}
}

// TestGoalFlowAnswersThenAsksAndResumeEventDoesNotNeedNewInbound 验证真实公共编排顺序、新简历事件恢复以及无新事件时不再催问。
func TestGoalFlowAnswersThenAsksAndResumeEventDoesNotNeedNewInbound(t *testing.T) {
	for _, afterResume := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true,"position":{"id":"position"},"config":{"position_id":"position","enabled":true,"version":1},"company_profile":{"id":"company"},"subscription":{"active":true,"allow_auto_reply":true}}`))
		}))
		flow, prepared := newReliabilityFlow(t, server.URL)
		responder := &goalPlanningResponder{afterResume: afterResume}
		flow.Responder = responder
		question := model.ConversationMessage{PlatformMessageID: "m1", Direction: "candidate", MessageType: "text", TextContent: "我对这个岗位有兴趣"}
		page := model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformThreadID: "thread", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{question}}
		if afterResume {
			page.Messages = append([]model.ConversationMessage{{PlatformMessageID: "resume-card", Direction: "candidate", MessageType: "resume", TextContent: "简历已发"}}, page.Messages...)
			page.ResumeCardAvailable = true
			page.ResumeSourceMessageID = "resume-card"
		}
		runtime := &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{opened: page, latestBefore: question, resumeBundle: model.AutoReplyResumeBundle{CandidateName: "合成人选", OnlineResumeText: "五年教学经验"}}}
		if afterResume {
			attachment := filepath.Join(t.TempDir(), "synthetic.txt")
			if err := os.WriteFile(attachment, []byte("五年教学经验"), 0600); err != nil {
				t.Fatal(err)
			}
			runtime.resumeBundle.AttachmentPaths = []string{attachment}
		}
		position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{Version: 1, PositionDescription: "安排沟通前了解候选人方便通话的时间段。"}}
		stats := &shared.Stats{}
		if err := flow.processConversation(context.Background(), prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "合成人选", CommunicationPosition: "工程师"}, nil, stats); err != nil {
			t.Fatal(afterResume, err)
		}
		if afterResume {
			if len(runtime.replies) != 1 {
				t.Fatal("简历尚未整理就发送了追问", runtime.replies)
			}
			if processed, _, err := flow.processCheckpoint(context.Background(), prepared, unreadScannerStub{}, runtime, []cloud.AutoReplyPositionSnapshot{position}, 1, stats, &shared.ConsecutiveErrorPolicy{}, nil); err != nil || processed != 1 {
				t.Fatal("没有新来信时未恢复新证据事件", processed, err)
			}
		}
		if len(runtime.replies) != 2 || runtime.replies[0] != "可以的，我们继续聊。" || runtime.replies[1] != "你方便什么时间通话？" {
			state, _ := flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"})
			goals, _ := flow.Cloud.AutoReplyConfirmationItems(context.Background(), credentials(prepared), state.Conversation.ID)
			t.Fatalf("未按先答后问顺序执行 mode=%v replies=%v reviews=%v collects=%v conv=%+v goals=%+v candidate=%+v", afterResume, runtime.replies, responder.reviews, runtime.resumeCollects, state.Conversation, goals, state.Candidate)
		}
		state, err := flow.Cloud.AutoReplyCandidateState(context.Background(), credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"})
		if err != nil {
			t.Fatal(err)
		}
		goals, err := flow.Cloud.AutoReplyConfirmationItems(context.Background(), credentials(prepared), state.Conversation.ID)
		if err != nil || len(goals) != 1 || goals[0].AskCount != 1 {
			t.Fatal("真实送达后目标计数错误", goals, err)
		}
		if _, _, err = flow.processCheckpoint(context.Background(), prepared, unreadScannerStub{}, runtime, []cloud.AutoReplyPositionSnapshot{position}, 1, stats, &shared.ConsecutiveErrorPolicy{}, nil); err != nil || len(runtime.replies) != 2 {
			t.Fatal("没有新证据仍重复追问", runtime.replies, err)
		}
		server.Close()
	}
}

// TestPhoneReceiptCanAdvanceUnaskedGoal 验证固定手机号签收后仍推进目标，且不会再编一条普通答复。
func TestPhoneReceiptCanAdvanceUnaskedGoal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"position":{"id":"position"},"config":{"position_id":"position","enabled":true,"version":1,"phrases_version":"1","phrases":[{"id":"receive_phone","content":"联系方式收到啦。"}]},"company_profile":{"id":"company"},"subscription":{"active":true,"allow_auto_reply":true}}`))
	}))
	defer server.Close()
	flow, prepared := newReliabilityFlow(t, server.URL)
	flow.Responder = &goalPlanningResponder{}
	message := model.ConversationMessage{PlatformMessageID: "phone", Direction: "candidate", MessageType: "text", TextContent: "13800138000"}
	runtime := &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{latestBefore: message, opened: model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformThreadID: "thread", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{message}}}}
	position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{Version: 1, PhrasesVersion: "1", Phrases: []cloud.AutoReplyPhrase{{ID: "receive_phone", Content: "联系方式收到啦。"}}}}
	if err := flow.processConversation(context.Background(), prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "合成人选", CommunicationPosition: "工程师"}, nil, &shared.Stats{}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.replies) != 2 || runtime.replies[0] != "联系方式收到啦。" || runtime.replies[1] != "你方便什么时间通话？" {
		t.Fatal("手机号签收后未按目标继续沟通", runtime.replies)
	}
}

// TestDynamicGoalBatchRejectsDuplicates 验证单次工具调用中同正文和同编号整批拒绝，不留下第二个可询问目标。
func TestDynamicGoalBatchRejectsDuplicates(t *testing.T) {
	for _, sameID := range []bool{false, true} {
		s := dynamicGoalState()
		item := confirmationToolItem{ItemType: "confirm", Content: "候选人方便通话的时间段", Status: "pending", StatusReason: "安排沟通前确认", SourceRef: "position", EvidenceText: "安排沟通前了解候选人方便通话的时间段"}
		if sameID {
			goal := createCallTimeGoal(t, s)
			item = conditionsForTool([]cloud.CandidateConfirmationItem{goal})[0]
		}
		second := item
		if sameID {
			second.Content += "（确认）"
		}
		before := len(s.confirmationUpdates)
		if _, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{item, second})); err == nil {
			t.Fatal("接受了单批重复目标", sameID)
		}
		if len(s.confirmationUpdates) != before || len(s.confirmations) > 1 {
			t.Fatal("校验失败后部分目标仍被合入", s.confirmations)
		}
	}
}

// createCallTimeGoal 通过真实工具参数提出新目标，返回程序生成的稳定编号。
func createCallTimeGoal(t *testing.T, s *toolExecutionState) cloud.CandidateConfirmationItem {
	t.Helper()
	item := confirmationToolItem{ItemType: "confirm", Content: "候选人方便通话的时间段", Status: "pending", StatusReason: "安排沟通前需要确认时间", SourceRef: "position", EvidenceText: "安排沟通前了解候选人方便通话的时间段"}
	if _, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{item})); err != nil {
		t.Fatal(err)
	}
	if len(s.confirmations) != 1 || s.confirmations[0].ID == "" {
		t.Fatal("没有返回可用于追问的目标编号")
	}
	return s.confirmations[0]
}

// TestDynamicGoalsCreateEditArchiveAndCancelPreparedQuestion 验证有依据建目标、去重、改写及删除后的旧追问失效。
func TestDynamicGoalsCreateEditArchiveAndCancelPreparedQuestion(t *testing.T) {
	s := dynamicGoalState()
	goal := createCallTimeGoal(t, s)
	if goal.SourceType != "ai" || goal.CreatedByKind != "ai" || !goal.RequireAbsent || goal.PositionConditionID != "" {
		t.Fatal("AI目标变成了岗位条件")
	}
	createCallTimeGoal(t, s)
	if len(s.confirmations) != 1 || s.confirmations[0].ID != goal.ID {
		t.Fatal("同正文目标重复创建")
	}
	raw, _ := json.Marshal(sendMessagesToolArgs{Messages: []ReplyMessage{{Type: "answer", Content: "可以的～"}, {Type: "confirmation", Content: "你方便什么时间通话？", ConfirmationItemID: goal.ID}}})
	if _, err := s.prepareMessages(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: string(raw)}}); err != nil {
		t.Fatal(err)
	}
	edit := conditionsForTool([]cloud.CandidateConfirmationItem{goal})[0]
	edit.Content = "候选人平日方便通话的具体时段"
	if _, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{edit})); err != nil {
		t.Fatal(err)
	}
	if len(s.pendingMessages) != 1 || s.pendingMessages[0].Type != "answer" {
		t.Fatal("修改目标后旧问题仍可发送")
	}
	archive, _ := json.Marshal(archiveGoalToolArgs{IDs: []string{goal.ID}, Reason: "候选人改为文字沟通，不再需要电话时间"})
	if _, err := s.archiveGoals(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: string(archive)}}); err != nil {
		t.Fatal(err)
	}
	if s.confirmations[0].ArchivedAt == nil || cloud.AutoReplyGoalCanAsk(s.confirmations[0]) {
		t.Fatal("已删除目标仍可追问")
	}
}

// TestDynamicGoalsCannotInventHardGateOrChangeHRConditions 验证岗位标题不构成新门槛，HR 必需条件不可改写或删除。
func TestDynamicGoalsCannotInventHardGateOrChangeHRConditions(t *testing.T) {
	for _, kind := range []string{"required", "confirm"} {
		s := dynamicGoalState()
		item := confirmationToolItem{ItemType: kind, Content: "具备教师资格证", Status: "pending", StatusReason: "猜测岗位需要", SourceRef: "position", EvidenceText: "教师资格证"}
		if _, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{item})); err == nil {
			t.Fatal("允许凭空增加招聘门槛")
		}
	}
	s := dynamicGoalState()
	s.confirmations = []cloud.CandidateConfirmationItem{{ID: "hr", ConversationID: "conv", CandidateID: "candidate", PositionID: "position", SourceType: "position", PositionConditionID: "configured", ItemType: "required", Content: "可以在成都工作", Status: "pending"}}
	edit := confirmationToolItem{ID: "hr", ItemType: "confirm", Content: "考虑任意工作地点", Status: "pending", StatusReason: "放宽条件", SourceRef: "position", EvidenceText: "工作地点成都"}
	if _, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{edit})); err == nil {
		t.Fatal("允许改写岗位必要条件")
	}
	raw, _ := json.Marshal(archiveGoalToolArgs{IDs: []string{"hr"}, Reason: "想直接生成报告"})
	if _, err := s.archiveGoals(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: string(raw)}}); err == nil {
		t.Fatal("允许删除HR必要条件")
	}
}

// TestGoalQuestionDoesNotRepeatOrOutliveContact 验证已问未答、候选人拒绝与归档后的问题都不能排入待发消息。
func TestGoalQuestionDoesNotRepeatOrOutliveContact(t *testing.T) {
	for _, mode := range []string{"asked", "archived", "declined"} {
		t.Run(mode, func(t *testing.T) {
			s := dynamicGoalState()
			goal := createCallTimeGoal(t, s)
			now := time.Now().UTC()
			switch mode {
			case "asked":
				s.confirmations[0].AskCount = 1
				s.confirmations[0].LastAskedAt = &now
			case "archived":
				s.confirmations[0].ArchivedAt = &now
			case "declined":
				s.input.PendingMessages = []cloud.AutoReplyMessage{{Direction: "candidate", TextContent: "不要联系我了"}}
			}
			raw, _ := json.Marshal(sendMessagesToolArgs{Messages: []ReplyMessage{{Type: "confirmation", Content: "你方便什么时间通话？", ConfirmationItemID: goal.ID}}})
			if _, err := s.prepareMessages(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: string(raw)}}); err == nil {
				t.Fatal("允许重复或被拒绝的追问")
			}
		})
	}
}

// TestNewResumeEvidenceCanProposeOneQuestionWithoutReansweringHistory 验证新证据轮可追问目标，但不能把历史问题再回答一次。
func TestNewResumeEvidenceCanProposeOneQuestionWithoutReansweringHistory(t *testing.T) {
	s := dynamicGoalState()
	s.input.ConditionsOnly, s.input.AllowGoalFollowup = true, true
	goal := createCallTimeGoal(t, s)
	for _, kind := range []string{"answer", "confirmation"} {
		raw, _ := json.Marshal(sendMessagesToolArgs{Messages: []ReplyMessage{{Type: kind, Content: "方便什么时间通话？", ConfirmationItemID: goal.ID}}})
		_, err := s.execute(context.Background(), ai.ToolCall{Function: ai.ToolCallFunction{Name: toolSendMessages, Arguments: string(raw)}})
		if (err == nil) != (kind == "confirmation") {
			t.Fatalf("新证据轮发送范围错误 kind=%s err=%v", kind, err)
		}
	}
	for _, def := range autoReplyToolsForContext(s.input) {
		if def.Function.Name == toolNotifyHR {
			t.Fatal("新证据复核不应通知未知招聘问题")
		}
	}
}

// TestNotifyHRCanKeepIndependentCandidateGoal 验证登记HR未知问题时仍可保留候选人信息追问，不把整个目标流程停住。
func TestNotifyHRCanKeepIndependentCandidateGoal(t *testing.T) {
	s := dynamicGoalState()
	goal := createCallTimeGoal(t, s)
	s.input.PendingMessages = []cloud.AutoReplyMessage{{Direction: "candidate", PlatformMessageID: "m", TextContent: "请问有没有班车？"}}
	raw, _ := json.Marshal(notifyHRToolArgs{Reason: "班车安排缺少公开资料", ReasonKey: "missing_recruitment_info", Question: "请问有没有班车？", Followup: &goalFollowupToolItem{Content: "你方便什么时间通话？", ConfirmationItemID: goal.ID}})
	if _, err := s.prepareManualHandoff(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: string(raw)}}); err != nil {
		t.Fatal(err)
	}
	if s.manualReason == "" || s.manualFollowup == nil || s.manualFollowup.Type != "confirmation" || s.manualFollowup.GoalContentKey == "" {
		t.Fatal("HR待确认吞掉了独立候选人目标")
	}
}

// TestDynamicGoalDoesNotChangeReportAdmission 验证 AI 沟通目标既不能绕过 HR 必需条件，也不会自己变成推荐硬门槛。
func TestDynamicGoalDoesNotChangeReportAdmission(t *testing.T) {
	in := proposalState().input
	in.Position.Config.Conditions = []cloud.PositionReplyCondition{{ID: "chengdu", Type: "required", Content: "可在成都", Enabled: true}}
	in.CandidateState.Candidate = &cloud.AutoReplyStoredCandidate{ID: "candidate", ResumeStructured: true}
	hr := cloud.CandidateConfirmationItem{ID: "hr", PositionID: "position", PositionConditionID: "chengdu", ItemType: "required", Content: "可在成都", SourceType: "position", Status: "matched", SourceRef: "message-1", EvidenceText: "我可以接受出差"}
	dynamic := cloud.CandidateConfirmationItem{ID: "ai", ConversationID: "conv", PositionID: "position", SourceType: "ai", CreatedByKind: "ai", ItemType: "confirm", Content: "方便通话时间", Status: "pending"}
	if _, qualified := recommendationSource(in, []cloud.CandidateConfirmationItem{hr, dynamic}); !qualified {
		t.Fatal("未回答AI沟通目标阻止了HR已经确认的准入")
	}
	if _, qualified := recommendationSource(in, []cloud.CandidateConfirmationItem{dynamic}); qualified {
		t.Fatal("AI目标绕过了缺失的HR必要条件")
	}
}
