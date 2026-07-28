// Package ai 文件作用：调用 OpenAI 兼容接口，支持文本、图片、SSE 流式响应和临时故障重试。
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// Client 是无共享任务状态的 OpenAI 兼容客户端。
type Client struct {
	http *http.Client
}

// Decision 表示候选人是否适合打招呼。
type Decision struct {
	Accepted bool    `json:"accepted"`
	Score    float64 `json:"score"`
	Reason   string  `json:"reason"`
}

// ServiceError 表示 AI 网络或服务端错误及其重试策略。
type ServiceError struct {
	StatusCode int
	Message    string
	Retryable  bool
	Fatal      bool
	RetryAfter time.Duration
	Cause      error
}

// Error 返回适合任务日志展示的 AI 错误。
func (e *ServiceError) Error() string {
	if e == nil {
		return "AI 服务请求失败"
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("AI 服务请求失败，状态码 %d", e.StatusCode)
	}
	if e.Cause != nil {
		return "AI 服务请求失败：" + e.Cause.Error()
	}
	return "AI 服务请求失败"
}

// Unwrap 返回 AI 服务错误的底层原因。
func (e *ServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsPositionStoppingError 判断 AI 错误是否需要停止整个岗位任务。
func IsPositionStoppingError(err error) bool {
	var serviceErr *ServiceError
	return errors.As(err, &serviceErr) && serviceErr.Fatal
}

// New 创建 AI 客户端。
func New() *Client {
	return &Client{http: &http.Client{Timeout: 180 * time.Second}}
}

// Ready 检查当前任务 AI 配置是否完整。
func (c *Client) Ready(cfg cloud.AIConfig) error {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return fmt.Errorf("AI 接口地址还没配置")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fmt.Errorf("AI 密钥还没配置")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("AI 模型还没配置")
	}
	return nil
}

// EvaluateCandidate 根据岗位和候选人详情生成强类型文本判断。
func (c *Client) EvaluateCandidate(ctx context.Context, cfg cloud.AIConfig, position cloud.PositionSnapshot, candidate model.Candidate, detail model.CandidateDetail) (Decision, error) {
	system := candidateSystemPrompt(cfg, position)
	user := candidateUserPrompt(position, candidate, detail)
	content, err := c.chat(ctx, cfg, []chatMessage{
		textChatMessage("system", system),
		textChatMessage("user", user),
	})
	if err != nil {
		return Decision{}, err
	}
	return parseDecision(content, decisionThreshold(cfg, position))
}

// EvaluateCandidateVision 根据候选人详情截图和文本生成强类型图片判断。
func (c *Client) EvaluateCandidateVision(ctx context.Context, cfg cloud.AIConfig, position cloud.PositionSnapshot, candidate model.Candidate, detail model.CandidateDetail, images [][]byte) (Decision, error) {
	if len(images) == 0 {
		return Decision{}, fmt.Errorf("AI 图片详情为空")
	}
	parts := []contentPart{{Type: "text", Text: candidateUserPrompt(position, candidate, detail)}}
	for _, image := range images {
		if len(image) == 0 {
			continue
		}
		parts = append(parts, contentPart{
			Type:     "image_url",
			ImageURL: &imageURL{URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(image)},
		})
	}
	if len(parts) == 1 {
		return Decision{}, fmt.Errorf("AI 图片详情没有可读取的图片")
	}
	content, err := c.chat(ctx, cfg, []chatMessage{
		textChatMessage("system", candidateSystemPrompt(cfg, position)),
		{Role: "user", Content: messageContent{Parts: parts}},
	})
	if err != nil {
		return Decision{}, err
	}
	return parseDecision(content, decisionThreshold(cfg, position))
}

// GenerateReply 根据会话上下文生成一条简短回复。
func (c *Client) GenerateReply(ctx context.Context, cfg cloud.AIConfig, position cloud.PositionSnapshot, conversation model.Conversation, history string) (string, error) {
	system := strings.TrimSpace(position.AIOptions.ReplyPrompt)
	if system == "" {
		system = strings.TrimSpace(cfg.ReplySystemPrompt)
	}
	if system == "" {
		system = "你是招聘助理。根据会话生成一条简短、礼貌、不过度承诺的回复，只输出回复正文。"
	}
	user := fmt.Sprintf("岗位：%s\n候选人：%s\n会话：%s", position.Name, conversation.Name, history)
	reply, err := c.chat(ctx, cfg, []chatMessage{
		textChatMessage("system", system),
		textChatMessage("user", user),
	})
	if err != nil {
		return "", err
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return "", fmt.Errorf("AI 没有生成回复")
	}
	return reply, nil
}

