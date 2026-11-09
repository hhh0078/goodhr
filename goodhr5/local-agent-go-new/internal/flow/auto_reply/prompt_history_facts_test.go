// 本文件验证模型历史投影隔离已撤回的雇主事实，同时保留候选人证据、沟通状态与完整原始审计。
package auto_reply

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// TestPromptHistoryCannotRestoreWithdrawnEmployerFact 回归实际停车补答撤回案例，旧己方正文与卡片摘要不再进入模型。
func TestPromptHistoryCannotRestoreWithdrawnEmployerFact(t *testing.T) {
	input := responderInput("https://example.test")
	sentAt := time.Date(2026, 11, 9, 5, 31, 0, 0, time.UTC)
	oldFact := "公司提供免费员工停车位，需提前一天预约。"
	old := cloud.AutoReplyMessage{PlatformMessageID: "old-answer", Direction: "self", MessageType: "text", TextContent: oldFact, CardContent: json.RawMessage(`{"summary":"公司提供免费员工停车位，需提前一天预约。"}`), PlatformSentAt: &sentAt}
	latest := cloud.AutoReplyMessage{PlatformMessageID: "new-question", Direction: "candidate", MessageType: "text", TextContent: "请问员工停车是否免费？"}
	input.Messages, input.PendingMessages, input.BasedOnMessageKey = []cloud.AutoReplyMessage{old, latest}, []cloud.AutoReplyMessage{latest}, latest.PlatformMessageID
	input.Position.CompanyProfile.ExtraInfo = "公司办公地址为成都测试园区B座。"
	input.KnownQuestions = []knownRecruitmentQuestion{{ID: "parking-question", Question: "公司有免费的员工停车位吗？", Status: "resolved"}}
	bounded, err := boundedReplyContext(input)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := initialToolMessages(bounded)
	if err != nil {
		t.Fatal(err)
	}
	raw := rows[len(rows)-1].Content
	if strings.Contains(raw, oldFact) || strings.Contains(raw, "提前一天预约") {
		t.Fatal("已经撤回的雇主事实仍出现在模型动态数据")
	}
	var dynamic replyPromptInput
	if err = json.Unmarshal([]byte(raw), &dynamic); err != nil {
		t.Fatal(err)
	}
	if len(dynamic.Messages) != 1 || !dynamic.Messages[0].HistoricalContentOmitted || dynamic.Messages[0].SourceRef != old.PlatformMessageID || dynamic.Messages[0].PlatformSentAt == nil || !dynamic.Messages[0].PlatformSentAt.Equal(sentAt) || dynamic.Messages[0].CardSummary != "" {
		t.Fatal("历史送达语境或隔离标记丢失", dynamic.Messages)
	}
	if len(dynamic.PendingMessages) != 1 || dynamic.PendingMessages[0].TextContent != latest.TextContent || dynamic.KnownQuestions[0].ID != "parking-question" {
		t.Fatal("本轮候选原话或重开去重编号丢失")
	}
	if input.Messages[0].TextContent != oldFact || string(input.Messages[0].CardContent) != string(old.CardContent) {
		t.Fatal("模型投影修改了原始服务器审计")
	}
	if !strings.Contains(rows[0].Content, "候选人原话与简历只能证明候选人自己的情况") || !strings.Contains(rows[0].Content, "字段为空或不再记载某项信息") {
		t.Fatal("工具协议没有限定候选人引用旧答复与当前正文撤回的事实边界")
	}
}

// TestPromptOmittedSelfRetainsContactAndGoalState 验证隔离己方正文后仍传递索号、简历和已问目标状态，避免重复打扰。
func TestPromptOmittedSelfRetainsContactAndGoalState(t *testing.T) {
	input := responderInput("https://example.test")
	input.Messages = append([]cloud.AutoReplyMessage{{PlatformMessageID: "asked-phone", Direction: "self", MessageType: "text", TextContent: "方便留个手机号吗？"}}, input.Messages...)
	input.Conversation.Status = "waiting_resume"
	input.CandidateState.HasResumeAttachment = true
	input.ConfirmationItems = []cloud.CandidateConfirmationItem{{ID: "contact-time", ConversationID: input.Conversation.ID, PositionID: input.Position.Position.ID, SourceType: "ai", CreatedByKind: "ai", ItemType: "confirm", Content: "方便联系的时间", Status: "pending", AskCount: 1}}
	bounded, err := boundedReplyContext(input)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := initialToolMessages(bounded)
	if err != nil {
		t.Fatal(err)
	}
	var dynamic replyPromptInput
	if err = json.Unmarshal([]byte(rows[len(rows)-1].Content), &dynamic); err != nil {
		t.Fatal(err)
	}
	if !dynamic.PhoneContact.Asked || !dynamic.CandidateState.HasResumeAttachment || dynamic.Conversation.Status != "waiting_resume" || dynamic.AskHistory["contact-time"].AskCount != 1 || dynamic.AskHistory["contact-time"].CanAskAgain {
		t.Fatal("历史投影隔离后丢失防重复沟通状态", dynamic.PhoneContact, dynamic.AskHistory)
	}
	state := toolExecutionState{input: bounded, confirmations: bounded.ConfirmationItems}
	if err = checkPhoneReply(bounded.PhoneContact, bounded.Position.Config, "方便留个手机号吗？"); err == nil {
		t.Fatal("历史正文省略后程序仍应阻止重复索号")
	}
	if err = state.validateGoalQuestion(ReplyMessage{Type: "confirmation", Content: "方便联系的时间是？", ConfirmationItemID: "contact-time"}); err == nil {
		t.Fatal("历史正文省略后程序仍应阻止重复问目标")
	}
	if _, err = state.prepareMessages(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: `{"messages":[{"type":"answer","content":"方便再发一下简历吗？","requests_resume":true}]}`}}); err == nil {
		t.Fatal("历史正文省略后程序仍应阻止重复索要简历")
	}
}
