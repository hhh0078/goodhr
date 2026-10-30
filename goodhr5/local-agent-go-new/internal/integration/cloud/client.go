// Package cloud 提供本地程序访问 GoodHR 云端登录、会员、岗位和统计能力的强类型客户端。
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
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/storage"
)

// UploadBrowserEvents 把已脱敏的浏览器异常退出日志批量上传云端。
func (c *Client) UploadBrowserEvents(ctx context.Context, token string, events []storage.BrowserEvent) error {
	return c.do(ctx, http.MethodPost, "/api/browser-events", token, map[string]any{"events": events}, nil)
}

// Client 是 GoodHR 云端强类型客户端。
type Client struct {
	baseURL             string
	http                *http.Client
	localAutoStore      *storage.Store
	localAutoDir        string
	localAutoConfigured bool
	localAutoMu         sync.Mutex
	localAutoOwners     sync.Map
}

// New 创建 GoodHR 云端客户端。
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// ValidateSession 验证云端登录态。
func (c *Client) ValidateSession(ctx context.Context, token string) (UserSession, error) {
	var response struct {
		User    UserSession `json:"user"`
		Session struct {
			Kind string `json:"kind"`
		} `json:"session"`
		Data struct {
			User    UserSession `json:"user"`
			Session struct {
				Kind string `json:"kind"`
			} `json:"session"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/auth/me", token, nil, &response); err != nil {
		return UserSession{}, err
	}
	session := response.User
	if session.UserID == "" {
		session = response.Data.User
	}
	if response.Session.Kind != "" {
		session.SessionKind = response.Session.Kind
	} else if response.Data.Session.Kind != "" {
		session.SessionKind = response.Data.Session.Kind
	}
	session.LoggedIn = session.UserID != ""
	if session.LoggedIn {
		c.localAutoOwners.Store(autoReplyDigest(token), session.UserID)
	}
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

// BindAgent 使用浏览器传来的登录凭证把当前稳定设备编号绑定到云端账号。
func (c *Client) BindAgent(ctx context.Context, token string, request AgentBindRequest) (AgentBinding, error) {
	if strings.TrimSpace(request.MachineID) == "" {
		return AgentBinding{}, fmt.Errorf("设备编号不能为空")
	}
	var response struct {
		Agent AgentBinding `json:"agent"`
		Data  struct {
			Agent AgentBinding `json:"agent"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/agents/bind", token, request, &response); err != nil {
		return AgentBinding{}, err
	}
	result := response.Agent
	if result.MachineID == "" {
		result = response.Data.Agent
	}
	if result.MachineID == "" {
		return AgentBinding{}, fmt.Errorf("云端没有确认设备绑定结果")
	}
	return result, nil
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

// RequestPositionStart 同步请求云端完成启动检查并占用账号唯一运行名额。
// token 为登录凭证，positionID 为岗位编号，taskType 为本地主流程类型。
func (c *Client) RequestPositionStart(ctx context.Context, token string, positionID string, taskType string, machineID string) error {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return fmt.Errorf("岗位编号不能为空")
	}
	request := struct {
		TaskType  string `json:"task_type"`
		MachineID string `json:"machine_id"`
	}{TaskType: strings.TrimSpace(taskType), MachineID: strings.TrimSpace(machineID)}
	path := "/api/positions/" + url.PathEscape(positionID) + "/start"
	return c.do(ctx, http.MethodPost, path, token, request, nil)
}

// RequestPositionStop 请求云端停止指定岗位，并核对岗位编号和已保存的停止状态。
// token 为本次停止请求的登录凭证，positionID 为待停止的岗位编号；本方法不修改任务统计。
func (c *Client) RequestPositionStop(ctx context.Context, token string, positionID string) error {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return fmt.Errorf("岗位编号不能为空")
	}
	type confirmation struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	var response struct {
		OK       *bool         `json:"ok"`
		Position *confirmation `json:"position"`
		Status   string        `json:"status"`
		Data     struct {
			OK       *bool         `json:"ok"`
			Position *confirmation `json:"position"`
			Status   string        `json:"status"`
		} `json:"data"`
	}
	path := "/api/positions/" + url.PathEscape(positionID) + "/stop"
	if err := c.do(ctx, http.MethodPost, path, token, struct{}{}, &response); err != nil {
		return err
	}
	notConfirmed := &APIError{
		StatusCode: http.StatusBadGateway, Code: "CLOUD_STOP_NOT_CONFIRMED",
		Message: "云端还没确认这个岗位已经停下，请稍后再试",
	}
	confirmedPosition, confirmedStatus, confirmedOK := false, false, false
	for _, position := range []*confirmation{response.Position, response.Data.Position} {
		if position == nil {
			continue
		}
		if position.ID != positionID || !strings.EqualFold(strings.TrimSpace(position.Status), "stopped") {
			return notConfirmed
		}
		confirmedPosition = true
	}
	for _, status := range []string{response.Status, response.Data.Status} {
		if strings.TrimSpace(status) == "" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(status), "stopped") {
			return notConfirmed
		}
		confirmedStatus = true
	}
	for _, ok := range []*bool{response.OK, response.Data.OK} {
		if ok != nil {
			if !*ok {
				return notConfirmed
			}
			confirmedOK = true
		}
	}
	// 旧版接口不返回岗位对象，只有明确的 ok=true 和 stopped 才能作为兼容确认。
	if !confirmedPosition && !(confirmedOK && confirmedStatus) {
		return notConfirmed
	}
	return nil
}

