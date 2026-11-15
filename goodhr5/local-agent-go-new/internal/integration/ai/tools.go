// Package ai 本文件提供标准 OpenAI tool_calls 请求、普通响应和 SSE 增量解析能力。
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// ToolFunction 表示一个标准 OpenAI 函数工具定义。
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolDefinition 表示发送给模型的一个函数工具。
type ToolDefinition struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolCallFunction 表示模型返回的函数名称和 JSON 参数。
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall 表示模型返回的一次标准工具调用。
type ToolCall struct {
	completion *streamCompletion
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Function   ToolCallFunction `json:"function"`
}

// ToolMessage 表示工具循环中的 system、user、assistant 或 tool 消息。
type ToolMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// WithArgumentParseDiagnostic 仅在调用方真正发现参数 JSON 解析失败时补充已观察到的流结束证据。
func (c ToolCall) WithArgumentParseDiagnostic(err error) error {
	return c.completion.parseError(err)
}

// ToolChatRequest 表示一次带标准函数工具的 AI 请求。
type ToolChatRequest struct {
	Messages       []ToolMessage
	Tools          []ToolDefinition
	EnableThinking bool
}

// ToolChatResult 表示一次 AI 工具轮次的完整助手消息和用量。
type ToolChatResult struct {
	Message    ToolMessage `json:"message"`
	TokenUsage int         `json:"token_usage"`
}

type toolChatRequestBody struct {
	Model          string           `json:"model"`
	Messages       []ToolMessage    `json:"messages"`
	Tools          []ToolDefinition `json:"tools"`
	ToolChoice     string           `json:"tool_choice"`
	Temperature    float64          `json:"temperature"`
	Stream         bool             `json:"stream"`
	EnableThinking *bool            `json:"enable_thinking,omitempty"`
}

