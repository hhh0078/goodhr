// Package auto_reply 本文件验证AI提议没有业务副作用、证据校验、增量条件和上下文预算。
package auto_reply

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// proposalCall 用真实参数协议构造合成条件工具调用。
func proposalCall(t *testing.T, items []confirmationToolItem) ai.ToolCall {
	t.Helper()
	raw, err := json.Marshal(confirmationToolArgs{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	return ai.ToolCall{Type: "function", Function: ai.ToolCallFunction{Name: toolUpsertConfirmations, Arguments: string(raw)}}
}

// proposalState 创建不带网络客户端的决策状态，工具若偷偷持久化会直接导致测试失败。
func proposalState() *toolExecutionState {
	return &toolExecutionState{input: ReplyContext{Conversation: cloud.AutoReplyConversation{ID: "conv", CandidateID: "candidate"}, Position: cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position"}}, BasedOnMessageKey: "message-1", Messages: []cloud.AutoReplyMessage{{Fingerprint: "message-1", Direction: "candidate", TextContent: "我可以接受出差"}}}}
}

// TestConfirmationProposalRejectsInventedEvidence 阻止模型无证据、虚构来源或引用不相符原文时写入符合状态。
func TestConfirmationProposalRejectsInventedEvidence(t *testing.T) {
	for _, item := range []confirmationToolItem{
		{ID: "travel", ItemType: "required", Content: "接受出差", Status: "matched", StatusReason: "模型判断", SourceRef: "message-1"},
		{ID: "travel", ItemType: "required", Content: "接受出差", Status: "matched", StatusReason: "模型判断", SourceRef: "resume", EvidenceText: "我可以接受出差"},
		{ID: "travel", ItemType: "required", Content: "接受出差", Status: "matched", StatusReason: "模型判断", SourceRef: "message-1", EvidenceText: "完全不存在的证据"},
	} {
		state := proposalState()
		state.confirmations = []cloud.CandidateConfirmationItem{{ID: "travel", ItemType: "required", Content: "接受出差", Status: "pending"}, {ID: "other", ItemType: "confirm", Content: "其他待确认条件", Status: "pending"}}
		_, err := state.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{item}))
		if err == nil || len(state.confirmationUpdates) != 0 {
			t.Fatalf("虚构证据被接受：%+v %v", state.confirmationUpdates, err)
		}
		valid := confirmationToolItem{ID: "other", ItemType: "confirm", Content: "其他待确认条件", Status: "pending", StatusReason: "尚未说明"}
		_, err = state.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{valid, item}))
		if err == nil || len(state.confirmationUpdates) != 0 || state.confirmations[0].Status != "pending" {
			t.Fatalf("失败参数留下半批判断：%+v %v", state.confirmationUpdates, err)
		}
	}
}

// TestConfirmationProposalIsLocalAndIncremental 验证51条既有条件仍可增量更新，返回稳定ID且不访问存储。
func TestConfirmationProposalIsLocalAndIncremental(t *testing.T) {
	state := proposalState()
	for index := 0; index < 51; index++ {
		state.confirmations = append(state.confirmations, cloud.CandidateConfirmationItem{ID: fmt.Sprintf("condition-%d", index), ItemType: "required", Content: fmt.Sprintf("条件%d", index), Status: "pending"})
	}
	_, err := state.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{{ID: "condition-0", ItemType: "required", Content: "条件0", Status: "matched", StatusReason: "有候选人原文", SourceRef: "message-1", EvidenceText: "我可以接受出差"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.confirmationUpdates) != 1 || len(state.confirmations) != 51 || state.confirmationUpdates[0].ID != "condition-0" {
		t.Fatalf("增量结果不正确：%+v", state.confirmationUpdates)
	}
}

// TestUnconfiguredConditionCannotBeReviewedOrAsked 验证普通问答和简历补审均拒绝凭岗位名称新增门槛，并忽略旧 AI 条目。
func TestUnconfiguredConditionCannotBeReviewedOrAsked(t *testing.T) {
	for _, review := range []bool{false, true} {
		input := proposalState().input
		input.ConditionsOnly = review
		input.Position.Position.Name = "高中数学老师"
		input.Position.Config.Conditions = []cloud.PositionReplyCondition{{ID: "location", Type: "confirm", Content: "可以在成都工作", Enabled: true}}
		input.ConfirmationItems = []cloud.CandidateConfirmationItem{
			{ID: "chengdu", PositionID: "position", SourceType: "position", PositionConditionID: "location", ItemType: "confirm", Content: "可以在成都工作", Status: "pending"},
			{ID: "old-ai", PositionID: "position", SourceType: "ai", ItemType: "confirm", Content: "具备高中数学教学经验", Status: "pending"},
		}
		bounded, err := boundedReplyContext(input)
		if err != nil || len(bounded.ConfirmationItems) != 1 || bounded.ConfirmationItems[0].ID != "chengdu" {
			t.Fatalf("模型上下文包含未配置条件：%+v %v", bounded.ConfirmationItems, err)
		}
		state := toolExecutionState{input: bounded, confirmations: bounded.ConfirmationItems}
		location := confirmationToolItem{ID: "chengdu", ItemType: "confirm", Content: "可以在成都工作", Status: "pending", StatusReason: "待确认"}
		for _, unauthorized := range []confirmationToolItem{
			{ItemType: "confirm", Content: "具备高中数学教学经验", Status: "pending", StatusReason: "岗位名称推断"},
			{ID: "old-ai", ItemType: "confirm", Content: "具备高中数学教学经验", Status: "pending", StatusReason: "沿用旧条件"},
			{ID: "chengdu", ItemType: "confirm", Content: "具备高中数学教学经验", Status: "pending", StatusReason: "借用有效编号"},
			{ID: "chengdu", PositionConditionID: "not-location", ItemType: "confirm", Content: "可以在成都工作", Status: "pending", StatusReason: "编号冲突"},
		} {
			if _, err = state.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{location, unauthorized})); err == nil || len(state.confirmationUpdates) != 0 {
				t.Fatalf("越权条件或半批更新被接受：%+v %v", state.confirmationUpdates, err)
			}
		}
		raw, _ := json.Marshal(sendMessagesToolArgs{Messages: []ReplyMessage{{Type: "confirmation", Content: "有高中数学教学经验吗？", ConfirmationItemID: "old-ai"}}})
		if _, err = state.prepareMessages(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: string(raw)}}); err == nil || len(state.pendingMessages) != 0 {
			t.Fatal("允许引用旧 AI 条目发起追问")
		}
	}
}

