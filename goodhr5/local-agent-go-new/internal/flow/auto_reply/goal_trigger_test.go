// Package auto_reply 本文件复现已处理历史拒绝被重建为pending目标的实测问题，验证只收紧新目标触发来源而保留既有目标和资料证据。
package auto_reply

import (
	"context"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// processedRefusalGoalState 使用run23同样的历史明确拒绝与本轮纯地点问题构造模型可见上下文，当前岗位没有排班条件。
func processedRefusalGoalState() *toolExecutionState {
	s := proposalState()
	old := cloud.AutoReplyMessage{ID: "old-local", Fingerprint: "history-refusal", Direction: "candidate", MessageType: "text", TextContent: "我不能接受周末排班。", ConversationID: s.input.Conversation.ID}
	current := cloud.AutoReplyMessage{Fingerprint: "current-location", Direction: "candidate", MessageType: "text", TextContent: "我再确认一下，工作地点是在成都吗？", ConversationID: s.input.Conversation.ID}
	s.input.Messages, s.input.PendingMessages = []cloud.AutoReplyMessage{old, current}, []cloud.AutoReplyMessage{current}
	s.input.BasedOnMessageKey = current.Fingerprint
	s.input.Position.Config.PositionDescription = "已知工作地点：成都。当前只需可到成都到岗和大专以上，工作时间安排未确认。"
	return s
}

// TestNewPendingGoalRejectsProcessedHistoricalRefusal 验证逐字真实的旧拒绝仍不能作为本轮新pending的唯一依据，也不因模型伪造前缀兜底而放行。
func TestNewPendingGoalRejectsProcessedHistoricalRefusal(t *testing.T) {
	for _, ref := range []string{"history-refusal", "old-local", "candidate_message:history-refusal"} {
		s := processedRefusalGoalState()
		item := confirmationToolItem{ItemType: "confirm", Status: "pending", Content: "确认候选人能否接受本岗位可能涉及的周末排班安排", StatusReason: "候选人主动说明不能接受周末排班，需要作为沟通事项了解", SourceRef: ref, EvidenceText: "我不能接受周末排班。"}
		_, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{item}))
		if err == nil || !strings.Contains(err.Error(), "本轮pending_messages") || len(s.confirmationUpdates) != 0 || len(s.confirmations) != 0 {
			t.Fatal("旧拒绝被重建成待确认，或失败后留下目标副作用", ref, err)
		}
	}
}

// TestNewPendingGoalAllowsCurrentOpenQuestion 验证本轮候选人确实提出新的未决事项时仍可新增动态目标，不把功能改成固定HR条件。
func TestNewPendingGoalAllowsCurrentOpenQuestion(t *testing.T) {
	s := processedRefusalGoalState()
	current := s.input.PendingMessages[0]
	current.TextContent = "我还需要确认一下什么时候可以到岗。"
	s.input.PendingMessages = []cloud.AutoReplyMessage{current}
	s.input.Messages[1] = current
	item := confirmationToolItem{ItemType: "confirm", Status: "pending", Content: "确认候选人预计到岗时间", StatusReason: "本轮候选人说明到岗时间仍需确认", SourceRef: current.Fingerprint, EvidenceText: current.TextContent}
	if _, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{item})); err != nil || len(s.confirmationUpdates) != 1 || s.confirmationUpdates[0].Status != "pending" || s.confirmationUpdates[0].SourceRef != current.Fingerprint {
		t.Fatal("本轮真实未决事项无法建立动态目标", err)
	}
}