type toolChatResponseBody struct {
	Choices []struct {
		Message ToolMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type toolStreamChunk struct {
	Choices []struct {
		FinishReason json.RawMessage `json:"finish_reason"`
		Delta        struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// ChatWithTools 调用支持标准 tool_calls 的 OpenAI 兼容接口，临时错误最多重试三次。
func (c *Client) ChatWithTools(ctx context.Context, cfg cloud.AIConfig, request ToolChatRequest) (ToolChatResult, error) {
	if err := c.Ready(cfg); err != nil {
		return ToolChatResult{}, err
	}
	if len(request.Messages) == 0 || len(request.Tools) == 0 {
		return ToolChatResult{}, fmt.Errorf("AI 工具请求缺少消息或工具定义")
	}
	body := toolChatRequestBody{
		Model: cfg.Model, Messages: request.Messages, Tools: request.Tools,
		ToolChoice: "auto", Temperature: cfg.Temperature, Stream: true,
	}
	if !request.EnableThinking {
		disabled := false
		body.EnableThinking = &disabled
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return ToolChatResult{}, fmt.Errorf("编码 AI 工具请求失败：%w", err)
	}
	endpoint := chatCompletionsURL(cfg.BaseURL)
	return runAIRequest(ctx, "AI 工具调用", cfg.Model, func(attempt int) (ToolChatResult, error) {
		result, err := c.doToolChatRequest(ctx, endpoint, cfg.APIKey, encoded, int64(attempt))
		if err == nil {
			err = validateToolChatResult(result, request.Tools)
		}
		return result, err
	})
}

// doToolChatRequest 执行一次工具请求并按响应类型选择 JSON 或 SSE 解析。
func (c *Client) doToolChatRequest(ctx context.Context, endpoint string, apiKey string, payload []byte, attempts ...int64) (result ToolChatResult, runErr error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ToolChatResult{}, &ServiceError{Message: "创建 AI 工具请求失败：" + err.Error(), Fatal: true, Cause: err}
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream, application/json")
	request.Header.Set("X-GoodHR-Billing", "1")
	var attempt int64 = 1
	if len(attempts) > 0 {
		attempt = attempts[0]
	}
	observation, request := startAIAttempt(ctx, request, apiKey, "AI 工具调用", attempt, c.http.Timeout)
	defer func() { runErr = observation.failure(runErr) }()
	billing := newBillingObserver(ctx, apiKey)
	billing.diagnostic = observation
	defer billing.finish()
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ToolChatResult{}, ctx.Err()
		}
		return ToolChatResult{}, &ServiceError{Message: "连接 AI 失败：" + err.Error(), Retryable: true, Fatal: true, Cause: err}
	}
	observation.headers(response)
	defer response.Body.Close()
	billing.enabled = response.Header.Get("X-GoodHR-Billing") == "1"
	if billing.enabled {
		billing.billing.Status = "unavailable"
	}
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		if toolCallsUnsupported(response.StatusCode, string(body)) {
			return ToolChatResult{}, &ServiceError{
				StatusCode: response.StatusCode,
				Message:    "当前 AI 模型不支持标准 tool_calls，请在后台换一个支持工具调用的模型",
				Fatal:      true,
			}
		}
		return ToolChatResult{}, newHTTPServiceError(response.StatusCode, string(body), response.Header.Get("Retry-After"))
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		return readToolChatStream(response.Body, billing)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return ToolChatResult{}, fmt.Errorf("读取 AI 工具响应失败：%w", err)
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "data:") {
		return readToolChatStream(bytes.NewReader(body), billing)
	}
	billing.observe(body)
	return readToolChatJSON(body)
}

// readToolChatJSON 解析非流式标准工具响应。
func readToolChatJSON(body []byte) (ToolChatResult, error) {
	var response toolChatResponseBody
	if err := json.Unmarshal(body, &response); err != nil {
		return ToolChatResult{}, fmt.Errorf("解析 AI 工具响应失败：%w", err)
	}
	if len(response.Choices) == 0 {
		return ToolChatResult{}, fmt.Errorf("AI 工具响应没有返回选择结果")
	}
	message := response.Choices[0].Message
	message.Role = firstAIMessageRole(message.Role)
	if strings.TrimSpace(message.Content) == "" && len(message.ToolCalls) == 0 {
		return ToolChatResult{}, fmt.Errorf("AI 没有返回文字或工具调用")
	}
	return ToolChatResult{Message: message, TokenUsage: response.Usage.TotalTokens}, nil
}

// readToolChatStream 累积 SSE 中可能被拆开的工具名称和 JSON 参数。
func readToolChatStream(reader io.Reader, billing ...*billingObserver) (ToolChatResult, error) {
	observation := streamObservation(billing)
	if observation != nil {
		observation.stream = true
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var content strings.Builder
	calls := make(map[int]*ToolCall)
	tokenUsage := 0
	endMarked := false
	var protocolErr error
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			endMarked = true
		}
		if data == "" || data == "[DONE]" {
			continue
		}
		if len(billing) > 0 {
			billing[0].observe([]byte(data))
		}
		var chunk toolStreamChunk
		if json.Unmarshal([]byte(data), &chunk) != nil {
			protocolErr = fmt.Errorf("AI 工具流式响应不是有效 JSON")
			continue
		}
		var signal struct {
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(data), &signal) == nil && len(signal.Error) > 0 && string(signal.Error) != "null" {
			protocolErr = fmt.Errorf("AI 工具流式响应返回服务错误")
		}
		if chunk.Usage.TotalTokens > 0 {
			tokenUsage = chunk.Usage.TotalTokens
		}
		for _, choice := range chunk.Choices {
			if streamFinishReason(choice.FinishReason) {
				endMarked = true
			}
			if choice.Delta.Content != "" || len(choice.Delta.ToolCalls) > 0 {
				observation.gotContent()
			}
			content.WriteString(choice.Delta.Content)
			for _, delta := range choice.Delta.ToolCalls {
				call := calls[delta.Index]
				if call == nil {
					call = &ToolCall{Type: "function"}
					calls[delta.Index] = call
				}
				call.ID += delta.ID
				if delta.Type != "" {
					call.Type = delta.Type
				}
				call.Function.Name += delta.Function.Name
				call.Function.Arguments += delta.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ToolChatResult{}, &ServiceError{Message: "AI 工具流式响应中断：" + err.Error(), Retryable: true, Fatal: true, Cause: err}
	}
	if err := observation.streamError(); err != nil {
		return ToolChatResult{}, err
	}
	if protocolErr != nil {
		return ToolChatResult{}, protocolErr
	}
	indexes := make([]int, 0, len(calls))
	for index := range calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	resultCalls := make([]ToolCall, 0, len(indexes))
	completion := observation.streamEndEvidence(endMarked)
	for _, index := range indexes {
		calls[index].completion = completion
		resultCalls = append(resultCalls, *calls[index])
	}
	if strings.TrimSpace(content.String()) == "" && len(resultCalls) == 0 {
		return ToolChatResult{}, fmt.Errorf("AI 没有返回文字或工具调用，当前模型可能不支持标准 tool_calls")
	}
	return ToolChatResult{
		Message:    ToolMessage{Role: "assistant", Content: content.String(), ToolCalls: resultCalls},
		TokenUsage: tokenUsage,
	}, nil
}

// firstAIMessageRole 为缺少角色的兼容响应补齐 assistant。
func firstAIMessageRole(role string) string {
	if strings.TrimSpace(role) == "" {
		return "assistant"
	}
	return role
}

// toolCallsUnsupported 判断模型或兼容接口是否明确拒绝标准工具调用参数。
func toolCallsUnsupported(statusCode int, body string) bool {
	if statusCode != http.StatusBadRequest && statusCode != http.StatusUnprocessableEntity {
		return false
	}
	lower := strings.ToLower(body)
	mentionsTool := strings.Contains(lower, "tool_calls") ||
		strings.Contains(lower, "tool call") ||
		strings.Contains(lower, "tools") ||
		strings.Contains(lower, "function calling")
	rejectsTool := strings.Contains(lower, "not support") ||
		strings.Contains(lower, "unsupported") ||
		strings.Contains(lower, "unknown") ||
		strings.Contains(lower, "不支持")
	return mentionsTool && rejectsTool
}

// validateToolChatResult 在公共 AI 边界校验工具名称和 JSON 参数，坏参数不得交给业务工具执行。
func validateToolChatResult(result ToolChatResult, tools []ToolDefinition) error {
	seen := map[string]bool{}
	for _, call := range result.Message.ToolCalls {
		allowed := false
		for _, tool := range tools {
			if tool.Function.Name == call.Function.Name {
				allowed = true
				break
			}
		}
		raw := strings.TrimSpace(call.Function.Arguments)
		if !allowed || call.ID == "" || seen[call.ID] || call.Type != "function" || !json.Valid([]byte(raw)) || !strings.HasPrefix(raw, "{") {
			return call.WithArgumentParseDiagnostic(fmt.Errorf("AI 工具名称、编号或 JSON 参数不完整"))
		}
		seen[call.ID] = true
	}
	return nil
}