// TestPendingBatchDoesNotLoseEarlierQuestion 验证连续的薪资问题和致谢被作为同一批次，不能被简短寒暄覆盖。
func TestPendingBatchDoesNotLoseEarlierQuestion(t *testing.T) {
	input := ReplyContext{BasedOnMessageKey: "thanks", Messages: []cloud.AutoReplyMessage{{Fingerprint: "question", Direction: "candidate", TextContent: "薪资多少"}, {Fingerprint: "thanks", Direction: "candidate", TextContent: "谢谢"}}}
	input.PendingMessages = input.Messages
	got := normalizeSimpleCandidateReply(input, "薪资为8千到1万。")
	if got != "薪资为8千到1万。" {
		t.Fatal(got)
	}
	bounded, err := boundedReplyContext(input)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := initialToolMessages(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(messages[1].Content, "薪资多少") || !strings.Contains(messages[1].Content, "pending_messages") {
		t.Fatal(messages[1].Content)
	}
}

// TestReplyContextBudgetKeepsPendingBatch 验证庞大历史会被裁剪，待处理批次完整保留且调用字节数有限。
func TestReplyContextBudgetKeepsPendingBatch(t *testing.T) {
	input := ReplyContext{BasedOnMessageKey: "new"}
	for index := 0; index < 5000; index++ {
		input.Messages = append(input.Messages, cloud.AutoReplyMessage{Fingerprint: fmt.Sprint(index), Direction: "candidate", TextContent: strings.Repeat("历史", 2000)})
	}
	latest := cloud.AutoReplyMessage{Fingerprint: "new", Direction: "candidate", TextContent: "工作地点在哪里"}
	input.Messages = append(input.Messages, latest)
	input.PendingMessages = []cloud.AutoReplyMessage{latest}
	bounded, err := boundedReplyContext(input)
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := initialToolMessages(bounded)
	raw, _ := json.Marshal(messages)
	if len(raw) > maxReplyContextBytes || len(bounded.Messages) >= 80 || len(bounded.PendingMessages) != 1 || bounded.PendingMessages[0].TextContent != latest.TextContent {
		t.Fatalf("预算失效：%d字节 %d条", len(raw), len(bounded.Messages))
	}
	if len(input.Messages) != 5001 {
		t.Fatal("修改了调用方原始历史")
	}
}

// TestRecommendationHashIgnoresReasonWording 验证同一证据被模型换一种说法时不会产生新的付费报告任务。
func TestRecommendationHashIgnoresReasonWording(t *testing.T) {
	source := cloud.AutoReplyRecommendationReport{Conditions: []cloud.AutoReplyReportCondition{{ID: "one", Status: "matched", StatusReason: "符合", Evidence: []string{"接受出差"}}}}
	first, err := recommendationInputHash(source)
	if err != nil {
		t.Fatal(err)
	}
	source.Conditions[0].StatusReason = "候选人已确认符合"
	second, err := recommendationInputHash(source)
	if err != nil || first != second {
		t.Fatalf("同证据哈希变化：%s %s %v", first, second, err)
	}
}
