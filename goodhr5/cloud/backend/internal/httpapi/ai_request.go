// 本文件统一服务器向 AI 上游的请求、JSON 格式检查和三次重试；客户端只接收最终结果，不承担上游重发。
package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const serverAIMaxAttempts = 4

// serverAIRequestGuardKey 绑定后台任务每次重发前的取消、权限与引用复核。
type serverAIRequestGuardKey struct{}

// withServerAIGuard 在当前后台上下文绑定只读复核，不改变客户端协议。
func withServerAIGuard(ctx context.Context, guard func() error) context.Context {
	return context.WithValue(ctx, serverAIRequestGuardKey{}, guard)
}

// serverAIRequestError 只携带固定错误类别与实际次数，不保存密钥、请求内容或模型原文。
type serverAIRequestError struct {
	Attempts, Status int
	Reason           string
	Cause            error
}

// Error 返回适合交给调用方的固定中文说明。
func (e *serverAIRequestError) Error() string {
	return fmt.Sprintf("AI 已尝试%d次，还是没拿到可用结果，请稍后再试", e.Attempts)
}

// Unwrap 保留取消等标准错误标识。
func (e *serverAIRequestError) Unwrap() error { return e.Cause }

// serverAIRequestOptions 只为服务器原计费与预算能力提供单次尝试回调，不包含业务动作。
type serverAIRequestOptions struct {
	Before          func(int) error
	Failed          func(int, *http.Response, []byte) error
	Fallback        *BuiltinAIFallbackConfig
	ClientRequestID string
	AnalysisGroupID string
	Model           string
	Observe         func(serverAIAttemptEvent)
}

// aiWireResult 汇总一次模型响应中的内容、工具参数和真实用量，仅在本次请求内存中使用。
type aiWireResult struct {
	Content            string
	Arguments          []string
	Prompt, Completion int
	HasChoices         bool
}

