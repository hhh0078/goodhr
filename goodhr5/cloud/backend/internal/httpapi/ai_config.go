// 本文件负责提供用户自定义 AI 配置的 HTTP API。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const aiConfigTestTimeout = 30 * time.Second

// AIConfigService 处理用户 AI 配置读取和保存请求。
type AIConfigService struct {
	auth       *AuthService
	store      AIConfigStore
	httpClient *http.Client
}

type aiConfigRequest struct {
	BaseURL        string  `json:"base_url"`
	Model          string  `json:"model"`
	APIKey         string  `json:"api_key"`
	Temperature    float64 `json:"temperature"`
	PromptTemplate string  `json:"prompt_template"`
	Enabled        bool    `json:"enabled"`
}

// NewAIConfigService 创建 AI 配置 API 服务，并注入认证服务和配置存储。
func NewAIConfigService(auth *AuthService, store AIConfigStore) *AIConfigService {
	return &AIConfigService{
		auth:       auth,
		store:      store,
		httpClient: newAIConfigTestHTTPClient(),
	}
}

// Test 验证当前登录用户填写的 OpenAI 兼容配置是否可以正常调用。
func (s *AIConfigService) Test(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.currentSession(w, r); !ok {
		return
	}
	req, ok := s.readConfigRequest(w, r)
	if !ok {
		return
	}
	if err := validateAIConfigTestRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), aiConfigTestTimeout)
	defer cancel()
	content, err := s.requestAITest(ctx, req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "AI 配置测试成功",
		"content": content,
	})
}

// requestAITest 请求用户填写的 AI 服务，并返回助手响应正文。
// ctx 控制请求超时，config 为待验证的 AI 配置。
func (s *AIConfigService) requestAITest(ctx context.Context, config aiConfigRequest) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"model":       strings.TrimSpace(config.Model),
		"messages":    []map[string]string{{"role": "user", "content": "请只返回两个字：成功"}},
		"temperature": 0,
		"stream":      false,
	})
	if err != nil {
		return "", fmt.Errorf("AI 测试参数生成失败：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(config.BaseURL), bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("AI 接口地址无效：%w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(config.APIKey))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", errors.New("AI 服务请求超时，请检查接口地址和网络")
		}
		return "", fmt.Errorf("AI 服务连接失败：%w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("读取 AI 服务响应失败：%w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("AI 服务返回状态码 %d：%s", resp.StatusCode, aiErrorMessage(body))
	}
	content := aiResponseContent(body)
	if content == "" {
		return "", errors.New("AI 服务响应中没有可用内容，请检查模型名称")
	}
	return content, nil
}

// validateAIConfigTestRequest 校验 AI 测试参数和公网 HTTPS 地址。
func validateAIConfigTestRequest(req aiConfigRequest) error {
	if strings.TrimSpace(req.APIKey) == "" {
		return errors.New("请填写 AI Key")
	}
	if strings.TrimSpace(req.Model) == "" {
		return errors.New("请填写 AI 模型")
	}
	parsed, err := url.Parse(strings.TrimSpace(req.BaseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("AI 接口必须是有效的公网 HTTPS 地址")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !isPublicAIIP(ip) {
		return errors.New("AI 接口不允许使用内网地址")
	}
	if strings.EqualFold(parsed.Hostname(), "localhost") {
		return errors.New("AI 接口不允许使用本机地址")
	}
	return nil
}

// newAIConfigTestHTTPClient 创建限制内网访问和重定向的 AI 测试客户端。
func newAIConfigTestHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialPublicAIEndpoint
	return &http.Client{
		Timeout:   aiConfigTestTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if err := validateAIConfigTestRequest(aiConfigRequest{BaseURL: req.URL.String(), Model: "redirect", APIKey: "redirect"}); err != nil {
				return err
			}
			if len(via) >= 3 {
				return errors.New("AI 接口重定向次数过多")
			}
			return nil
		},
	}
}

// dialPublicAIEndpoint 仅连接解析后的公网 IP，防止 AI 测试接口访问服务器内网。
func dialPublicAIEndpoint(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if !isPublicAIIP(address.IP) {
			continue
		}
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
	}
	return nil, errors.New("AI 接口未解析到可用的公网地址")
}

// isPublicAIIP 判断 IP 是否属于允许访问的公网地址。
func isPublicAIIP(ip net.IP) bool {
	return ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

// aiResponseContent 从 OpenAI 兼容 JSON 响应中提取助手正文。
func aiResponseContent(body []byte) string {
	var payload struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload.Choices) == 0 {
		return ""
	}
	switch content := payload.Choices[0].Message.Content.(type) {
	case string:
		return strings.TrimSpace(content)
	case []any:
		parts := make([]string, 0, len(content))
		for _, item := range content {
			part, _ := item.(map[string]any)
			parts = append(parts, strings.TrimSpace(fmt.Sprint(part["text"])))
		}
		return strings.TrimSpace(strings.Join(parts, ""))
	default:
		return ""
	}
}

