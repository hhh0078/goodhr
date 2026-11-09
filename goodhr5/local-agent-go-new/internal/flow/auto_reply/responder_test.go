// Package auto_reply 本文件验证 AI 工具循环的提示词边界、参数修正、调用上限和完整审计。
package auto_reply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type responderTestState struct {
	mu                      sync.Mutex
	aiResponses             []string
	aiRequests              []json.RawMessage
	toolCalls               []cloud.AutoReplyToolCall
	finishedRuns            []cloud.AutoReplyAIRun
	resumeStructureRequests []cloud.AutoReplyResumeStructureRequest
	chatCallNumber          int
}

// TestAutoReplyToolDefinitionsAreStableAndUnique 验证目标与回复工具名称唯一且每份参数定义都是合法 JSON。
func TestAutoReplyToolDefinitionsAreStableAndUnique(t *testing.T) {
	definitions := autoReplyToolDefinitions()
	if len(definitions) != 5 {
		t.Fatalf("tool count = %d", len(definitions))
	}
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		name := definition.Function.Name
		if _, exists := seen[name]; exists {
			t.Fatalf("duplicate tool = %s", name)
		}
		seen[name] = struct{}{}
		if definition.Type != "function" || !json.Valid(definition.Function.Parameters) {
			t.Fatalf("tool = %+v", definition)
		}
	}
}

// TestAutoReplySystemPromptKeepsHumanHRReplyBoundaries 验证固定提示词限制真人 HR 语气、本轮消息边界和内部确认项边界。
func TestAutoReplySystemPromptKeepsHumanHRReplyBoundaries(t *testing.T) {
	requiredRules := []string{
		"你就是当前岗位的招聘方 HR",
		"对话中不存在另一个需要你转达的“招聘方”",
		"pending_messages 是本轮尚未处理的连续消息批次",
		"优先按语义匹配已有目标",
		"条件状态只能是 pending、matched、unmatched",
		"资料和聊天没有提到时必须是 pending",
		"HR 配置的 required 和 confirm 决定岗位准入",
		"用独立第二条 confirmation 主动追问一个",
		"如果本轮问题都没有可靠答案，只调用 notify_hr",
		"不要承诺后续主动反馈或具体时间，也不要声称已经转达",
		"禁止提及 GoodHR、AI、系统、工具、岗位资料或另一个“招聘方”",
		"单条优先120字以内、最多200字",
	}
	for _, rule := range requiredRules {
		if !strings.Contains(autoReplySystemPrompt, rule) {
			t.Fatalf("自动回复固定提示词缺少规则：%s", rule)
		}
	}
}

// TestPromptMessagesKeepOneVerifiableCopy 验证历史和待回复批次只传一份可核验原文，身份与入库时间均不外发。
func TestPromptMessagesKeepOneVerifiableCopy(t *testing.T) {
	input := responderInput("https://example.com")
	sentAt := time.Date(2026, 11, 7, 8, 3, 0, 0, time.UTC)
	history := cloud.AutoReplyMessage{ID: "local-only", ConversationID: "hidden-thread", Fingerprint: "key-history", Direction: "candidate",
		MessageType: "resume", TextContent: "已发  简历\n请查收", CardContent: json.RawMessage(`{"summary":"简历附件.pdf","candidate_id":"hidden-cid"}`), PlatformSentAt: &sentAt}
	pending := cloud.AutoReplyMessage{ID: "legacy-key", PlatformMessageID: "platform-key", Fingerprint: "hidden-fingerprint", Direction: "candidate",
		MessageType: "text", TextContent: "请问薪资范围？", CardContent: json.RawMessage(`{"summary":"请问薪资范围？"}`), CreatedAt: sentAt, IngestedAt: sentAt}
	input.Messages, input.PendingMessages, input.BasedOnMessageKey = []cloud.AutoReplyMessage{history, pending}, []cloud.AutoReplyMessage{pending}, pending.ID
	result, err := initialToolMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	raw := result[len(result)-1].Content
	var dynamic replyPromptInput
	if err = json.Unmarshal([]byte(raw), &dynamic); err != nil {
		t.Fatal(err)
	}
	if len(dynamic.Messages) != 1 || len(dynamic.PendingMessages) != 1 || dynamic.BasedOnMessageKey != pending.PlatformMessageID ||
		strings.Count(raw, pending.TextContent) != 1 || strings.Contains(raw, `"based_on_message":`) {
		t.Fatal("待处理消息没有与历史分开，或被重复发送")
	}
	row := dynamic.Messages[0]
	if row.SourceRef != history.Fingerprint || row.TextContent != history.TextContent || row.Direction != history.Direction || row.MessageType != history.MessageType ||
		row.PlatformSentAt == nil || !row.PlatformSentAt.Equal(sentAt) || row.CardSummary != "简历附件.pdf" || dynamic.PendingMessages[0].PlatformSentAt != nil {
		t.Fatal("消息投影丢失原文、来源、方向、类型、卡片或平台时间")
	}
	for _, secret := range []string{"local-only", "hidden-thread", "hidden-cid", "hidden-fingerprint", "legacy-key", "ingested_at"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("消息投影带出内部字段 %s", secret)
		}
	}
	state := toolExecutionState{input: input}
	for _, message := range []replyPromptMessage{row, dynamic.PendingMessages[0]} {
		if err := state.validateConfirmationEvidence(cloud.CandidateConfirmationItem{Status: "matched", SourceRef: message.SourceRef, EvidenceText: message.TextContent}); err != nil {
			t.Fatalf("模型可见的来源键和原文未通过现有工具核验：%v", err)
		}
	}
}

