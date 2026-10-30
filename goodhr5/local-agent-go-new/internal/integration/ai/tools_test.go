// Package ai 本文件验证标准 OpenAI tool_calls 的普通响应、SSE 增量、能力错误和取消行为。
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

var testToolDefinitions = []ToolDefinition{{
	Type: "function",
	Function: ToolFunction{
		Name:        "send_message",
		Description: "发送一条招聘消息",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`),
	},
}}

// TestChatWithToolsReadsJSON 验证非流式响应中的工具名称、参数和 Token 用量。
func TestChatWithToolsReadsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["stream"] != true || request["tool_choice"] != "auto" {
			t.Fatalf("request = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"send_message","arguments":"{\"message\":\"您好\"}"}}]}}],"usage":{"total_tokens":23}}`)
	}))
	defer server.Close()

	result, err := New().ChatWithTools(context.Background(), testAIConfig(server.URL), ToolChatRequest{
		Messages: []ToolMessage{{Role: "system", Content: "只处理招聘消息"}},
		Tools:    testToolDefinitions,
	})
	if err != nil {
		t.Fatalf("ChatWithTools() error = %v", err)
	}
	if result.TokenUsage != 23 || len(result.Message.ToolCalls) != 1 {
		t.Fatalf("result = %+v", result)
	}
	call := result.Message.ToolCalls[0]
	if call.ID != "call-1" || call.Function.Name != "send_message" || call.Function.Arguments != `{"message":"您好"}` {
		t.Fatalf("tool call = %+v", call)
	}
}

// TestChatWithToolsJoinsStreamFragments 验证 SSE 会按 index 拼接被拆开的名称、参数和调用编号。
func TestChatWithToolsJoinsStreamFragments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-\",\"type\":\"function\",\"function\":{\"name\":\"send_\",\"arguments\":\"{\\\"mess\"}}]}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"1\",\"function\":{\"name\":\"message\",\"arguments\":\"age\\\":\\\"您好\\\"}\"}}]}}],\"usage\":{\"total_tokens\":31}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	result, err := New().ChatWithTools(context.Background(), testAIConfig(server.URL), ToolChatRequest{
		Messages: []ToolMessage{{Role: "user", Content: "候选人问薪资"}}, Tools: testToolDefinitions,
	})
	if err != nil {
		t.Fatalf("ChatWithTools() error = %v", err)
	}
	call := result.Message.ToolCalls[0]
	if call.ID != "call-1" || call.Function.Name != "send_message" || call.Function.Arguments != `{"message":"您好"}` || result.TokenUsage != 31 {
		t.Fatalf("result = %+v", result)
	}
}

// TestChatWithToolsReportsUnsupportedModel 验证模型拒绝 tools 参数时给出可执行的中文提示。
func TestChatWithToolsReportsUnsupportedModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"function calling is not supported"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := New().ChatWithTools(context.Background(), testAIConfig(server.URL), ToolChatRequest{
		Messages: []ToolMessage{{Role: "user", Content: "你好"}}, Tools: testToolDefinitions,
	})
	if err == nil || !strings.Contains(err.Error(), "不支持标准 tool_calls") {
		t.Fatalf("error = %v", err)
	}
}

// TestChatWithToolsHonorsCancellation 验证等待 AI 时取消上下文会立即结束请求。
func TestChatWithToolsHonorsCancellation(t *testing.T) {
	releaseServer := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-releaseServer:
		}
	}))
	defer server.Close()
	defer close(releaseServer)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := New().ChatWithTools(ctx, testAIConfig(server.URL), ToolChatRequest{
		Messages: []ToolMessage{{Role: "user", Content: "你好"}}, Tools: testToolDefinitions,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

// testAIConfig 返回工具客户端测试使用的最小 AI 配置。
func testAIConfig(baseURL string) cloud.AIConfig {
	return cloud.AIConfig{BaseURL: baseURL, APIKey: "test-key", Model: "test-model"}
}
