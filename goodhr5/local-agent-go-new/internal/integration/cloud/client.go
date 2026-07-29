// Package cloud 提供本地程序访问 GoodHR 云端登录、会员、岗位和平台配置的强类型客户端。
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
		return UserSession{}, &APIError{
			StatusCode: http.StatusUnauthorized,
			Message:    "登录状态已经失效，请重新登录",
		}
	}
	return session, nil
}

// IsAuthExpired 判断云端错误是否明确表示登录态失效。
func IsAuthExpired(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) &&
		(apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden)
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

// Preferences 读取当前用户的拟人等待和休息配置。
func (c *Client) Preferences(ctx context.Context, token string) (UserPreferences, error) {
	var response struct {
		Config *UserPreferences `json:"config"`
		Data   struct {
			Config *UserPreferences `json:"config"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/config/user-preferences", token, nil, &response); err != nil {
		return UserPreferences{}, err
	}
	result := response.Config
	if result == nil {
		result = response.Data.Config
	}
	if result == nil {
		defaults := DefaultUserPreferences()
		result = &defaults
	}
	normalized := normalizeUserPreferences(*result)
	return normalized, nil
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
	_, err := c.syncSummary(ctx, token, summary)
	return err
}

// SyncPositionCounts 把主动打招呼任务的累计统计同步到云端岗位。
func (c *Client) SyncPositionCounts(ctx context.Context, token string, summary TaskSummary) error {
	if strings.TrimSpace(summary.PositionID) == "" {
		return fmt.Errorf("岗位编号不能为空")
	}
	request := struct {
		ScannedCount int `json:"scanned_count"`
		GreetedCount int `json:"greeted_count"`
		SkippedCount int `json:"skipped_count"`
		FailedCount  int `json:"failed_count"`
	}{
		ScannedCount: max(summary.Processed, 0),
		GreetedCount: max(summary.Succeeded, 0),
		SkippedCount: max(summary.Skipped, 0),
		FailedCount:  max(summary.Failed, 0),
	}
	path := "/api/positions/" + url.PathEscape(summary.PositionID) + "/counts"
	return c.do(ctx, http.MethodPost, path, token, request, nil)
}

// AddProcessedResumes 累加本次任务新发现且具有稳定编号的候选人数量。
func (c *Client) AddProcessedResumes(ctx context.Context, token string, positionID string, count int) error {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return fmt.Errorf("岗位编号不能为空")
	}
	if count <= 0 {
		return nil
	}
	request := struct {
		Count int `json:"count"`
	}{Count: count}
	path := "/api/positions/" + url.PathEscape(positionID) + "/processed-resumes"
	return c.do(ctx, http.MethodPost, path, token, request, nil)
}

// SyncCompletedSummary 最多尝试三次完成状态同步，并要求云端确认完成邮件已发送。
func (c *Client) SyncCompletedSummary(ctx context.Context, token string, summary TaskSummary) error {
	summary.Status = "completed"
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		result, err := c.syncSummary(ctx, token, summary)
		if err == nil && result.NoticeSent {
			return nil
		}
		if err == nil {
			err = fmt.Errorf("云端未确认完成邮件已发送")
		}
		lastErr = err
		if attempt < 3 {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return fmt.Errorf("同步完成状态和邮件失败，已尝试 3 次：%w", lastErr)
}

// syncSummary 执行一次任务状态同步并返回云端通知结果。
func (c *Client) syncSummary(ctx context.Context, token string, summary TaskSummary) (SummaryResult, error) {
	if strings.EqualFold(strings.TrimSpace(summary.TaskType), "greeting") &&
		!strings.EqualFold(strings.TrimSpace(summary.Status), "running") {
		if err := c.SyncPositionCounts(ctx, token, summary); err != nil {
			return SummaryResult{}, err
		}
	}
	path := "/api/positions/" + url.PathEscape(summary.PositionID) + "/status"
	request := struct {
		Status string `json:"status"`
	}{Status: summary.Status}
	var response SummaryResult
	err := c.do(ctx, http.MethodPost, path, token, request, &response)
	return response, err
}

// SendFailNotice 通知云端按岗位和当前用户发送失败邮件。
func (c *Client) SendFailNotice(ctx context.Context, token string, positionID string, errorMessage string) error {
	request := struct {
		PositionID   string `json:"position_id"`
		ErrorMessage string `json:"error_message"`
	}{
		PositionID:   strings.TrimSpace(positionID),
		ErrorMessage: strings.TrimSpace(errorMessage),
	}
	if request.PositionID == "" {
		return fmt.Errorf("岗位编号不能为空，失败邮件暂时没法发送")
	}
	var response struct {
		Status string `json:"status"`
	}
	return c.do(ctx, http.MethodPost, "/api/fail-notice", token, request, &response)
}

// DefaultUserPreferences 返回与云端一致的个人运行默认值。
func DefaultUserPreferences() UserPreferences {
	return UserPreferences{
		ClickFrequency: 80, DetailOpenProbability: 80,
		ScrollDelayMin: 3, ScrollDelayMax: 8,
		ListViewDelayMin: 1, ListViewDelayMax: 2,
		DetailViewDelayMin: 1, DetailViewDelayMax: 2,
		GreetDelayMin: 1, GreetDelayMax: 2,
		DetailOpenDelayMin: 1, DetailOpenDelayMax: 2,
		GreetBeforeDelayMin: 1, GreetBeforeDelayMax: 2,
		RestAfterCandidatesMin: 40, RestAfterCandidatesMax: 70,
		RestTimesMin: 2, RestTimesMax: 3,
		RestDurationMin: 2, RestDurationMax: 7,
	}
}

// normalizeUserPreferences 修正云端个人配置中的越界值和反向区间。
func normalizeUserPreferences(value UserPreferences) UserPreferences {
	value.DetailOpenProbability = clamp(value.DetailOpenProbability, 0, 100)
	value.ScrollDelayMin, value.ScrollDelayMax = normalizeIntRange(value.ScrollDelayMin, value.ScrollDelayMax)
	value.ListViewDelayMin, value.ListViewDelayMax = normalizeFloatRange(value.ListViewDelayMin, value.ListViewDelayMax)
	value.DetailViewDelayMin, value.DetailViewDelayMax = normalizeFloatRange(value.DetailViewDelayMin, value.DetailViewDelayMax)
	value.DetailOpenDelayMin, value.DetailOpenDelayMax = normalizeFloatRange(value.DetailOpenDelayMin, value.DetailOpenDelayMax)
	value.DetailCloseDelayMin, value.DetailCloseDelayMax = normalizeFloatRange(value.DetailCloseDelayMin, value.DetailCloseDelayMax)
	value.GreetBeforeDelayMin, value.GreetBeforeDelayMax = normalizeFloatRange(value.GreetBeforeDelayMin, value.GreetBeforeDelayMax)
	value.RestAfterCandidatesMin, value.RestAfterCandidatesMax = normalizeIntRange(value.RestAfterCandidatesMin, value.RestAfterCandidatesMax)
	value.RestTimesMin, value.RestTimesMax = normalizeIntRange(value.RestTimesMin, value.RestTimesMax)
	value.RestDurationMin, value.RestDurationMax = normalizeFloatRange(value.RestDurationMin, value.RestDurationMax)
	return value
}

// normalizeIntRange 把整数区间修正为非负且最大值不小于最小值。
func normalizeIntRange(minimum int, maximum int) (int, int) {
	if minimum < 0 {
		minimum = 0
	}
	if maximum < minimum {
		maximum = minimum
	}
	return minimum, maximum
}

// normalizeFloatRange 把浮点区间修正为非负且最大值不小于最小值。
func normalizeFloatRange(minimum float64, maximum float64) (float64, float64) {
	if minimum < 0 {
		minimum = 0
	}
	if maximum < minimum {
		maximum = minimum
	}
	return minimum, maximum
}

// clamp 把整数限制在指定闭区间内。
func clamp(value int, minimum int, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
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
	position.RequiresAI = position.RequiresAI || mode != "keyword" || detailMode == "ai"
	position.RequestPhone = position.RequestPhone || position.CommonConfig.RequestPhone
	position.RequestWechat = position.RequestWechat || position.CommonConfig.RequestWechat
	position.RequestResume = position.RequestResume || position.CommonConfig.RequestResume
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
			Msg     string `json:"msg"`
		}
		_ = json.Unmarshal(content, &failure)
		message := strings.TrimSpace(failure.Message)
		if message == "" {
			message = strings.TrimSpace(failure.Error)
		}
		if message == "" {
			message = strings.TrimSpace(failure.Msg)
		}
		if message == "" {
			message = response.Status
		}
		if message == "session is invalid or expired" || message == "session invalid or expired" {
			message = "登录状态已经失效，请重新登录"
		}
		return &APIError{StatusCode: response.StatusCode, Message: message}
	}
	if result != nil && len(content) > 0 {
		if err := json.Unmarshal(content, result); err != nil {
			return fmt.Errorf("解析云端响应失败：%w", err)
		}
	}
	return nil
}
