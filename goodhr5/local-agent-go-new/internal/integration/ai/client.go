// Package ai 调用云端下发配置的 OpenAI 兼容接口，完成候选人判断和自动回复生成。
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// Client 是无状态的 OpenAI 兼容客户端。
type Client struct {
	http *http.Client
}

// Decision 表示候选人是否适合打招呼。
type Decision struct {
	Accepted bool    `json:"accepted"`
	Score    float64 `json:"score"`
	Reason   string  `json:"reason"`
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

// EvaluateCandidate 根据岗位和候选人详情生成强类型判断。
func (c *Client) EvaluateCandidate(ctx context.Context, cfg cloud.AIConfig, position cloud.PositionSnapshot, candidate model.Candidate, detail model.CandidateDetail) (Decision, error) {
	if err := c.Ready(cfg); err != nil {
		return Decision{}, err
	}
	system := strings.TrimSpace(cfg.SystemPrompt)
	if system == "" {
		system = `你是招聘顾问。只返回 JSON：{"score":0到100数字,"reason":"30字以内原因"}，不要 Markdown。`
	}
	user := fmt.Sprintf("岗位：%s\n关键词：%s\n候选人：%s\n摘要：%s\n详情：%s", position.Name, position.Keyword, candidate.Name, candidate.Summary, detail.Text)
	content, err := c.chat(ctx, cfg, system, user)
	if err != nil {
		return Decision{}, err
	}
	var parsed struct {
		Score  float64 `json:"score"`
		Reason string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &parsed); err != nil {
		return Decision{}, fmt.Errorf("AI 判断格式不正确：%w", err)
	}
	threshold := cfg.ScoreThreshold
	if threshold <= 0 {
		threshold = 70
	}
	return Decision{
		Accepted: parsed.Score >= threshold,
		Score:    parsed.Score,
		Reason:   strings.TrimSpace(parsed.Reason),
	}, nil
}

// GenerateReply 根据会话上下文生成一条简短回复。
func (c *Client) GenerateReply(ctx context.Context, cfg cloud.AIConfig, position cloud.PositionSnapshot, conversation model.Conversation, history string) (string, error) {
	if err := c.Ready(cfg); err != nil {
		return "", err
	}
	system := strings.TrimSpace(cfg.ReplySystemPrompt)
	if system == "" {
		system = "你是招聘助理。根据会话生成一条简短、礼貌、不过度承诺的回复，只输出回复正文。"
	}
	user := fmt.Sprintf("岗位：%s\n候选人：%s\n会话：%s", position.Name, conversation.Name, history)
	reply, err := c.chat(ctx, cfg, system, user)
	if err != nil {
		return "", err
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return "", fmt.Errorf("AI 没有生成回复")
	}
	return reply, nil
}

// chat 调用 OpenAI 兼容聊天接口并返回文本。
func (c *Client) chat(ctx context.Context, cfg cloud.AIConfig, system string, user string) (string, error) {
	endpoint := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
		endpoint += "/chat/completions"
	}
	payload := struct {
		Model       string    `json:"model"`
		Messages    []message `json:"messages"`
		Temperature float64   `json:"temperature"`
	}{
		Model: cfg.Model,
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: cfg.Temperature,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("编码 AI 请求失败：%w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", fmt.Errorf("创建 AI 请求失败：%w", err)
	}
	request.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("连接 AI 失败：%w", err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("读取 AI 响应失败：%w", err)
	}
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("AI 请求失败：%s", response.Status)
	}
	var result struct {
		Choices []struct {
			Message message `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(content, &result); err != nil {
		return "", fmt.Errorf("解析 AI 响应失败：%w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("AI 没有返回内容")
	}
	return result.Choices[0].Message.Content, nil
}

// message 表示 OpenAI 兼容聊天消息。
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// extractJSONObject 去掉模型可能附带的 Markdown 包裹。
func extractJSONObject(value string) string {
	value = strings.TrimSpace(value)
	start := strings.Index(value, "{")
	end := strings.LastIndex(value, "}")
	if start >= 0 && end >= start {
		return value[start : end+1]
	}
	return value
}