// ReportScreeningResult 通过已有流程接口同步真实终态筛选结果，供新手教程恢复完成状态。
func (c *Client) ReportScreeningResult(ctx context.Context, token string, positionID string, evidence ScreeningResultEvidence) error {
	if strings.TrimSpace(positionID) == "" {
		return fmt.Errorf("岗位编号不能为空")
	}
	if (evidence.Kind != "ai" && evidence.Kind != "keyword") || evidence.Phase != "result" || !evidence.Terminal {
		return fmt.Errorf("只有真实完成的 AI 或关键词筛选结果才能记录教程完成")
	}
	request := struct {
		Step       string                  `json:"step"`
		Status     string                  `json:"status"`
		Source     string                  `json:"source"`
		PositionID string                  `json:"position_id"`
		Metadata   ScreeningResultEvidence `json:"metadata"`
	}{Step: "first_screening_result", Status: "completed", Source: "local_agent_screening", PositionID: strings.TrimSpace(positionID), Metadata: evidence}
	return c.do(ctx, http.MethodPost, "/api/user-flow", token, request, nil)
}

// SyncSummary 把不含候选人详情的任务统计同步给云端。
func (c *Client) SyncSummary(ctx context.Context, token string, summary TaskSummary) error {
	_, err := c.syncSummary(ctx, token, summary)
	return err
}

// SyncPositionCounts 把主动打招呼任务的累计扫描、跳过和失败统计同步到云端岗位。
func (c *Client) SyncPositionCounts(ctx context.Context, token string, summary TaskSummary) error {
	if strings.TrimSpace(summary.PositionID) == "" {
		return fmt.Errorf("岗位编号不能为空")
	}
	request := struct {
		ScannedCount int `json:"scanned_count"`
		SkippedCount int `json:"skipped_count"`
		FailedCount  int `json:"failed_count"`
	}{
		ScannedCount: max(summary.Processed, 0),
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

// SavePositionCandidate 把开启结构化简历后的完整候选人结果写入云端简历库。
func (c *Client) SavePositionCandidate(ctx context.Context, token string, positionID string, candidate CandidateUpload) error {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return fmt.Errorf("岗位编号不能为空")
	}
	if strings.TrimSpace(candidate.CandidateName) == "" {
		return fmt.Errorf("候选人姓名为空，暂时不能同步到云端简历库")
	}
	path := "/api/positions/" + url.PathEscape(positionID) + "/candidates"
	return c.do(ctx, http.MethodPost, path, token, candidate, nil)
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
		Status          string `json:"status"`
		RunGreetedCount int    `json:"run_greeted_count"`
		RunSkippedCount int    `json:"run_skipped_count"`
	}{
		Status: summary.Status, RunGreetedCount: max(summary.RunGreetedCount, 0),
		RunSkippedCount: max(summary.RunSkippedCount, 0),
	}
	var response SummaryResult
	err := c.do(ctx, http.MethodPost, path, token, request, &response)
	return response, err
}