// aiErrorMessage 提取 AI 服务错误响应，避免向前端返回过长内容。
func aiErrorMessage(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 1000 {
		return text[:1000] + "..."
	}
	if text == "" {
		return "无响应内容"
	}
	return text
}

// User 按请求方法读取或保存当前登录用户的自定义 AI 配置。
func (s *AIConfigService) User(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		// PUT 请求复用用户配置保存逻辑，让读取和更新保持同一个资源路径。
		s.UpdateUser(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，用于只返回自己的 AI 配置。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	// 调用 AIConfigStore 读取用户配置；未配置时返回空配置。
	config, err := s.store.UserConfig(session.Email)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"config": nil,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user ai config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": publicUserAIConfig(config),
	})
}

// UpdateUser 保存当前登录用户的自定义 AI 配置。
func (s *AIConfigService) UpdateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，用于将配置写入该用户名下。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	req, ok := s.readConfigRequest(w, r)
	if !ok {
		return
	}

	configToSave := req.toConfig()
	if configToSave.APIKey == "" {
		current, err := s.store.UserConfig(session.Email)
		if err == nil && current.APIKey != "" {
			configToSave.APIKey = current.APIKey
		}
	}

	// 调用 AIConfigStore 保存用户配置，任务运行时会优先使用它。
	config, err := s.store.SaveUserConfig(session.Email, configToSave)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save user ai config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": publicUserAIConfig(config),
	})
}

// Effective 返回当前登录用户最终生效的 AI 配置。
func (s *AIConfigService) Effective(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，用于返回该用户自己的配置。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	// 调用 AIConfigStore 读取用户配置；AI 连接参数不再使用系统默认兜底。
	user, err := s.store.UserConfig(session.Email)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"config": nil,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user ai config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": publicAIConfigForRequest(user, r),
	})
}

// publicAIConfigForRequest 按请求场景返回 AI 配置。
// config 为用户配置，r 为 HTTP 请求；本地 Agent 可通过 reveal_api_key=1 获取明文 Key。
func publicAIConfigForRequest(config AIConfig, r *http.Request) map[string]any {
	result := publicAIConfig(config)
	if shouldRevealAPIKey(r) {
		result["api_key"] = config.APIKey
	}
	return result
}

// publicUserAIConfig 返回个人配置页面使用的 AI 配置，并明文返回 API Key。
// config 为用户自己的 AI 配置，返回值用于前端表单直接展示和编辑。
func publicUserAIConfig(config AIConfig) map[string]any {
	result := publicAIConfig(config)
	result["api_key"] = config.APIKey
	return result
}

// shouldRevealAPIKey 判断本次请求是否允许返回明文 AI Key。
// r 为 HTTP 请求，本地程序传 reveal_api_key=1 时返回 true。
func shouldRevealAPIKey(r *http.Request) bool {
	return r != nil && r.URL.Query().Get("reveal_api_key") == "1"
}

// currentSession 从请求中解析登录会话。
func (s *AIConfigService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	// 调用认证服务解析请求会话，避免 AI 配置 API 自己重复处理 token。
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

// readConfigRequest 读取并校验 AI 配置请求体。
func (s *AIConfigService) readConfigRequest(w http.ResponseWriter, r *http.Request) (aiConfigRequest, bool) {
	var req aiConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return aiConfigRequest{}, false
	}
	return req, true
}

// toConfig 将 HTTP 请求结构转换为内部 AI 配置模型。
func (r aiConfigRequest) toConfig() AIConfig {
	return AIConfig{
		BaseURL:        r.BaseURL,
		Model:          r.Model,
		APIKey:         r.APIKey,
		Temperature:    r.Temperature,
		PromptTemplate: r.PromptTemplate,
		Enabled:        r.Enabled,
	}
}

// publicAIConfig 返回可给前端展示的 AI 配置，并隐藏完整 API Key。
func publicAIConfig(config AIConfig) map[string]any {
	return map[string]any{
		"base_url":         config.BaseURL,
		"model":            config.Model,
		"api_key_set":      config.APIKey != "",
		"temperature":      config.Temperature,
		"prompt_template":  config.PromptTemplate,
		"enabled":          config.Enabled,
		"updated_at":       config.UpdatedAt,
		"api_key_masked":   maskedAPIKey(config.APIKey),
		"api_key_redacted": config.APIKey != "",
	}
}

// maskedAPIKey 返回脱敏后的 API Key，避免完整密钥进入前端。
func maskedAPIKey(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}
