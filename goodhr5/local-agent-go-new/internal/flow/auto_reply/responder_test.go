// Package auto_reply 本文件验证 AI 工具循环的提示词边界、参数修正、调用上限和完整审计。
package auto_reply

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type responderTestState struct {
	mu             sync.Mutex
	aiResponses    []string
	aiRequests     []json.RawMessage
	toolCalls      []cloud.AutoReplyToolCall
	finishedRuns   []cloud.AutoReplyAIRun
	chatCallNumber int
}

// TestAutoReplyToolDefinitionsAreStableAndUnique 验证九个工具名称唯一且每份参数定义都是合法 JSON。
func TestAutoReplyToolDefinitionsAreStableAndUnique(t *testing.T) {
	definitions := autoReplyToolDefinitions()
	if len(definitions) != 9 {
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

// TestAIResponderRepairsArgumentsAndKeepsPromptBoundary 验证参数修正后只生成一条回复，且固定规则不混入动态岗位数据。
func TestAIResponderRepairsArgumentsAndKeepsPromptBoundary(t *testing.T) {
	state := &responderTestState{aiResponses: []string{
		toolCallResponse("call-bad", toolSendMessage, `{"message":""}`, 10),
		toolCallResponse("call-good", toolSendMessage, `{"message":"您好，薪资范围以岗位面谈为准。"}`, 11),
	}}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()

	responder := &AIResponder{AI: newAIClient(), Cloud: cloud.New(server.URL)}
	input := responderInput(server.URL)
	decision, err := responder.Reply(context.Background(), input)
	if err != nil {
		t.Fatalf("Reply() error = %v", err)
	}
	if decision.Reply != "您好，薪资范围以岗位面谈为准。" || decision.ManualReason != "" {
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
	if state.toolCalls[1].Status != "failed" || state.toolCalls[1].ErrorCode != "INVALID_TOOL_ARGUMENTS" {
		t.Fatalf("first tool finish = %+v", state.toolCalls[1])
	}
}

// TestAIResponderStopsAfterTwoArgumentRepairs 验证第三次错误参数不会继续调用 AI，而是安全转人工。
func TestAIResponderStopsAfterTwoArgumentRepairs(t *testing.T) {
	state := &responderTestState{aiResponses: []string{
		toolCallResponse("call-1", toolSendMessage, `{"message":""}`, 1),
		toolCallResponse("call-2", toolSendMessage, `{"message":""}`, 1),
		toolCallResponse("call-3", toolSendMessage, `{"message":""}`, 1),
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

// TestAIResponderStopsBeforeNinthTool 验证单轮返回九个工具时不会执行任何越限动作。
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

// TestAIResponderRejectsPlainTextAction 验证模型不用标准工具动作时明确失败，不会把普通文本直接发给候选人。
func TestAIResponderRejectsPlainTextAction(t *testing.T) {
	state := &responderTestState{aiResponses: []string{`{"choices":[{"message":{"role":"assistant","content":"直接回复您好"}}]}`}}
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
	if len(state.finishedRuns) != 1 || state.finishedRuns[0].Status != "failed" || state.finishedRuns[0].ErrorCode != "AI_TOOL_ACTION_MISSING" {
		t.Fatalf("finished runs = %+v", state.finishedRuns)
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