// SendFailNotice 通知云端按岗位和当前用户发送失败邮件。
func (c *Client) SendFailNotice(ctx context.Context, token string, positionID string, errorMessage string, runGreetedCount int, runSkippedCount int) error {
	request := struct {
		PositionID      string `json:"position_id"`
		ErrorMessage    string `json:"error_message"`
		RunGreetedCount int    `json:"run_greeted_count"`
		RunSkippedCount int    `json:"run_skipped_count"`
	}{
		PositionID: strings.TrimSpace(positionID), ErrorMessage: strings.TrimSpace(errorMessage),
		RunGreetedCount: max(runGreetedCount, 0), RunSkippedCount: max(runSkippedCount, 0),
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
		MouseSpeed:     "fast",
		ClickFrequency: 80, DetailOpenProbability: 80,
		CandidateReadConcurrency: 100,
		ScrollDelayMin:           3, ScrollDelayMax: 8,
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
	value.MouseSpeed = contract.NormalizeMouseSpeed(value.MouseSpeed)
	value.DetailOpenProbability = clamp(value.DetailOpenProbability, 0, 100)
	if value.CandidateReadConcurrency == 0 {
		value.CandidateReadConcurrency = 100
	} else {
		value.CandidateReadConcurrency = clamp(value.CandidateReadConcurrency, 5, 100)
	}
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
	mode := strings.ToLower(strings.TrimSpace(position.CommonConfig.ModeDefault))
	detailMode := strings.ToLower(strings.TrimSpace(position.CommonConfig.DetailMode))
	position.RequiresOCR = position.RequiresOCR || detailMode == "ocr"
	position.RequiresAI = position.RequiresAI || mode != "keyword" || detailMode == "ai"
	position.RequestPhone = position.RequestPhone || position.CommonConfig.RequestPhone
	position.RequestWechat = position.RequestWechat || position.CommonConfig.RequestWechat
	position.RequestResume = position.RequestResume || position.CommonConfig.RequestResume
	return position
}

// do 发送云端请求并解析强类型响应。
func (c *Client) do(ctx context.Context, method string, path string, token string, payload any, result any) error {
	return c.doWithMachineID(ctx, method, path, token, "", payload, result)
}

// doWithMachineID 发送可选携带稳定设备编号的云端 JSON 请求。
func (c *Client) doWithMachineID(ctx context.Context, method string, path string, token string, machineID string, payload any, result any) error {
	if handled, err := c.handleLocalAutoReply(ctx, method, path, AgentCredentials{Token: token, MachineID: machineID}, payload, result); handled {
		return err
	}
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
	return c.doBody(ctx, method, path, token, machineID, "application/json", body, result)
}

// doBody 发送已经编码好的请求体，并统一解析云端成功和错误响应。
func (c *Client) doBody(ctx context.Context, method string, path string, token string, machineID string, contentType string, body io.Reader, result any) error {
	if c.baseURL == "" {
		return fmt.Errorf("云端地址不能为空")
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("创建云端请求失败：%w", err)
	}
	request.Header.Set("Accept", "application/json")
	if strings.TrimSpace(contentType) != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if strings.TrimSpace(token) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	if strings.TrimSpace(machineID) != "" {
		request.Header.Set("X-GoodHR-Machine-ID", strings.TrimSpace(machineID))
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
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Error   json.RawMessage `json:"error"`
			Msg     string          `json:"msg"`
			ErrorID string          `json:"error_id"`
		}
		_ = json.Unmarshal(content, &failure)
		message := strings.TrimSpace(failure.Message)
		code := strings.TrimSpace(failure.Code)
		errorID := strings.TrimSpace(failure.ErrorID)
		if message == "" {
			var errorBody struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				ErrorID string `json:"error_id"`
			}
			if err := json.Unmarshal(failure.Error, &errorBody); err == nil {
				if code == "" {
					code = strings.TrimSpace(errorBody.Code)
				}
				message = strings.TrimSpace(errorBody.Message)
				errorID = strings.TrimSpace(errorBody.ErrorID)
			}
		}
		if message == "" && len(failure.Error) > 0 {
			var errorText string
			if err := json.Unmarshal(failure.Error, &errorText); err == nil {
				message = strings.TrimSpace(errorText)
			}
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
		return &APIError{StatusCode: response.StatusCode, Code: code, Message: message, ErrorID: errorID}
	}
	if result != nil && len(content) > 0 {
		if err := json.Unmarshal(content, result); err != nil {
			return fmt.Errorf("解析云端响应失败：%w", err)
		}
	}
	return nil
}