// chat 调用 OpenAI 兼容接口，临时错误最多尝试三次。
func (c *Client) chat(ctx context.Context, cfg cloud.AIConfig, messages []chatMessage) (string, error) {
	if err := c.Ready(cfg); err != nil {
		return "", err
	}
	requestBody := chatRequest{
		Model: cfg.Model, Messages: messages, Temperature: cfg.Temperature, Stream: true,
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("编码 AI 请求失败：%w", err)
	}
	endpoint := chatCompletionsURL(cfg.BaseURL)
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		content, requestErr := c.doChatRequest(ctx, endpoint, cfg.APIKey, encoded)
		if requestErr == nil {
			return content, nil
		}
		lastErr = requestErr
		var serviceErr *ServiceError
		if !errors.As(requestErr, &serviceErr) || !serviceErr.Retryable || attempt == 3 {
			return "", requestErr
		}
		if err = waitRetry(ctx, serviceErr.RetryAfter, attempt); err != nil {
			return "", err
		}
	}
	return "", lastErr
}

// doChatRequest 执行一次 AI 请求并兼容 JSON 和 SSE 响应。
func (c *Client) doChatRequest(ctx context.Context, endpoint string, apiKey string, payload []byte) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("创建 AI 请求失败：%w", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream, application/json")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", &ServiceError{Message: "连接 AI 失败：" + err.Error(), Retryable: true, Fatal: true, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return "", newHTTPServiceError(response.StatusCode, string(body), response.Header.Get("Retry-After"))
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		return readChatStream(response.Body)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return "", &ServiceError{Message: "读取 AI 响应失败：" + err.Error(), Retryable: true, Fatal: true, Cause: err}
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "data:") {
		return readChatStream(bytes.NewReader(body))
	}
	return readChatJSON(body)
}

// readChatJSON 解析普通 OpenAI 兼容 JSON 响应。
func readChatJSON(body []byte) (string, error) {
	var response chatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("解析 AI 响应失败：%w", err)
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("AI 没有返回内容")
	}
	return response.Choices[0].Message.Content, nil
}

// readChatStream 逐行读取 OpenAI 兼容 SSE 内容。
func readChatStream(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var content strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk streamChunk
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		content.WriteString(chunk.Choices[0].Delta.Content)
	}
	if err := scanner.Err(); err != nil {
		return "", &ServiceError{Message: "AI 流式响应中断：" + err.Error(), Retryable: true, Fatal: true, Cause: err}
	}
	if strings.TrimSpace(content.String()) == "" {
		return "", fmt.Errorf("AI 流式响应没有返回内容")
	}
	return content.String(), nil
}

// newHTTPServiceError 根据状态码和响应摘要生成错误策略。
func newHTTPServiceError(statusCode int, body string, retryAfter string) *ServiceError {
	body = strings.TrimSpace(body)
	if len([]rune(body)) > 500 {
		body = string([]rune(body)[:500])
	}
	lower := strings.ToLower(body)
	retryable := statusCode == http.StatusTooManyRequests || statusCode >= 500
	fatal := retryable || statusCode == 401 || statusCode == 402 || statusCode == 403 || statusCode == 404
	if statusCode == 400 || statusCode == 422 {
		fatal = containsAny(lower, "quota", "balance", "余额", "额度", "api key", "model", "模型", "not found")
	}
	message := fmt.Sprintf("AI 请求失败，状态码 %d", statusCode)
	if body != "" {
		message += "，响应：" + body
	}
	return &ServiceError{
		StatusCode: statusCode, Message: message, Retryable: retryable,
		Fatal: fatal, RetryAfter: parseRetryAfter(retryAfter),
	}
}