// aiOutputMode 从固定系统合同或 response_format 判断 JSON 要求；不把简历和聊天里的指令当作协议。
func aiOutputMode(payload []byte) string {
	var request struct {
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(payload, &request) != nil {
		return ""
	}
	if request.ResponseFormat.Type == "json_object" || request.ResponseFormat.Type == "json_schema" {
		return "json"
	}
	for _, message := range request.Messages {
		if message.Role != "system" && message.Role != "developer" {
			continue
		}
		var text string
		if json.Unmarshal(message.Content, &text) != nil {
			continue
		}
		lower := strings.ToLower(text)
		compact := strings.Join(strings.Fields(lower), "")
		if strings.Contains(compact, "不要返回json") || strings.Contains(compact, "不返回json") || strings.Contains(compact, "不要json") || strings.Contains(compact, "禁止json") {
			continue
		}
		if strings.Contains(lower, "json") && (strings.Contains(text, "每行") || strings.Contains(text, "JSON事件流")) {
			return "json_lines"
		}
		if strings.Contains(lower, "json") || strings.Contains(lower, "score") && strings.Contains(lower, "reason") {
			return "json"
		}
	}
	return ""
}

// readAIWire 读取 JSON 或 SSE 的正文、工具参数和 usage；即便读取中断，也可保留已收到的真实用量。
func readAIWire(body []byte) (aiWireResult, error) {
	var result aiWireResult
	var content strings.Builder
	// accept 仅解析上游标准字段，忽略服务商伪造的 GoodHR 私有信号。
	accept := func(raw []byte, stream bool) error {
		var chunk struct {
			Error   json.RawMessage `json:"error"`
			Choices []struct {
				Message struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Function struct {
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int `json:"index"`
						Function struct {
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(raw, &chunk) != nil {
			return errors.New("response_json_invalid")
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return errors.New("upstream_error")
		}
		if chunk.Usage.Prompt > 0 || chunk.Usage.Completion > 0 {
			result.Prompt, result.Completion = chunk.Usage.Prompt, chunk.Usage.Completion
		}
		if len(chunk.Choices) == 0 {
			return nil
		}
		result.HasChoices = true
		choice := chunk.Choices[0]
		if !stream {
			result.Content = choice.Message.Content
			for _, call := range choice.Message.ToolCalls {
				result.Arguments = append(result.Arguments, call.Function.Arguments)
			}
			return nil
		}
		content.WriteString(choice.Delta.Content)
		for _, call := range choice.Delta.ToolCalls {
			if call.Index < 0 || call.Index > 128 {
				return errors.New("tool_index_invalid")
			}
			for len(result.Arguments) <= call.Index {
				result.Arguments = append(result.Arguments, "")
			}
			result.Arguments[call.Index] += call.Function.Arguments
		}
		return nil
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		err := accept(trimmed, false)
		return result, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 4096), maxAICompatibleBodyBytes)
	var protocol error
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		if err := accept([]byte(data), true); err != nil {
			protocol = err
		}
	}
	if err := scanner.Err(); err != nil {
		return result, errors.New("stream_invalid")
	}
	result.Content = content.String()
	return result, protocol
}

// validateAIWire 只检查通用响应与 JSON 语法，不承担评分、岗位或简历字段等业务判断。
func validateAIWire(body []byte, mode string, chat bool) error {
	if !chat {
		if !json.Valid(bytes.TrimSpace(body)) {
			return errors.New("response_json_invalid")
		}
		return nil
	}
	result, err := readAIWire(body)
	if err != nil {
		return err
	}
	if !result.HasChoices || strings.TrimSpace(result.Content) == "" && len(result.Arguments) == 0 {
		return errors.New("response_empty")
	}
	for _, arguments := range result.Arguments {
		if !json.Valid([]byte(arguments)) {
			return errors.New("tool_json_invalid")
		}
	}
	if mode == "" || strings.TrimSpace(result.Content) == "" && len(result.Arguments) > 0 {
		return nil
	}
	if mode == "json_lines" {
		for _, line := range strings.Split(result.Content, "\n") {
			if strings.TrimSpace(line) != "" && !json.Valid([]byte(line)) {
				return errors.New("model_json_invalid")
			}
		}
		return nil
	}
	if !json.Valid([]byte(strings.TrimSpace(result.Content))) {
		return errors.New("model_json_invalid")
	}
	return nil
}

// doServerAIRequest 在服务器使用同一四次预算完成上游请求和 JSON 检查，失败正文不会提前交给客户端。
func doServerAIRequest(client *http.Client, request *http.Request, options ...serverAIRequestOptions) (*http.Response, error) {
	// 请求本站兼容代理时，其内部已经统一重试；此处不得再套一层次数。
	if request.URL.Path == "/api/ai-compatible/v1/chat/completions" && isCurrentSiteAIURL(request.URL.String()) {
		return client.Do(request)
	}
	if request.GetBody == nil {
		return nil, &serverAIRequestError{Reason: "request_not_replayable"}
	}
	source, err := request.GetBody()
	if err != nil {
		return nil, err
	}
	payload, err := io.ReadAll(source)
	source.Close()
	if err != nil {
		return nil, err
	}
	mode := aiOutputMode(payload)
	chat := strings.HasSuffix(strings.TrimRight(request.URL.Path, "/"), "/chat/completions")
	var option serverAIRequestOptions
	if len(options) > 0 {
		option = options[0]
	}
	if option.Model == "" {
		option.Model = aiModelFromBody(payload, "")
	}
	for attempt := 1; attempt <= serverAIMaxAttempts; attempt++ {
		if err = request.Context().Err(); err != nil {
			return nil, err
		}
		if guard, ok := request.Context().Value(serverAIRequestGuardKey{}).(func() error); ok {
			if err = guard(); err != nil {
				return nil, err
			}
		}
		if option.Before != nil {
			if err = option.Before(attempt); err != nil {
				return nil, err
			}
		}
		cloned := request.Clone(request.Context())
		cloned.Body, err = request.GetBody()
		if err != nil {
			return nil, err
		}
		provider, actualModel := "primary", option.Model
		if attempt > 1 && usableAIFallback(option.Fallback) {
			cloned, err = prepareAIFallbackRequest(cloned, payload, option.Fallback)
			if err != nil {
				return nil, err
			}
			provider, actualModel = "fallback", option.Fallback.Model
		}
		started := time.Now()
		operation := "ai_request"
		if chat {
			operation = "chat_completions"
		}
		event := serverAIAttemptEvent{Layer: "server", Operation: operation, Event: "start", ClientRequestID: safeAITraceID(option.ClientRequestID), AnalysisGroupID: safeAITraceID(option.AnalysisGroupID), ServerAttempt: attempt, MaxAttempts: serverAIMaxAttempts, Provider: provider, Model: safeAIModelName(actualModel), StartedAt: started.UTC().Format(time.RFC3339Nano)}
		observeServerAIAttempt(option, event)
		var completed time.Time
		finish := func(response *http.Response, reason, stop string, retry bool, backoff time.Duration) {
			if completed.IsZero() {
				completed = time.Now()
			}
			event.Event, event.FinishedAt, event.ElapsedMS = "end", completed.UTC().Format(time.RFC3339Nano), completed.Sub(started).Milliseconds()
			event.Result, event.Reason, event.StopReason, event.WillRetry = "failed", reason, stop, retry
			if reason == "success" {
				event.Result = "success"
			}
			if retry {
				event.BackoffMS = backoff.Milliseconds()
			}
			if response != nil {
				credentials := []string{bearerToken(cloned.Header.Get("Authorization")), bearerToken(request.Header.Get("Authorization"))}
				if option.Fallback != nil {
					credentials = append(credentials, option.Fallback.APIKey)
				}
				event.HTTPStatus, event.ProviderRequestID = response.StatusCode, aiProviderRequestID(response.Header, credentials...)
			}
			observeServerAIAttempt(option, event)
		}
		response, callErr := client.Do(cloned)
		var raw []byte
		status := 0
		reason := "transport_error"
		delay := time.Duration(attempt) * 500 * time.Millisecond
		if response != nil {
			if response.Header == nil {
				response.Header = make(http.Header)
			}
			status = response.StatusCode
			raw, err = io.ReadAll(io.LimitReader(response.Body, maxAICompatibleBodyBytes+1))
			response.Body.Close()
			response.Body = io.NopCloser(bytes.NewReader(raw))
			if callErr == nil {
				callErr = err
				if err != nil {
					reason = "read_body"
					if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
						reason = "read_stream"
					}
				}
			}
			if len(raw) > maxAICompatibleBodyBytes {
				callErr = errors.New("response_too_large")
				reason = "response_too_large"
			}
			if callErr == nil && (status < 200 || status >= 300) {
				if status != 408 && status != 429 && status < 500 {
					response.Header.Set("X-GoodHR-Upstream-Attempts", strconv.Itoa(attempt))
					finish(response, "http_status", "", false, 0)
					return response, nil
				}
				callErr = errors.New("upstream_status")
				reason = "http_status"
			}
			if callErr == nil {
				callErr = validateAIWire(raw, mode, chat)
				if callErr != nil {
					reason = callErr.Error()
				}
			}
			if value, parseErr := strconv.Atoi(response.Header.Get("Retry-After")); parseErr == nil && value > 0 {
				delay = max(delay, time.Duration(value)*time.Second)
			} else if until, parseErr := http.ParseTime(response.Header.Get("Retry-After")); parseErr == nil {
				delay = max(delay, time.Until(until))
			}
		}
		completed = time.Now()
		if callErr == nil {
			response.Header.Set("X-GoodHR-Upstream-Attempts", strconv.Itoa(attempt))
			finish(response, "success", "", false, 0)
			return response, nil
		}
		observedReason := aiProxyReason(callErr, reason)
		if option.Failed != nil {
			if err = option.Failed(attempt, response, raw); err != nil {
				stop := "retry_stopped"
				var failure *serverAIRequestError
				if errors.As(err, &failure) && (failure.Reason == "billing_failed" || failure.Reason == "billing_total_invalid") {
					stop = failure.Reason
				}
				finish(response, observedReason, stop, false, 0)
				return nil, err
			}
		}
		log.Printf("[AI服务器重试] attempt=%d max_attempts=%d http_status=%d reason=%s", attempt, serverAIMaxAttempts, status, reason)
		if request.Context().Err() != nil {
			finish(response, observedReason, aiProxyReason(request.Context().Err(), "cancelled"), false, 0)
			return nil, request.Context().Err()
		}
		if attempt == serverAIMaxAttempts {
			finish(response, observedReason, "", false, 0)
			return nil, &serverAIRequestError{Attempts: attempt, Status: status, Reason: reason, Cause: callErr}
		}
		finish(response, observedReason, "", true, delay)
		timer := time.NewTimer(delay)
		select {
		case <-request.Context().Done():
			timer.Stop()
			return nil, request.Context().Err()
		case <-timer.C:
		}
	}
	return nil, errors.New("AI 请求未完成")
}
