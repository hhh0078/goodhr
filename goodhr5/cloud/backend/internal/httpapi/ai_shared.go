// 本文件负责定义云端 AI 接口复用的轻量结构和工具方法。
package httpapi

import (
	"regexp"
	"strings"
)

// AIRequest 表示 OpenAI 兼容接口请求体。
type AIRequest struct {
	Model          string            `json:"model"`
	Messages       []AIMsg           `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
	ReasoningSplit bool              `json:"reasoning_split,omitempty"`
}

// AIMsg 表示兼容 OpenAI Chat Completions 的消息结构。
type AIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AIResponse 表示 OpenAI 兼容接口响应体。
type AIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// firstNonEmpty 返回第一个非空字符串，常用于候选人入库字段兜底。
// primary 为优先值，fallback 为兜底值。
func firstNonEmpty(primary string, fallback string) string {
	text := strings.TrimSpace(primary)
	if text != "" {
		return text
	}
	return strings.TrimSpace(fallback)
}

// cleanAITextOutput 清理 AI 文本输出中的思考标签和单层 Markdown 代码块。
// raw 为模型原始输出，返回可用于解析或展示的正文。
func cleanAITextOutput(raw string) string {
	text := stripThinkTags(raw)
	text = stripMarkdownCodeFence(text)
	return strings.TrimSpace(text)
}

// stripThinkTags 删除模型输出中的 <think> 思考内容。
// raw 为模型原始输出，返回删除思考标签后的正文。
func stripThinkTags(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	re := regexp.MustCompile(`(?is)<think>.*?</think>`)
	return strings.TrimSpace(re.ReplaceAllString(text, ""))
}

// stripMarkdownCodeFence 删除模型输出外层的 Markdown 代码块。
// raw 为模型原始输出，返回去掉外层 ``` 或 ```json 后的正文。
func stripMarkdownCodeFence(raw string) string {
	text := strings.TrimSpace(raw)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return text
	}
	first := strings.TrimSpace(lines[0])
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(first, "```") || last != "```" {
		return text
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}

// float64Ptr 返回 float64 的指针。
// v 为原始浮点数。
func float64Ptr(v float64) *float64 {
	value := v
	return &value
}