// TestRewrittenPendingGoalCannotRecycleHistory 验证已问AI目标不能只改措辞就借同一历史拒绝重新变成可追问的新pending问题。
func TestRewrittenPendingGoalCannotRecycleHistory(t *testing.T) {
	s := processedRefusalGoalState()
	goal := cloud.CandidateConfirmationItem{ID: "existing", ConversationID: s.input.Conversation.ID, CandidateID: s.input.Conversation.CandidateID, PositionID: s.input.Position.Position.ID, SourceType: "ai", CreatedByKind: "ai", ItemType: "confirm", Content: "确认候选人排班偏好", Status: "pending", AskCount: 1}
	s.confirmations = []cloud.CandidateConfirmationItem{goal}
	item := confirmationToolItem{ID: goal.ID, ItemType: "confirm", Status: "pending", Content: "确认候选人是完全不能接受周末排班还是视情况而定", StatusReason: "换一种方式进一步了解", SourceRef: "history-refusal", EvidenceText: "我不能接受周末排班。"}
	if _, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{item})); err == nil || !strings.Contains(err.Error(), "本轮pending_messages") || len(s.confirmationUpdates) != 0 || s.confirmations[0].Content != goal.Content {
		t.Fatal("改写旧目标绕过历史重复追问保护", err)
	}
}

// TestExistingGoalCanUseHistoricalConclusion 验证旧目标更新matched或unmatched仍可引用合法历史，不因新目标触发收紧而丢失明确结论。
func TestExistingGoalCanUseHistoricalConclusion(t *testing.T) {
	for _, status := range []string{"matched", "unmatched"} {
		s := processedRefusalGoalState()
		body := "确认候选人是否拒绝周末排班"
		if status == "unmatched" {
			body = "确认候选人能否接受周末排班"
		}
		goal := cloud.CandidateConfirmationItem{ID: "existing", ConversationID: s.input.Conversation.ID, CandidateID: s.input.Conversation.CandidateID, PositionID: s.input.Position.Position.ID, SourceType: "ai", CreatedByKind: "ai", ItemType: "confirm", Content: body, Status: "pending"}
		s.confirmations = []cloud.CandidateConfirmationItem{goal}
		item := confirmationToolItem{ID: goal.ID, ItemType: "confirm", Content: body, Status: status, StatusReason: "候选人历史原文已有明确结论", SourceRef: "history-refusal", EvidenceText: "我不能接受周末排班。"}
		if _, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{item})); err != nil || len(s.confirmationUpdates) != 1 || s.confirmationUpdates[0].Status != status || s.confirmationUpdates[0].SourceRef != "history-refusal" {
			t.Fatal("历史真实证据不能更新旧目标结论", status, err)
		}
	}
}

// TestNewPendingGoalKeepsPositionAndResumeSources 验证当前岗位、在线资料和简历的既有建档依据规则保持不变。
func TestNewPendingGoalKeepsPositionAndResumeSources(t *testing.T) {
	for _, ref := range []string{"position", "resume", "online_profile"} {
		s := processedRefusalGoalState()
		quote := "安排沟通前了解候选人方便通话的时间段"
		s.input.Position.Config.PositionDescription = quote
		s.input.Resume = &model.AutoReplyResumeBundle{OnlineResumeText: quote}
		s.input.CandidateState.Candidate = &cloud.AutoReplyStoredCandidate{OnlineProfile: &cloud.AutoReplyOnlineProfileState{Profile: model.AutoReplyOnlineProfile{RawText: quote}}}
		item := confirmationToolItem{ItemType: "confirm", Status: "pending", Content: "确认候选人方便通话时间", StatusReason: "安排沟通前需要了解", SourceRef: ref, EvidenceText: quote}
		if _, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{item})); err != nil || len(s.confirmationUpdates) != 1 {
			t.Fatal("非历史候选消息的既有依据被误拦", ref, err)
		}
	}
}

// TestGoalTriggerContractKeepsAnsweredFactsClosed 验证固定合同提醒先回答当前问题，不把历史偏好当作岗位安排或重建待确认。
func TestGoalTriggerContractKeepsAnsweredFactsClosed(t *testing.T) {
	for _, text := range []string{"只能引用本轮pending_messages", "历史仍可用于更新已有目标", "没有目标则保留沟通事实", "不得换成“完全不能接受还是视具体情况”再次追问", "偏好不能推断本岗位", "地点已能完整回答时，直接回答地点"} {
		if !strings.Contains(autoReplyToolContract, text) {
			t.Fatal("缺少固定动态目标合同", text)
		}
	}
}
