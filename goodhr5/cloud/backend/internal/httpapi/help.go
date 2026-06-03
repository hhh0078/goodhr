// 本文件负责提供帮助中心系统指南读取和 AI 问答接口。
package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const helpAssistantAdminEmail = "1224299352@qq.com"

// HelpService 处理帮助中心系统指南和 AI 助手请求。
type HelpService struct {
	auth          *AuthService
	systemConfig  SystemConfigStore
	aiConfigStore AIConfigStore
	httpClient    *http.Client
}

type helpChatRequest struct {
	Messages []helpChatMessage `json:"messages"`
}

type helpChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type helpAIRequest struct {
	Model          string            `json:"model"`
	Messages       []AIMsg           `json:"messages"`
	Temperature    float64           `json:"temperature"`
	Stream         bool              `json:"stream"`
	ReasoningSplit bool              `json:"reasoning_split,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type helpAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// NewHelpService 创建帮助中心服务。
func NewHelpService(auth *AuthService, systemConfig SystemConfigStore, aiConfigStore AIConfigStore) *HelpService {
	return &HelpService{
		auth:          auth,
		systemConfig:  systemConfig,
		aiConfigStore: aiConfigStore,
		httpClient:    &http.Client{Timeout: 180 * time.Second},
	}
}

// Guide 返回系统指南 JSON，供前端帮助卡片展示。
func (s *HelpService) Guide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.currentSession(w, r); !ok {
		return
	}
	guide, err := s.loadGuide()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load system guide")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "guide": guide})
}

// Chat 使用超级管理员 AI 配置回答帮助问题，并以文本流返回。
func (s *HelpService) Chat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.currentSession(w, r); !ok {
		return
	}
	var req helpChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	messages := normalizeHelpMessages(req.Messages)
	if len(messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages is required")
		return
	}
	guide, err := s.loadGuide()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load system guide")
		return
	}
	aiConfig, err := s.aiConfigStore.UserConfig(helpAssistantAdminEmail)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusConflict, "超级管理员 AI 配置未设置")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load admin ai config")
		return
	}
	if !aiConfig.Enabled {
		writeError(w, http.StatusConflict, "超级管理员 AI 配置未启用")
		return
	}
	if strings.TrimSpace(aiConfig.BaseURL) == "" || strings.TrimSpace(aiConfig.Model) == "" || strings.TrimSpace(aiConfig.APIKey) == "" {
		writeError(w, http.StatusConflict, "超级管理员 AI 配置不完整")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	if err := s.streamAIAnswer(w, r, guide, aiConfig, messages); err != nil {
		_, _ = w.Write([]byte("\n[帮助助手错误] " + err.Error()))
	}
}

// currentSession 从请求中解析登录会话。
func (s *HelpService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return Session{}, false
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return Session{}, false
	}
	return session, true
}

// loadGuide 读取并解析系统指南配置。
func (s *HelpService) loadGuide() (map[string]any, error) {
	cfg, err := s.systemConfig.Get("system.guide")
	if err != nil {
		return nil, err
	}
	var guide map[string]any
	if err := json.Unmarshal([]byte(cfg.ConfigValue), &guide); err != nil {
		return nil, err
	}
	return guide, nil
}

// normalizeHelpMessages 清洗前端传入的聊天记录，最多保留 20 轮。
func normalizeHelpMessages(items []helpChatMessage) []helpChatMessage {
	result := make([]helpChatMessage, 0, len(items))
	for _, item := range items {
		role := strings.TrimSpace(strings.ToLower(item.Role))
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		if role != "assistant" {
			role = "user"
		}
		result = append(result, helpChatMessage{Role: role, Content: content})
	}
	if len(result) > 40 {
		result = result[len(result)-40:]
	}
	return result
}

// streamAIAnswer 调用 OpenAI 兼容接口并把回答内容转发给前端。
func (s *HelpService) streamAIAnswer(w http.ResponseWriter, r *http.Request, guide map[string]any, aiConfig AIConfig, messages []helpChatMessage) error {
	guideBytes, _ := json.Marshal(guide)
	aiMessages := []AIMsg{
		{
			Role:    "system",
			Content: "你是 GoodHR 5 的中文帮助助手。你只回答 GoodHR 5 使用、参数、报错排查、订阅、本地程序、平台账号、任务运行相关问题。回答要简洁、具体、适合代码小白。禁止输出 <think> 思考内容，禁止输出 Markdown 代码块。下面是系统指南 JSON：\n" + string(guideBytes),
		},
	}
	for _, item := range messages {
		aiMessages = append(aiMessages, AIMsg{Role: item.Role, Content: item.Content})
	}
	reqBody := helpAIRequest{
		Model:       strings.TrimSpace(aiConfig.Model),
		Messages:    aiMessages,
		Temperature: aiConfig.Temperature,
		Stream:      true,
	}
	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimSpace(aiConfig.BaseURL), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(aiConfig.APIKey))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("AI API 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("AI API 错误 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := forwardAIStream(w, resp.Body); err != nil {
		return err
	}
	return nil
}

// forwardAIStream 解析 OpenAI 兼容流式响应并输出纯文本。
func forwardAIStream(w http.ResponseWriter, body io.Reader) error {
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if line == "[DONE]" {
			break
		}
		var chunk helpAIStreamChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			text := choice.Delta.Content
			if text == "" {
				text = choice.Message.Content
			}
			if text == "" {
				continue
			}
			if _, err := w.Write([]byte(text)); err != nil {
				return err
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	return scanner.Err()
}
