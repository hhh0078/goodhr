// Package cloud 提供本地程序访问 GoodHR 云端登录、会员、岗位和平台配置的强类型客户端。
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// Client 是 GoodHR 云端强类型客户端。
type Client struct {
	baseURL string
	http    *http.Client
}

// UserSession 表示云端登录状态。
type UserSession struct {
	UserID   string `json:"id"`
	LoggedIn bool   `json:"logged_in"`
}

// Subscription 表示会员可用状态。
type Subscription struct {
	Active     bool   `json:"active"`
	MemberType string `json:"member_type"`
	ExpiresAt  string `json:"expires_at"`
}

// AIConfig 表示云端下发给本地 AI 客户端的配置。
type AIConfig struct {
	BaseURL           string  `json:"base_url"`
	APIKey            string  `json:"api_key"`
	Model             string  `json:"model"`
	Temperature       float64 `json:"temperature"`
	ScoreThreshold    float64 `json:"score_threshold"`
	SystemPrompt      string  `json:"system_prompt"`
	ReplySystemPrompt string  `json:"reply_system_prompt"`
	PromptTemplate    string  `json:"prompt_template"`
}

// PositionCommonConfig 表示云端岗位公共运行配置。
type PositionCommonConfig struct {
	ModeDefault string `json:"mode_default"`
	DetailMode  string `json:"detail_mode"`
	ScanRounds  int    `json:"scan_rounds"`
}

// PositionAIOptions 表示岗位级 AI 阈值和提示词。
type PositionAIOptions struct {
	GreetScoreThreshold float64 `json:"greet_score_threshold"`
	GreetPrompt         string  `json:"greet_prompt"`
	ReplyPrompt         string  `json:"reply_prompt"`
}

// PositionSnapshot 表示任务启动时冻结的岗位配置。
type PositionSnapshot struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	PlatformID      string               `json:"platform_id"`
	ProfileID       string               `json:"profile_id"`
	Keyword         string               `json:"keyword"`
	Keywords        []string             `json:"keywords"`
	ExcludeKeywords []string             `json:"exclude_keywords"`
	IsAndMode       bool                 `json:"is_and_mode"`
	Description     string               `json:"description"`
	GreetMessage    string               `json:"greet_message"`
	MatchLimit      int                  `json:"match_limit"`
	MaxBatches      int                  `json:"max_batches"`
	RequiresAI      bool                 `json:"requires_ai"`
	RequiresOCR     bool                 `json:"requires_ocr"`
	AutoReplyWait   int                  `json:"auto_reply_wait_seconds"`
	CommonConfig    PositionCommonConfig `json:"common_config"`
	AIOptions       PositionAIOptions    `json:"ai_config"`
	AI              AIConfig             `json:"ai"`
}

// TaskSummary 表示同步到云端的不含敏感数据任务摘要。
type TaskSummary struct {
	TaskID       string `json:"task_id"`
	PositionID   string `json:"position_id"`
	Status       string `json:"status"`
	Processed    int    `json:"processed"`
	Succeeded    int    `json:"succeeded"`
	Failed       int    `json:"failed"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// New 创建 GoodHR 云端客户端。
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// ValidateSession 验证云端登录态。
func (c *Client) ValidateSession(ctx context.Context, token string) (UserSession, error) {
	var response struct {
		User UserSession `json:"user"`
		Data struct {
			User UserSession `json:"user"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/auth/me", token, nil, &response); err != nil {
		return UserSession{}, err
	}
	session := response.User
	if session.UserID == "" {
		session = response.Data.User
	}
	session.LoggedIn = session.UserID != ""
	if !session.LoggedIn {
		return UserSession{}, fmt.Errorf("登录状态已经失效，请重新登录")
	}
	return session, nil
}