// TestAutoReplyActionToolsExplainDirectReplyOrInternalHandoff 验证发送和转人工工具延续招聘方本人、技术异常不回复、登记后按配置签收的边界。
func TestAutoReplyActionToolsExplainDirectReplyOrInternalHandoff(t *testing.T) {
	descriptions := make(map[string]string)
	for _, definition := range autoReplyToolDefinitions() {
		descriptions[definition.Function.Name] = definition.Function.Description
	}
	if !strings.Contains(descriptions[toolSendMessages], "最多两条") ||
		!strings.Contains(descriptions[toolSendMessages], "改用 notify_hr") {
		t.Fatalf("send_messages 工具说明不完整：%s", descriptions[toolSendMessages])
	}
	if !strings.Contains(descriptions[toolNotifyHR], "内部通知") ||
		!strings.Contains(descriptions[toolNotifyHR], "登记人工待办后") || !strings.Contains(descriptions[toolNotifyHR], "技术问题或无关问题只作内部通知") {
		t.Fatalf("notify_hr 工具说明不完整：%s", descriptions[toolNotifyHR])
	}
}

// TestPrepareMessageRejectsRepliesOverTwoHundredRunes 验证自动回复发送工具拒绝超过二百字的长消息。
func TestPrepareMessageRejectsRepliesOverTwoHundredRunes(t *testing.T) {
	state := &toolExecutionState{}
	arguments, err := json.Marshal(sendMessagesToolArgs{Messages: []ReplyMessage{{Type: "answer", Content: strings.Repeat("招", maxAutoReplyMessageRunes+1)}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.prepareMessages(ai.ToolCall{
		Type: "function",
		Function: ai.ToolCallFunction{
			Name:      toolSendMessages,
			Arguments: string(arguments),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "超过200字") {
		t.Fatalf("prepareMessage() error = %v", err)
	}
}

// TestPrepareMessagesAllowsAnswerThenOnePendingConfirmation 验证 AI 可以先回答最新问题，再单独询问一条未确认条件。
func TestPrepareMessagesAllowsAnswerThenOnePendingConfirmation(t *testing.T) {
	state := &toolExecutionState{
		confirmations: []cloud.CandidateConfirmationItem{{
			ID: "confirmation-1", ItemType: "required", Content: "是否能接受出差", Status: "pending",
		}},
	}
	arguments, err := json.Marshal(sendMessagesToolArgs{Messages: []ReplyMessage{
		{Type: "answer", Content: "工作地点在成都哈～"},
		{Type: "confirmation", Content: "另外想确认下，你能接受偶尔出差吗？", ConfirmationItemID: "confirmation-1"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.prepareMessages(ai.ToolCall{Type: "function", Function: ai.ToolCallFunction{
		Name: toolSendMessages, Arguments: string(arguments),
	}}); err != nil {
		t.Fatal(err)
	}
	if len(state.pendingMessages) != 2 || state.pendingMessages[1].ConfirmationItemID != "confirmation-1" {
		t.Fatalf("两条消息没有正确保留：%+v", state.pendingMessages)
	}
}

// TestPrepareMessageKeepsSimpleInterestReplyShort 验证纯求职兴趣不会被 AI 扩展成岗位介绍或匹配判断。
func TestPrepareMessageKeepsSimpleInterestReplyShort(t *testing.T) {
	state := &toolExecutionState{input: ReplyContext{
		Messages: []cloud.AutoReplyMessage{{
			Fingerprint: "message-interest", Direction: "candidate",
			TextContent: "我目前正在看机会，对这个职位很感兴趣，希望可以详聊~",
		}},
		BasedOnMessageKey: "message-interest",
	}}
	arguments, err := json.Marshal(sendMessagesToolArgs{Messages: []ReplyMessage{{Type: "answer", Content: "岗位在成都，而且你的经历和岗位不匹配，想确认下你的意向。"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.prepareMessages(ai.ToolCall{Type: "function", Function: ai.ToolCallFunction{
		Name: toolSendMessages, Arguments: string(arguments),
	}}); err != nil {
		t.Fatal(err)
	}
	if len(state.pendingMessages) != 1 || state.pendingMessages[0].Content != "可以的，你想了解岗位哪方面呢？" {
		t.Fatalf("纯兴趣消息没有被简短承接：%+v", state.pendingMessages)
	}
}

// TestPrepareMessageDoesNotOverrideCandidateQuestion 验证候选人提出具体问题时仍保留 AI 的针对性回答。
func TestPrepareMessageDoesNotOverrideCandidateQuestion(t *testing.T) {
	state := &toolExecutionState{input: ReplyContext{
		Messages: []cloud.AutoReplyMessage{{
			Fingerprint: "message-question", Direction: "candidate",
			TextContent: "我对岗位感兴趣，薪资多少",
		}},
		BasedOnMessageKey: "message-question",
	}}
	arguments, err := json.Marshal(sendMessagesToolArgs{Messages: []ReplyMessage{{Type: "answer", Content: "薪资是5k到8k。"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.prepareMessages(ai.ToolCall{Type: "function", Function: ai.ToolCallFunction{
		Name: toolSendMessages, Arguments: string(arguments),
	}}); err != nil {
		t.Fatal(err)
	}
	if len(state.pendingMessages) != 1 || state.pendingMessages[0].Content != "薪资是5k到8k。" {
		t.Fatalf("具体问题的回答不该被覆盖：%+v", state.pendingMessages)
	}
}

// TestAIResponderRepairsArgumentsAndKeepsPromptBoundary 验证参数修正后只生成一条回复，且固定规则不混入动态岗位数据。
func TestAIResponderRepairsArgumentsAndKeepsPromptBoundary(t *testing.T) {
	state := &responderTestState{aiResponses: []string{
		toolCallResponse("call-bad", toolSendMessages, `{"messages":[{"type":"answer","content":""}]}`, 10),
		toolCallResponse("call-good", toolSendMessages, `{"messages":[{"type":"answer","content":"您好，薪资范围以岗位面谈为准。"}]}`, 11),
	}}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()

	responder := &AIResponder{AI: newAIClient(), Cloud: cloud.New(server.URL)}
	input := responderInput(server.URL)
	decision, err := responder.Reply(context.Background(), input)
	if err != nil {
		t.Fatalf("Reply() error = %v", err)
	}
	if len(decision.Messages) != 1 || decision.Messages[0].Content != "您好，薪资范围以岗位面谈为准。" || decision.ManualReason != "" {
		t.Fatalf("decision = %+v", decision)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.chatCallNumber != 2 || len(state.toolCalls) != 4 || len(state.finishedRuns) != 1 {
		t.Fatalf("chat=%d tool audits=%d finished=%d", state.chatCallNumber, len(state.toolCalls), len(state.finishedRuns))
	}
	if state.finishedRuns[0].Status != "completed" || state.finishedRuns[0].TokenUsage != 21 {
		t.Fatalf("finished run = %+v", state.finishedRuns[0])
	}
	var firstRequest struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err = json.Unmarshal(state.aiRequests[0], &firstRequest); err != nil {
		t.Fatal(err)
	}
	if len(firstRequest.Messages) < 2 || strings.Contains(firstRequest.Messages[0].Content, "AI应用开发工程师") {
		t.Fatalf("system 消息混入动态岗位：%+v", firstRequest.Messages)
	}
	if !strings.Contains(firstRequest.Messages[1].Content, "AI应用开发工程师") || !strings.Contains(firstRequest.Messages[1].Content, "薪资是多少") {
		t.Fatalf("user 消息缺少动态上下文：%s", firstRequest.Messages[1].Content)
	}
	var dynamic replyPromptInput
	if err = json.Unmarshal([]byte(firstRequest.Messages[1].Content), &dynamic); err != nil {
		t.Fatalf("动态上下文无法解析：%v", err)
	}
	if len(dynamic.PendingMessages) != 1 || dynamic.PendingMessages[0].TextContent != "薪资是多少" ||
		dynamic.PendingMessages[0].SourceRef != dynamic.BasedOnMessageKey || dynamic.BasedOnMessageKey != input.BasedOnMessageKey {
		t.Fatalf("本轮唯一消息没有按来源键定位：%+v", dynamic.PendingMessages)
	}
	if state.toolCalls[1].Status != "failed" || state.toolCalls[1].ErrorCode != "INVALID_TOOL_ARGUMENTS" {
		t.Fatalf("first tool finish = %+v", state.toolCalls[1])
	}
}

// TestInitialToolMessagesRejectsMissingBasedOnMessage 验证消息编号无法匹配时不会让 AI 猜要回复哪一条。
func TestInitialToolMessagesRejectsMissingBasedOnMessage(t *testing.T) {
	input := responderInput("https://example.com")
	input.BasedOnMessageKey = "missing-message"
	if _, err := initialToolMessages(input); err == nil || !strings.Contains(err.Error(), "没有在聊天上下文里找到") {
		t.Fatalf("缺失本轮消息时没有停止：%v", err)
	}
}

// TestAIResponderStopsAfterTwoArgumentRepairs 验证第三次错误参数不会继续调用 AI，而是安全转人工。
func TestAIResponderStopsAfterTwoArgumentRepairs(t *testing.T) {
	state := &responderTestState{aiResponses: []string{
		toolCallResponse("call-1", toolSendMessages, `{"messages":[{"type":"answer","content":""}]}`, 1),
		toolCallResponse("call-2", toolSendMessages, `{"messages":[{"type":"answer","content":""}]}`, 1),
		toolCallResponse("call-3", toolSendMessages, `{"messages":[{"type":"answer","content":""}]}`, 1),
	}}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()

	decision, err := (&AIResponder{AI: newAIClient(), Cloud: cloud.New(server.URL)}).Reply(
		context.Background(), responderInput(server.URL),
	)
	if err != nil {
		t.Fatalf("Reply() error = %v", err)
	}
	if decision.ReasonKey != "ai_tool_arguments" || !strings.Contains(decision.ManualReason, "参数") {
		t.Fatalf("decision = %+v", decision)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.chatCallNumber != 3 || len(state.toolCalls) != 6 {
		t.Fatalf("chat=%d tool audits=%d", state.chatCallNumber, len(state.toolCalls))
	}
}

// TestAIResponderStopsBeforeNinthTool 验证单轮返回四个工具时不会执行任何越限动作。
func TestAIResponderStopsBeforeNinthTool(t *testing.T) {
	state := &responderTestState{aiResponses: []string{manyToolCallsResponse(9)}}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()

	decision, err := (&AIResponder{AI: newAIClient(), Cloud: cloud.New(server.URL)}).Reply(
		context.Background(), responderInput(server.URL),
	)
	if err != nil {
		t.Fatalf("Reply() error = %v", err)
	}
	if decision.ReasonKey != "ai_tool_limit" || !strings.Contains(decision.ManualReason, "8次") {
		t.Fatalf("decision = %+v", decision)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.toolCalls) != 0 || len(state.finishedRuns) != 1 {
		t.Fatalf("tool audits=%d finished=%d", len(state.toolCalls), len(state.finishedRuns))
	}
}

// TestAIResponderRepairsPlainTextAction 验证普通文本会携带协议错误重新请求，并只执行修正后的标准工具动作。
func TestAIResponderRepairsPlainTextAction(t *testing.T) {
	state := &responderTestState{aiResponses: []string{
		`{"choices":[{"message":{"role":"assistant","content":"直接回复您好"}}]}`,
		toolCallResponse("call-good", toolSendMessages, `{"messages":[{"type":"answer","content":"您好，请问有什么想了解的？"}]}`, 2),
	}}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()

	decision, err := (&AIResponder{AI: newAIClient(), Cloud: cloud.New(server.URL)}).Reply(
		context.Background(), responderInput(server.URL),
	)
	if err != nil {
		t.Fatalf("Reply() error = %v", err)
	}
	if len(decision.Messages) != 1 || decision.Messages[0].Content != "您好，请问有什么想了解的？" {
		t.Fatalf("decision = %+v", decision)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.chatCallNumber != 2 || len(state.finishedRuns) != 1 || state.finishedRuns[0].Status != "completed" {
		t.Fatalf("finished runs = %+v", state.finishedRuns)
	}
	var repairRequest struct {
		Messages []ai.ToolMessage `json:"messages"`
	}
	if err = json.Unmarshal(state.aiRequests[1], &repairRequest); err != nil {
		t.Fatal(err)
	}
	lastMessage := repairRequest.Messages[len(repairRequest.Messages)-1]
	if lastMessage.Role != "user" || !strings.Contains(lastMessage.Content, "错误信息") || !strings.Contains(lastMessage.Content, "send_messages") {
		t.Fatalf("repair message = %+v", lastMessage)
	}
}

// TestAIResponderRejectsPlainTextAfterTwoRepairs 验证连续三次普通文本后才明确失败，且不会直接发送普通文本。
func TestAIResponderRejectsPlainTextAfterTwoRepairs(t *testing.T) {
	plainText := `{"choices":[{"message":{"role":"assistant","content":"直接回复您好"}}]}`
	state := &responderTestState{aiResponses: []string{plainText, plainText, plainText}}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()

	_, err := (&AIResponder{AI: newAIClient(), Cloud: cloud.New(server.URL)}).Reply(
		context.Background(), responderInput(server.URL),
	)
	if err == nil || !strings.Contains(err.Error(), "标准 tool_calls") {
		t.Fatalf("error = %v", err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.chatCallNumber != 3 || len(state.finishedRuns) != 1 || state.finishedRuns[0].Status != "failed" || state.finishedRuns[0].ErrorCode != "AI_TOOL_ACTION_MISSING" {
		t.Fatalf("chat=%d finished=%+v", state.chatCallNumber, state.finishedRuns)
	}
}

// TestAIResponderStructuresResumeLocally 验证附件提取与AI调用都在本地执行，云端不会收到原件或结构化请求。
func TestAIResponderStructuresResumeLocally(t *testing.T) {
	cloudCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudCalls++
		http.Error(w, "不允许上传简历原件", 500)
	}))
	defer server.Close()
	aiCalls := 0
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aiCalls++
		var request struct {
			Messages []ai.ToolMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Messages) != 2 || !strings.Contains(request.Messages[1].Content, "真实附件正文") || strings.Contains(request.Messages[0].Content, "真实附件正文") {
			t.Errorf("简历没有进入动态消息：%+v", request)
		}
		content, _ := json.Marshal(`{"candidate_name":"测试甲","phone":"13800138000","gender":"女","wechat":"candidate_wechat","educations":[{"school_name":"合成大学"}],"is_resume":true}`)
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, content)
	}))
	defer modelServer.Close()
	directory := t.TempDir()
	store, err := storage.Open(filepath.Join(directory, "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := cloud.New(server.URL)
	client.EnableLocalAutoReply(store, filepath.Join(directory, "private"))
	credentials := cloud.AgentCredentials{Token: "test-token", MachineID: "test-machine"}
	filePath := filepath.Join(directory, "resume.txt")
	if err = os.WriteFile(filePath, []byte("真实附件正文：测试甲，就读合成大学。"), 0600); err != nil {
		t.Fatal(err)
	}
	attachment, err := client.UploadAutoReplyAttachment(context.Background(), credentials, cloud.AutoReplyAttachmentUpload{ConversationID: "conversation-1", FilePath: filePath})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filePath); err != nil {
		t.Fatal(err)
	}
	result, err := (&AIResponder{AI: newAIClient(), Cloud: client}).StructureResume(context.Background(), ResumeStructureContext{
		Credentials: credentials, Conversation: cloud.AutoReplyConversation{ID: "conversation-1"}, Attachments: []cloud.StoredResumeAttachment{attachment},
		Resume: model.AutoReplyResumeBundle{OnlineResumeText: "页面补充事实"}, AIConfig: cloud.AIConfig{BaseURL: modelServer.URL, APIKey: "test", Model: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidate.CandidateName != "测试甲" || result.Candidate.Phone != "13800138000" || !strings.Contains(result.RawText, "真实附件正文") || cloudCalls != 0 || aiCalls != 1 {
		t.Fatalf("本地处理结果不正确：%+v 云端请求=%d AI请求=%d", result, cloudCalls, aiCalls)
	}
}

// TestAutoReplyResumeRequiresGroundedHistory 验证纯身份、模板、错判、旧模型与精简应届生的履历门槛。
func TestAutoReplyResumeRequiresGroundedHistory(t *testing.T) {
	for _, tc := range []struct {
		name, source, fields string
		want                 bool
	}{
		{"只有姓名", "测试甲", `{"candidate_name":"测试甲"}`, false},
		{"只有联系方式", "测试甲，13800138000，test@example.com", `{"candidate_name":"测试甲","phone":"13800138000","email":"test@example.com","is_resume":true}`, false},
		{"空模板", "个人简历 学校名称 公司名称 项目名称", `{"is_resume":true,"personal_description":"个人简历","educations":[{"school_name":"学校名称"}],"work_experiences":[{}],"project_experiences":[{"project_name":"项目名称"}]}`, false},
		{"模型补出的经历", "测试甲的简历", `{"is_resume":true,"educations":[{"school_name":"合成大学"}]}`, false},
		{"明确非简历", "合成大学", `{"is_resume":false,"educations":[{"school_name":"合成大学"}]}`, false},
		{"旧模型的应届生", "测试甲 就读合成大 学", `{"candidate_name":"测试甲","educations":[{"school_name":"合成大学"}]}`, true},
		{"应届生仅有明确学历", "测试甲 学历：本科", `{"candidate_name":"测试甲","education_level":"本科","is_resume":true}`, true},
		{"只有实习", "测试甲 在合成公司实习", `{"is_resume":true,"work_experiences":[{"company_name":"合成公司"}]}`, true},
		{"只有项目", "校园图书预约系统", `{"is_resume":true,"project_experiences":[{"project_name":"校园图书预约系统"}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var candidate cloud.LibraryCandidate
			if err := json.Unmarshal([]byte(tc.fields), &candidate); err != nil {
				t.Fatal(err)
			}
			if got := hasAutoReplyResumeEvidence(candidate, tc.source); got != tc.want {
				t.Fatalf("可用履历=%v，期望%v", got, tc.want)
			}
		})
	}
}

// TestWeakNewResumeKeepsPreviousProfile 使用正式附件解析和 SQLite 待办验证只有姓名的新附件不会替换历史简历或触发推荐。
func TestWeakNewResumeKeepsPreviousProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("意外请求 %s", r.URL.Path)
		}
		content, _ := json.Marshal(`{"candidate_name":"合成人选","is_resume":true}`)
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, content)
	}))
	defer server.Close()
	ctx := context.Background()
	flow, prepared := newReliabilityFlow(t, "")
	flow.Responder = &AIResponder{Cloud: flow.Cloud, AI: newAIClient()}
	prepared.Position.AI = cloud.AIConfig{BaseURL: server.URL, APIKey: "test", Model: "test"}
	cred := credentials(prepared)
	lookup := cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"}
	conversation, err := flow.Cloud.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PositionID: "position", PlatformID: lookup.PlatformID, PlatformAccountID: lookup.PlatformAccountID, PlatformThreadID: lookup.PlatformThreadID})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := flow.Cloud.EnsureLocalAutoReplyCandidate(ctx, cred, conversation)
	if err != nil {
		t.Fatal(err)
	}
	old := cloud.AutoReplyCandidateInput{PositionID: "position", LocalCandidateID: candidate.ID, PlatformID: lookup.PlatformID, PlatformAccountID: lookup.PlatformAccountID, PlatformCandidateID: "person", ResumeSourceMessageID: "old-card", StructuredCandidate: cloud.StructuredCandidate{CandidateName: "合成人选", RawText: "旧档原文", Educations: []cloud.CandidateEducation{{SchoolName: "合成大学"}}}}
	if _, err = flow.Cloud.SaveAutoReplyCandidate(ctx, cred, old); err != nil {
		t.Fatal(err)
	}
	const source = "new-card"
	card := cloud.AutoReplyMessage{PlatformMessageID: source, Direction: "candidate", MessageType: "resume"}
	if _, err = flow.Cloud.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: []cloud.AutoReplyMessage{card}, HistoryComplete: true}); err != nil {
		t.Fatal(err)
	}
	conversation, err = flow.Cloud.EnqueueAutoReplyResumeRetry(ctx, cred, conversation.ID, source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "new-resume.txt")
	if err = os.WriteFile(path, []byte("合成人选"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime := &autoReplyRuntimeStub{resumeBundle: model.AutoReplyResumeBundle{CandidateName: "合成人选", AttachmentPaths: []string{path}, ResumeSourceMessageID: source}}
	snapshot := model.AutoReplyConversationSnapshot{ResumeCardAvailable: true, ResumeSourceMessageID: source, PlatformCandidateID: "person", PlatformAccountID: lookup.PlatformAccountID, Messages: []model.ConversationMessage{{PlatformMessageID: source, MessageType: "resume"}}}
	state, err := flow.Cloud.AutoReplyCandidateState(ctx, cred, lookup)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = flow.ensureResume(ctx, prepared, runtime, cloud.AutoReplyPositionSnapshot{}, &conversation, snapshot, state, source, &shared.Stats{})
	if !errors.Is(err, errAutoReplyResumeDeferred) || !strings.Contains(err.Error(), "履历") {
		t.Fatalf("姓名被当作已结构化或不是预期延期：%v", err)
	}
	if err = flow.recordResumeRetryFailure(ctx, prepared, &conversation, snapshot, err); err != nil {
		t.Fatal(err)
	}
	state, err = flow.Cloud.AutoReplyCandidateState(ctx, cred, lookup)
	if err != nil || state.Candidate == nil || state.Conversation == nil || state.Conversation.ResumeRetry == nil {
		t.Fatalf("待办丢失：%v", err)
	}
	if state.Candidate.ResumeSourceMessageID != "old-card" || state.Candidate.RawText != old.RawText || len(state.Candidate.Educations) != 1 || state.Candidate.Educations[0].SchoolName != "合成大学" || len(state.Attachments) != 1 || state.Conversation.ResumeRetry.SourceMessageID != source || state.Conversation.ResumeRetry.LastError == "" {
		t.Fatal("新附件未保留或旧履历、核对待办被覆盖")
	}
	jobs, err := flow.Cloud.PendingAutoReplyRecommendations(ctx, cred)
	if err != nil || len(jobs) != 0 {
		t.Fatal("无效附件生成了推荐", jobs, err)
	}
}

// TestAIResponderBirthMonthRequiresBirthContext 验证实际解析链保留明确年月精度，但不把年龄、仅年份或经历日期当作生日。
func TestAIResponderBirthMonthRequiresBirthContext(t *testing.T) {
	for _, item := range []struct {
		name, original, parsed, want string
	}{
		{"年月", "出生年月：1990-05", "1990-05", "1990-05"},
		{"PDF分散标签", "出 生：1990-05", "1990-05", "1990-05"},
		{"模型遗漏", "出 生：1990-05", "", "1990-05"},
		{"月份冲突", "出生：1990-05", "1990-06", ""},
		{"不同简历日期冲突", "出生：1990-05\n生日：1991-05", "", ""},
		{"PDF全角空格", "出　生：1990-05", "1990-05", "1990-05"},
		{"中文年月", "出生日期：1990年5月", "1990-05", "1990-05"},
		{"英文生日", "Date of birth: 1990/05/12", "1990-05", "1990-05"},
		{"出生后缀", "1990年5月出生", "1990-05", "1990-05"},
		{"仅年份", "出生年份：1990", "1990", ""},
		{"年份不补月", "出生年份：1990", "1990-01", ""},
		{"空值", "没有填写生日", "", ""},
		{"非法月份", "出生年月：1990-13", "1990-13", ""},
		{"仅年龄", "年龄：30岁", "1996-01", ""},
		{"工作年月", "工作经历：1990-05至今", "1990-05", ""},
		{"年份与工作年月混合", "出生年份：1990\n工作经历：1990-05至今", "1990-05", ""},
	} {
		t.Run(item.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/v1/chat/completions" {
					t.Errorf("不应访问云端：%s", r.URL.Path)
				}
				content, _ := json.Marshal(map[string]any{"candidate_name": "合成人选", "birth_ym": item.parsed, "age": 30, "is_resume": true, "educations": []cloud.CandidateEducation{{SchoolName: "合成大学"}}})
				encoded, _ := json.Marshal(string(content))
				_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, encoded)
			}))
			defer server.Close()
			directory := t.TempDir()
			store, err := storage.Open(filepath.Join(directory, "local.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			client := cloud.New(server.URL)
			client.EnableLocalAutoReply(store, filepath.Join(directory, "private"))
			credentials := cloud.AgentCredentials{Token: "synthetic", MachineID: "synthetic"}
			ctx := context.Background()
			conversation, err := client.SaveAutoReplyConversation(ctx, credentials, cloud.AutoReplyConversation{PlatformID: "liepin", PlatformAccountID: "account", PlatformThreadID: "thread", PositionID: "position", CandidateName: "合成人选"})
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := client.EnsureLocalAutoReplyCandidate(ctx, credentials, conversation)
			if err != nil {
				t.Fatal(err)
			}
			conversation.CandidateID = candidate.ID
			path := filepath.Join(directory, "resume.txt")
			if err := os.WriteFile(path, []byte("合成简历\n毕业于合成大学\n"+item.original), 0600); err != nil {
				t.Fatal(err)
			}
			attachment, err := client.UploadAutoReplyAttachment(ctx, credentials, cloud.AutoReplyAttachmentUpload{ConversationID: conversation.ID, FilePath: path})
			if err != nil {
				t.Fatal(err)
			}
			result, err := (&AIResponder{AI: newAIClient(), Cloud: client}).StructureResume(context.Background(), ResumeStructureContext{
				Credentials: credentials, Conversation: conversation, Attachments: []cloud.StoredResumeAttachment{attachment},
				AIConfig: cloud.AIConfig{BaseURL: server.URL, APIKey: "synthetic", Model: "synthetic"},
			})
			precision := ""
			if item.want != "" {
				precision = "month"
			}
			if err != nil || calls != 1 || result.Candidate.BirthYM != item.want || result.BirthYMPrecision != precision || !strings.Contains(result.RawText, item.original) {
				t.Fatalf("年月=%q 精度=%q calls=%d err=%v", result.Candidate.BirthYM, result.BirthYMPrecision, calls, err)
			}
			// 复用刚取得的真实解析结果走保存链，不再发模型请求，直接覆盖此前 ensureResume 丢年月的触发点。
			flow := &Flow{Cloud: client, Responder: resumeResponderStub{structured: result}}
			prepared := shared.PreparedTask{Request: shared.StartRequest{Token: credentials.Token, PlatformAccountID: "account"}, MachineID: credentials.MachineID, Platform: model.Config{ID: "liepin"}}
			position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position", PlatformID: "liepin"}}
			_, _, err = flow.ensureResume(ctx, prepared, &autoReplyRuntimeStub{}, position, &conversation,
				model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformAccountID: "account"},
				cloud.AutoReplyCandidateState{Candidate: &candidate, Attachments: []cloud.StoredResumeAttachment{attachment}, HasResumeAttachment: true}, "message", &shared.Stats{})
			if err != nil {
				t.Fatal(err)
			}
			state, err := client.AutoReplyCandidateState(ctx, credentials, cloud.AutoReplyCandidateLookup{PlatformID: "liepin", PlatformAccountID: "account", PlatformThreadID: "thread"})
			if err != nil || state.Candidate == nil || state.Candidate.BirthYM != item.want || state.Candidate.BirthYMPrecision != precision || calls != 1 {
				t.Fatalf("解析年月在保存链丢失：candidate=%+v calls=%d err=%v", state.Candidate, calls, err)
			}
		})
	}
}

// serveHTTP 同时模拟标准 AI 接口和云端 AI 审计接口。
func (s *responderTestState) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/chat/completions":
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.aiRequests = append(s.aiRequests, append(json.RawMessage(nil), body...))
		index := s.chatCallNumber
		s.chatCallNumber++
		if index >= len(s.aiResponses) {
			http.Error(w, "missing ai response", http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, s.aiResponses[index])
	case "/api/auto-reply/agent/ai-runs/start":
		var run cloud.AutoReplyAIRun
		_ = json.NewDecoder(r.Body).Decode(&run)
		run.ID = "ai-run-1"
		run.Status = "running"
		writeResponderJSON(w, struct {
			Run cloud.AutoReplyAIRun `json:"ai_run"`
		}{run})
	case "/api/auto-reply/agent/tool-calls":
		var call cloud.AutoReplyToolCall
		_ = json.NewDecoder(r.Body).Decode(&call)
		call.ID = "audit-" + call.ToolCallID
		s.toolCalls = append(s.toolCalls, call)
		writeResponderJSON(w, struct {
			Call cloud.AutoReplyToolCall `json:"tool_call"`
		}{call})
	case "/api/auto-reply/agent/ai-runs/finish":
		var run cloud.AutoReplyAIRun
		_ = json.NewDecoder(r.Body).Decode(&run)
		s.finishedRuns = append(s.finishedRuns, run)
		writeResponderJSON(w, struct {
			Run cloud.AutoReplyAIRun `json:"ai_run"`
		}{run})
	case "/api/auto-reply/agent/resume-structure":
		var request cloud.AutoReplyResumeStructureRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		s.resumeStructureRequests = append(s.resumeStructureRequests, request)
		writeResponderJSON(w, map[string]any{
			"ok": true,
			"candidate": map[string]any{
				"candidate_name": "页面名字", "phone": "17607080935", "email": "user@example.com", "wechat": "candidate_wechat",
			},
			"gender": "女", "birth_ym_precision": "year_estimated", "wechat": "candidate_wechat",
			"attachment_text": "真实附件正文", "attachment_ids": []string{"attachment-1"}, "attempts": 1,
		})
	default:
		http.NotFound(w, r)
	}
}

// writeResponderJSON 输出测试云端接口的 JSON 响应。
func writeResponderJSON(w http.ResponseWriter, value any) {
	_ = json.NewEncoder(w).Encode(value)
}

// toolCallResponse 构造一轮非流式标准工具调用响应。
func toolCallResponse(id string, name string, arguments string, tokens int) string {
	encodedArguments, _ := json.Marshal(arguments)
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":%q,"type":"function","function":{"name":%q,"arguments":%s}}]}}],"usage":{"total_tokens":%d}}`, id, name, encodedArguments, tokens)
}

// manyToolCallsResponse 构造同一轮包含指定数量只读工具的响应。
func manyToolCallsResponse(count int) string {
	calls := make([]string, 0, count)
	for index := 0; index < count; index++ {
		calls = append(calls, fmt.Sprintf(`{"id":"call-%d","type":"function","function":{"name":"get_context","arguments":"{}"}}`, index+1))
	}
	return `{"choices":[{"message":{"role":"assistant","tool_calls":[` + strings.Join(calls, ",") + `]}}]}`
}

// responderInput 返回 AI 工具循环测试使用的最小完整业务上下文。
func responderInput(baseURL string) ReplyContext {
	return ReplyContext{
		TaskID: "task-1", Credentials: cloud.AgentCredentials{Token: "token", MachineID: "machine"},
		AIConfig: cloud.AIConfig{BaseURL: baseURL, APIKey: "key", Model: "tool-model"},
		Position: cloud.AutoReplyPositionSnapshot{
			Position:       cloud.AutoReplyPosition{ID: "position-1", Name: "AI应用开发工程师", PlatformID: "liepin"},
			Config:         cloud.PositionAutoReplyConfig{PositionID: "position-1", PositionDescription: "本科及以上"},
			CompanyProfile: cloud.CompanyProfile{ID: "company-1", Name: "测试公司"},
		},
		Conversation:      cloud.AutoReplyConversation{ID: "conversation-1", PositionID: "position-1", CandidateName: "陈女士"},
		Messages:          []cloud.AutoReplyMessage{{Fingerprint: "message-1", Direction: "candidate", MessageType: "text", TextContent: "薪资是多少"}},
		PageSnapshot:      model.AutoReplyConversationSnapshot{CandidateName: "陈女士"},
		BasedOnMessageKey: "message-1",
	}
}

// newAIClient 返回自动回复测试使用的真实 AI 客户端。
func newAIClient() *ai.Client {
	return ai.New()
}

// TestReplyFollowThroughChecksPublicEvidence 验证模型不能复述聊天里的虚假处理进度，或承诺没有配置的后续联系。
func TestReplyFollowThroughChecksPublicEvidence(t *testing.T) {
	var position cloud.AutoReplyPositionSnapshot
	position.Config.PositionDescription = "教学岗位，工作城市为成都。"
	for _, reply := range []string{"在成都哦～具体校区还在确认中。", "公司全称我暂时没有准确信息，后续有准确信息再和您同步～"} {
		if err := checkReplyFollowThrough(position, reply); err == nil {
			t.Fatalf("应当要求修正没有依据的进度或承诺：%q", reply)
		}
	}
	for _, reply := range []string{"这份岗位在成都。", "您问到的公司全称我暂时无法确认。", "之后再跟同事参加岗位培训。"} {
		if err := checkReplyFollowThrough(position, reply); err != nil {
			t.Fatalf("正常回复被拒：%q %v", reply, err)
		}
	}
	position.Config.PositionDescription += "具体校区还在确认中。"
	if err := checkReplyFollowThrough(position, "在成都，具体校区还在确认中。"); err != nil {
		t.Fatalf("岗位明确提供的原文应能回复：%v", err)
	}
}

// TestVerifiedPartialLocationReply 验证部分问题有明确答案时保留可证实的地点，不误报成全部转人工。
func TestVerifiedPartialLocationReply(t *testing.T) {
	input := ReplyContext{
		Position:        cloud.AutoReplyPositionSnapshot{Config: cloud.PositionAutoReplyConfig{PositionDescription: "高中数学老师，工作地点成都，主要工作为高中数学教学。"}},
		PendingMessages: []cloud.AutoReplyMessage{{Direction: "candidate", TextContent: "这个岗位在哪个城市，公司的全称是什么？"}},
	}
	draft := "岗位在成都哦～具体校区还在确认中。"
	if got := verifiedLocationAnswer(input, draft); got != "这份岗位的工作地点在成都。" {
		t.Fatalf("未保留有据可核的已问地点：%q", got)
	}
	state := toolExecutionState{input: input}
	args, err := json.Marshal(sendMessagesToolArgs{Messages: []ReplyMessage{{Type: "answer", Content: draft}}})
	if err != nil {
		t.Fatal(err)
	}
	call := ai.ToolCall{Type: "function", Function: ai.ToolCallFunction{Name: toolSendMessages, Arguments: string(args)}}
	if _, err = state.prepareMessages(call); err != nil || len(state.pendingMessages) != 1 || state.pendingMessages[0].Content != "这份岗位的工作地点在成都。" {
		t.Fatalf("正常工具入口未保留有据可核的地点：%+v %v", state.pendingMessages, err)
	}
	if verifiedLocationAnswer(input, "岗位在成都和北京哦～具体校区还在确认中。") != "" {
		t.Fatal("不应保留草稿擅自增加的地点")
	}
	input.PendingMessages[0].TextContent = "公司全称是什么？"
	if verifiedLocationAnswer(input, draft) != "" {
		t.Fatal("没有问地点时不能用城市回答")
	}
}

// TestModelHTTPUsesWritableConditionFields 捕获模型连续两次 HTTP 请求，核对岗位条件、确认项与成功回执的数据边界。
func TestModelHTTPUsesWritableConditionFields(t *testing.T) {
	const evidence = "我可以在成都工作"
	state := &responderTestState{aiResponses: []string{
		toolCallResponse("apply", toolUpsertConfirmations, `{"items":[{"id":"item","position_condition_id":"location","item_type":"confirm","content":"可在成都工作","status":"matched","status_reason":"候选人已确认","source_ref":"message-1","evidence_text":"我可以在成都工作"}]}`, 1),
		toolCallResponse("finish", toolFinishReview, `{}`, 1),
	}}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()
	input := responderInput(server.URL)
	input.ConditionsOnly = true
	input.Messages[0].TextContent = evidence
	input.Position.Config.Conditions = []cloud.PositionReplyCondition{
		{ID: "location", Type: "confirm", Content: "可在成都工作", Enabled: true, DedupeKey: "hidden-position-dedupe"},
		{ID: "disabled", Type: "required", Content: "禁用条件", DedupeKey: "hidden-disabled-dedupe"},
	}
	asked := time.Date(2026, 1, 27, 12, 0, 0, 0, time.UTC)
	input.ConfirmationItems = []cloud.CandidateConfirmationItem{{
		ID: "item", PositionID: input.Position.Position.ID, PositionConditionID: "location", ItemType: "confirm", Content: "可在成都工作", Status: "pending",
		SourceType: "position", DedupeKey: "hidden-item-dedupe", ReviewID: "hidden-review", AskCount: 2, LastAskedAt: &asked,
	}}
	result, err := (&AIResponder{AI: newAIClient(), Cloud: cloud.New(server.URL)}).Reply(context.Background(), input)
	if err != nil || len(result.ConfirmationUpdates) != 1 || result.ConfirmationUpdates[0].DedupeKey != "hidden-item-dedupe" {
		t.Fatalf("确认项没有保留用于实际存储的去重依据：%+v, %v", result, err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.aiRequests) != 2 {
		t.Fatalf("模型请求次数=%d", len(state.aiRequests))
	}
	var request struct {
		Messages []ai.ToolMessage `json:"messages"`
	}
	if err = json.Unmarshal(state.aiRequests[1], &request); err != nil {
		t.Fatal(err)
	}
	var seenUser, seenReceipt bool
	for _, msg := range request.Messages {
		if msg.Role != "user" && msg.Role != "tool" {
			continue
		}
		for _, forbidden := range []string{"dedupe_key", "hidden-", "禁用条件", "archived_at", "last_reviewed_at", "created_by_kind"} {
			if strings.Contains(msg.Content, forbidden) {
				t.Fatalf("%s 带出内部字段 %s", msg.Role, forbidden)
			}
		}
		switch msg.Role {
		case "user":
			var body struct {
				Position replyPromptPosition               `json:"position"`
				Items    []map[string]any                  `json:"confirmation_items"`
				History  map[string]confirmationAskHistory `json:"confirmation_ask_history"`
			}
			if err = json.Unmarshal([]byte(msg.Content), &body); err != nil {
				t.Fatal(err)
			}
			c := body.Position.Config.Conditions
			h := body.History["item"]
			if len(c) != 1 || c[0].ID != "location" || len(body.Items) != 1 || len(body.Items[0]) != 8 || body.Items[0]["id"] != "item" || h.AskCount != 2 || h.LastAskedAt == nil || !h.LastAskedAt.Equal(asked) {
				t.Fatalf("确认项、条件或只读追问记录错误：%+v", body)
			}
			seenUser = true
		case "tool":
			var body struct {
				Items []map[string]any `json:"items"`
			}
			if err = json.Unmarshal([]byte(msg.Content), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Items) != 1 || len(body.Items[0]) != 8 || body.Items[0]["source_ref"] != "message-1" || body.Items[0]["evidence_text"] != evidence || body.Items[0]["position_condition_id"] != "location" {
				t.Fatalf("回执字段或证据丢失：%+v", body)
			}
			seenReceipt = true
		}
	}
	if !seenUser || !seenReceipt {
		t.Fatal("缺少动态上下文或实际工具回执")
	}
	var args confirmationToolArgs
	if err = decodeToolArguments(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: `{"items":[{"dedupe_key":"extra"}]}`}}, &args); err == nil {
		t.Fatal("内部字段仍应被工具拒绝")
	}
}