// waitRetry 在 AI 重试前执行可取消退避。
func waitRetry(ctx context.Context, retryAfter time.Duration, attempt int) error {
	delay := time.Duration(attempt) * 500 * time.Millisecond
	if retryAfter > delay {
		delay = retryAfter
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// parseRetryAfter 解析 Retry-After 秒数或 HTTP 时间。
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if target, err := http.ParseTime(value); err == nil {
		return max(time.Until(target), 0)
	}
	return 0
}

// candidateSystemPrompt 返回岗位级或全局候选人评分提示词。
func candidateSystemPrompt(cfg cloud.AIConfig, position cloud.PositionSnapshot) string {
	system := strings.TrimSpace(position.AIOptions.GreetPrompt)
	if system == "" {
		system = strings.TrimSpace(cfg.SystemPrompt)
	}
	if system == "" {
		system = `你是招聘顾问。只返回 JSON：{"score":0到100数字,"reason":"30字以内原因"}，不要 Markdown。`
	}
	return system
}

// candidateUserPrompt 构造候选人评分动态内容。
func candidateUserPrompt(position cloud.PositionSnapshot, candidate model.Candidate, detail model.CandidateDetail) string {
	return fmt.Sprintf(
		"岗位：%s\n岗位要求：%s\n关键词：%s\n候选人：%s\n摘要：%s\n详情：%s",
		position.Name, position.Description, position.Keyword, candidate.Name, candidate.Summary, detail.Text,
	)
}

// decisionThreshold 返回岗位级优先的打招呼分数阈值。
func decisionThreshold(cfg cloud.AIConfig, position cloud.PositionSnapshot) float64 {
	threshold := position.AIOptions.GreetScoreThreshold
	if threshold <= 0 {
		threshold = cfg.ScoreThreshold
	}
	if threshold <= 0 {
		threshold = 70
	}
	return threshold
}

// parseDecision 从模型文本提取、规范化评分和原因。
func parseDecision(content string, threshold float64) (Decision, error) {
	var parsed struct {
		Score  float64 `json:"score"`
		Reason string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &parsed); err != nil {
		return Decision{}, fmt.Errorf("AI 判断格式不正确：%w", err)
	}
	parsed.Score = min(100, max(0, parsed.Score))
	parsed.Reason = truncateRunes(strings.TrimSpace(parsed.Reason), 30)
	if parsed.Reason == "" {
		parsed.Reason = "AI 没有给出原因"
	}
	return Decision{
		Accepted: parsed.Score >= threshold, Score: parsed.Score, Reason: parsed.Reason,
	}, nil
}

// chatCompletionsURL 规范化 OpenAI 兼容聊天接口地址。
func chatCompletionsURL(baseURL string) string {
	endpoint := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(endpoint, "/chat/completions") {
		return endpoint
	}
	if !strings.HasSuffix(endpoint, "/v1") {
		endpoint += "/v1"
	}
	return endpoint + "/chat/completions"
}

// extractJSONObject 去掉模型可能附带的 Markdown 或解释文本。
func extractJSONObject(value string) string {
	value = strings.TrimSpace(value)
	start := strings.Index(value, "{")
	end := strings.LastIndex(value, "}")
	if start >= 0 && end >= start {
		return value[start : end+1]
	}
	return value
}

// truncateRunes 按字符数安全截断文本。
func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

// containsAny 判断文本是否包含任一关键词。
func containsAny(value string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(value, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

// textChatMessage 创建纯文本聊天消息。
func textChatMessage(role string, content string) chatMessage {
	return chatMessage{Role: role, Content: messageContent{Text: content}}
}

// messageContent 表示字符串或多模态数组形式的消息正文。
type messageContent struct {
	Text  string
	Parts []contentPart
}

// MarshalJSON 按内容类型编码 OpenAI 兼容消息正文。
func (c messageContent) MarshalJSON() ([]byte, error) {
	if len(c.Parts) > 0 {
		return json.Marshal(c.Parts)
	}
	return json.Marshal(c.Text)
}

// chatMessage 表示一条 OpenAI 兼容聊天消息。
type chatMessage struct {
	Role    string         `json:"role"`
	Content messageContent `json:"content"`
}

// contentPart 表示多模态消息中的文字或图片。
type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

// imageURL 表示 OpenAI 兼容图片地址。
type imageURL struct {
	URL string `json:"url"`
}

// chatRequest 表示 OpenAI 兼容聊天请求。
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

// chatResponse 表示非流式聊天响应。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// streamChunk 表示一段 OpenAI 兼容 SSE 增量。
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}