// Subscription 读取当前会员状态。
func (c *Client) Subscription(ctx context.Context, token string) (Subscription, error) {
	var response struct {
		Subscription Subscription `json:"subscription"`
		Data         struct {
			Subscription Subscription `json:"subscription"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/subscription/status", token, nil, &response); err != nil {
		return Subscription{}, err
	}
	result := response.Subscription
	if result.MemberType == "" {
		result = response.Data.Subscription
	}
	if !result.Active {
		return Subscription{}, fmt.Errorf("当前会员暂不可用，请先看看会员状态")
	}
	return result, nil
}

// Position 读取并冻结岗位配置。
func (c *Client) Position(ctx context.Context, token string, positionID string) (PositionSnapshot, error) {
	var response struct {
		Position PositionSnapshot `json:"position"`
		Data     struct {
			Position PositionSnapshot `json:"position"`
		} `json:"data"`
	}
	path := "/api/positions/" + url.PathEscape(strings.TrimSpace(positionID))
	if err := c.do(ctx, http.MethodGet, path, token, nil, &response); err != nil {
		return PositionSnapshot{}, err
	}
	result := response.Position
	if result.ID == "" {
		result = response.Data.Position
	}
	if result.ID == "" || result.PlatformID == "" {
		return PositionSnapshot{}, fmt.Errorf("云端岗位配置不完整")
	}
	result = normalizePosition(result)
	return result, nil
}

// EffectiveAI 读取允许本地任务使用的明文 AI 配置。
func (c *Client) EffectiveAI(ctx context.Context, token string) (AIConfig, error) {
	var response struct {
		Config AIConfig `json:"config"`
		Data   struct {
			Config AIConfig `json:"config"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/config/effective-ai?reveal_api_key=1", token, nil, &response); err != nil {
		return AIConfig{}, err
	}
	result := response.Config
	if result.BaseURL == "" {
		result = response.Data.Config
	}
	if result.SystemPrompt == "" {
		result.SystemPrompt = result.PromptTemplate
	}
	return result, nil
}

// PlatformConfig 读取指定平台的强类型 URL 和选择器配置。
func (c *Client) PlatformConfig(ctx context.Context, token string, platformID string) (model.Config, error) {
	var direct struct {
		Config json.RawMessage `json:"config"`
		Data   struct {
			Config json.RawMessage `json:"config"`
		} `json:"data"`
	}
	path := "/api/platforms/config/" + url.PathEscape(strings.ToLower(strings.TrimSpace(platformID)))
	if err := c.do(ctx, http.MethodGet, path, token, nil, &direct); err == nil {
		raw := direct.Config
		if len(raw) == 0 {
			raw = direct.Data.Config
		}
		if len(raw) > 0 {
			return decodePlatformConfig(raw, platformID)
		}
	}
	return c.platformConfigFromList(ctx, token, platformID)
}

// SyncSummary 把不含候选人详情的任务统计同步给云端。
func (c *Client) SyncSummary(ctx context.Context, token string, summary TaskSummary) error {
	if summary.Status == "failed" {
		summary.Status = "stopped"
	}
	path := "/api/positions/" + url.PathEscape(summary.PositionID) + "/status"
	var response struct {
		Success bool `json:"success"`
	}
	return c.do(ctx, http.MethodPost, path, token, summary, &response)
}

// normalizePosition 从现有云端岗位字段推导新主流程所需值。
func normalizePosition(position PositionSnapshot) PositionSnapshot {
	if position.ProfileID == "" {
		position.ProfileID = "default"
	}
	if position.Keyword == "" {
		position.Keyword = strings.Join(position.Keywords, " ")
	}
	if position.MaxBatches <= 0 {
		position.MaxBatches = position.CommonConfig.ScanRounds
	}
	if position.MaxBatches <= 0 {
		position.MaxBatches = 3
	}
	mode := strings.ToLower(strings.TrimSpace(position.CommonConfig.ModeDefault))
	detailMode := strings.ToLower(strings.TrimSpace(position.CommonConfig.DetailMode))
	position.RequiresOCR = position.RequiresOCR || detailMode == "ocr"
	position.RequiresAI = position.RequiresAI || mode != "keyword" || detailMode == "ai" || detailMode == "ocr"
	return position
}

// platformConfigFromList 兼容云端 system_configs 列表格式并立即转成强类型。
func (c *Client) platformConfigFromList(ctx context.Context, token string, platformID string) (model.Config, error) {
	var response struct {
		Configs []struct {
			ConfigKey   string          `json:"config_key"`
			ConfigValue json.RawMessage `json:"config_value"`
		} `json:"configs"`
		Data struct {
			Configs []struct {
				ConfigKey   string          `json:"config_key"`
				ConfigValue json.RawMessage `json:"config_value"`
			} `json:"configs"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/platforms/config/", token, nil, &response); err != nil {
		return model.Config{}, err
	}
	entries := response.Configs
	if len(entries) == 0 {
		entries = response.Data.Configs
	}
	target := "platform." + strings.ToLower(strings.TrimSpace(platformID))
	for _, entry := range entries {
		if strings.ToLower(strings.TrimSpace(entry.ConfigKey)) != target {
			continue
		}
		return decodePlatformConfig(entry.ConfigValue, platformID)
	}
	return model.Config{}, fmt.Errorf("云端没有找到平台配置 %s", platformID)
}

// do 发送云端请求并解析强类型响应。
func (c *Client) do(ctx context.Context, method string, path string, token string, payload any, result any) error {
	if c.baseURL == "" {
		return fmt.Errorf("云端地址不能为空")
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("编码云端请求失败：%w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("创建云端请求失败：%w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(token) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("连接云端失败：%w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 4<<20)
	content, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("读取云端响应失败：%w", err)
	}
	if response.StatusCode >= 400 {
		var failure struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(content, &failure)
		message := strings.TrimSpace(failure.Message)
		if message == "" {
			message = strings.TrimSpace(failure.Error)
		}
		if message == "" {
			message = response.Status
		}
		return fmt.Errorf("云端请求失败：%s", message)
	}
	if result != nil && len(content) > 0 {
		if err := json.Unmarshal(content, result); err != nil {
			return fmt.Errorf("解析云端响应失败：%w", err)
		}
	}
	return nil
}
