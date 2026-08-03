// 本文件负责提供岗位配置的 HTTP API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultPositionRequirementOptimizePrompt = `你是一个招聘筛选规则整理助手。请把用户输入的岗位要求整理成适合 AI 筛选候选人简历的规则。

要求：
1. 只保留候选人自身条件，不要保留岗位福利、薪资待遇、工作时间、公司介绍、岗位职责、工作内容。
2. 去掉无法从简历中稳定判断的主观要求，例如：有上进心、责任心强、抗压能力强、沟通能力好、性格开朗、团队意识强、吃苦耐劳等。
3. 优先保留硬性条件，例如：学历、专业、工作年限、行业经验、岗位经验、证书、技能、城市、年龄、到岗状态。
4. 如果原文里有模糊条件，请改写成更清晰的筛选规则。
5. 输出中文，按条目列出，不要解释，不要输出 JSON。
6. 禁止输出 Markdown，禁止输出代码块。

用户输入：
{{input}}`

// PositionService 处理岗位配置的创建、查询和删除请求。
type PositionService struct {
	auth          *AuthService
	store         PositionStore
	subscriptions SubscriptionStore
	systemConfigs SystemConfigStore
	aiConfigStore AIConfigStore
	userFlow      UserFlowStore
	httpClient    *http.Client
}

type positionRequest struct {
	ID              string         `json:"id"`
	PlatformID      string         `json:"platform_id"`
	Name            string         `json:"name"`
	Keywords        []string       `json:"keywords"`
	ExcludeKeywords []string       `json:"exclude_keywords"`
	Description     string         `json:"description"`
	GreetMessage    string         `json:"greet_message"`
	IsAndMode       bool           `json:"is_and_mode"`
	CommonConfig    map[string]any `json:"common_config"`
	AIConfig        map[string]any `json:"ai_config"`
	KeywordConfig   map[string]any `json:"keyword_config"`
	MatchLimit      int            `json:"match_limit"`
	EnableSound     bool           `json:"enable_sound"`
	EnableThinking  bool           `json:"enable_thinking"`
}

type optimizeRequirementRequest struct {
	Text string `json:"text"`
}

// NewPositionService 创建岗位配置 API 服务，并注入认证服务和岗位存储。
func NewPositionService(auth *AuthService, store PositionStore, subscriptions SubscriptionStore, systemConfigs SystemConfigStore, aiConfigStore AIConfigStore, userFlow UserFlowStore) *PositionService {
	return &PositionService{
		auth:          auth,
		store:         store,
		subscriptions: subscriptions,
		systemConfigs: systemConfigs,
		aiConfigStore: aiConfigStore,
		userFlow:      userFlow,
		httpClient:    &http.Client{Timeout: aiRequestTimeout},
	}
}

// Collection 按请求方法处理岗位配置集合资源。
func (s *PositionService) Collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.List(w, r)
	case http.MethodPost:
		s.Save(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// List 返回当前登录用户的岗位配置列表。
func (s *PositionService) List(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	tenantID := ""
	if s.auth != nil && s.auth.tenantStore != nil {
		tenant, tenantErr := s.auth.tenantStore.GetOrCreateTenant(session.Email)
		if tenantErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to get tenant")
			return
		}
		tenantID = tenant.ID
	}
	// 调用岗位存储读取团队岗位，当前账号创建的岗位优先，其余岗位仅供查看。
	items, err := s.store.ListPositions(tenantID, session.Email, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list positions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"positions": publicPositionsForUser(items, session.Email),
	})
}

// OptimizeRequirement 使用当前用户 AI 配置优化岗位要求。
// 会从系统其它配置读取优化提示词，没有配置时使用内置默认提示词。
func (s *PositionService) OptimizeRequirement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	var req optimizeRequirementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	input := strings.TrimSpace(req.Text)
	if input == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if !s.requireAIMembership(w, session.Email) {
		return
	}
	aiConfig, err := s.aiConfigStore.UserConfig(session.Email)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusConflict, "请先在个人配置里填写并启用 AI 配置")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ai config")
		return
	}
	if !aiConfig.Enabled {
		writeError(w, http.StatusConflict, "个人 AI 配置未启用")
		return
	}
	if strings.TrimSpace(aiConfig.BaseURL) == "" || strings.TrimSpace(aiConfig.Model) == "" || strings.TrimSpace(aiConfig.APIKey) == "" {
		writeError(w, http.StatusConflict, "个人 AI 配置不完整")
		return
	}
	prompt := s.positionRequirementOptimizePrompt(input)
	optimized, err := s.callRequirementOptimizeAI(r, aiConfig, prompt)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	result := map[string]any{
		"ok":        true,
		"optimized": optimized,
	}
	writeJSON(w, http.StatusOK, result)
}

// Save 创建或更新一个岗位配置。
func (s *PositionService) Save(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	var req positionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	position, ok := req.toPosition(w, session.Email)
	if !ok {
		return
	}
	if positionUsesAI(position) && !s.requireAIMembership(w, session.Email) {
		return
	}

	// 调用岗位存储保存岗位配置，用于后续岗位运行快速选择筛选条件。
	saved, err := s.store.SavePosition(position)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save position")
		return
	}
	if s.userFlow != nil {
		_, _ = s.userFlow.Record(session.Email, UserFlowUpdate{Step: userFlowPositionCreated, Status: "completed", Source: "cloud_backend"})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"position": publicPosition(saved),
	})
}

// requireAIMembership 统一检查用户是否可以使用岗位 AI 功能。
func (s *PositionService) requireAIMembership(w http.ResponseWriter, email string) bool {
	if s.subscriptions == nil {
		writeError(w, http.StatusServiceUnavailable, "会员状态暂时没查清楚，请稍后再试")
		return false
	}
	subscription, err := s.subscriptions.UserSubscription(email)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "会员状态暂时没查清楚，请稍后再试")
		return false
	}
	access, err := subscriptionAccess(s.systemConfigs, subscription, time.Now())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "会员套餐配置暂时没读明白，请稍后再试")
		return false
	}
	if !access.AllowAI {
		writeError(w, http.StatusForbidden, "AI 筛选和 AI 详情识别需要有效的 Plus 或 Max 会员")
		return false
	}
	return true
}

// Delete 删除当前登录用户的岗位配置。
func (s *PositionService) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	positionID := strings.TrimPrefix(r.URL.Path, "/api/positions/")
	if positionID == "" || positionID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "position id is required")
		return
	}

	// 调用岗位存储删除岗位配置，避免继续出现在岗位运行配置候选项里。
	err := s.store.DeletePosition(session.Email, positionID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete position")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

// Detail 读取、更新或删除单个岗位，供前端和本地程序共用同一岗位快照。
func (s *PositionService) Detail(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.Delete(w, r)
		return
	}
	if r.Method == http.MethodPut {
		s.Save(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	positionID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/positions/"))
	if positionID == "" || strings.Contains(positionID, "/") {
		writeError(w, http.StatusBadRequest, "position id is required")
		return
	}
	position, err := s.store.PositionByID("", session.Email, positionID, false)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "position": publicPosition(position)})
}

// positionRequirementOptimizePrompt 生成岗位要求优化提示词。
// input 为用户输入的原始岗位要求，返回已替换占位符的完整提示词。
func (s *PositionService) positionRequirementOptimizePrompt(input string) string {
	template := defaultPositionRequirementOptimizePrompt
	if s.systemConfigs != nil {
		if cfg, err := s.systemConfigs.Get("system.app_config"); err == nil {
			var appConfig map[string]any
			if json.Unmarshal([]byte(cfg.ConfigValue), &appConfig) == nil {
				if value, ok := appConfig["position_requirement_optimize_prompt"].(string); ok && strings.TrimSpace(value) != "" {
					template = strings.TrimSpace(value)
				}
			}
		}
	}
	if strings.Contains(template, "{{input}}") {
		return strings.ReplaceAll(template, "{{input}}", input)
	}
	return template + "\n\n用户输入：\n" + input
}

// callRequirementOptimizeAI 调用 OpenAI 兼容接口优化岗位要求。
// r 为当前请求，aiConfig 为用户个人 AI 配置，prompt 为完整提示词。
func (s *PositionService) callRequirementOptimizeAI(r *http.Request, aiConfig AIConfig, prompt string) (string, error) {
	reqBody := AIRequest{
		Model:       strings.TrimSpace(aiConfig.Model),
		Messages:    []AIMsg{{Role: "user", Content: prompt}},
		Temperature: aiConfig.Temperature,
	}
	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimSpace(aiConfig.BaseURL), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(aiConfig.APIKey))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("AI API 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("AI API 错误 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var aiResp AIResponse
	if err := json.Unmarshal(body, &aiResp); err != nil {
		return "", fmt.Errorf("解析 AI 响应失败: %w", err)
	}
	if len(aiResp.Choices) == 0 {
		return "", fmt.Errorf("AI 未返回结果")
	}
	optimized := cleanAITextOutput(aiResp.Choices[0].Message.Content)
	if optimized == "" {
		return "", fmt.Errorf("AI 返回内容为空")
	}
	return optimized, nil
}

// currentSession 从请求中解析登录会话。
func (s *PositionService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	// 调用认证服务解析请求会话，避免岗位配置 API 自己重复处理 token。
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

// toPosition 将请求结构转换为岗位配置模型。
func (r positionRequest) toPosition(w http.ResponseWriter, userEmail string) (Position, bool) {
	position := Position{
		ID:              strings.TrimSpace(r.ID),
		UserEmail:       userEmail,
		PlatformID:      normalizePositionPlatformID(r.PlatformID),
		Name:            strings.TrimSpace(r.Name),
		Keywords:        trimStringList(r.Keywords),
		ExcludeKeywords: trimStringList(r.ExcludeKeywords),
		Description:     strings.TrimSpace(r.Description),
		GreetMessage:    strings.TrimSpace(r.GreetMessage),
		IsAndMode:       r.IsAndMode,
		CommonConfig:    cloneMap(r.CommonConfig),
		AIConfig:        cloneMap(r.AIConfig),
		KeywordConfig:   cloneMap(r.KeywordConfig),
		MatchLimit:      r.MatchLimit,
		EnableSound:     r.EnableSound,
		EnableThinking:  r.EnableThinking,
	}

	if position.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return Position{}, false
	}
	if position.MatchLimit <= 0 {
		position.MatchLimit = 50
	}
	applyPositionPlatformRules(&position)
	return position, true
}

// normalizePositionPlatformID 标准化岗位模板所属平台。
// platformID 为空时默认使用 boss，返回标准平台标识。
func normalizePositionPlatformID(platformID string) string {
	value := strings.TrimSpace(strings.ToLower(platformID))
	if value == "" {
		return "boss"
	}
	return value
}

// applyPositionPlatformRules 根据平台修正岗位模板参数。
// position 为岗位模板；Boss 不支持 DOM 时改为 OCR，智联和猎聘只允许 DOM。
func applyPositionPlatformRules(position *Position) {
	if position == nil {
		return
	}
	if position.CommonConfig == nil {
		position.CommonConfig = map[string]any{}
	}
	if _, ok := position.CommonConfig["output_structured_resume"]; !ok {
		position.CommonConfig["output_structured_resume"] = false
	}
	if isDOMOnlyPlatform(position.PlatformID) {
		position.CommonConfig["detail_mode"] = "dom"
		return
	}
	if strings.EqualFold(position.PlatformID, "boss") && strings.EqualFold(fmt.Sprint(position.CommonConfig["detail_mode"]), "dom") {
		position.CommonConfig["detail_mode"] = "ocr"
	}
}

// isDOMOnlyPlatform 判断平台是否只支持 DOM 详情识别。
// platformID 为招聘平台标识。
func isDOMOnlyPlatform(platformID string) bool {
	switch strings.ToLower(strings.TrimSpace(platformID)) {
	case "hliepin", "liepin", "zhaopin":
		return true
	default:
		return false
	}
}

// publicPositions 将岗位配置列表转换为前端响应结构。
func publicPositions(items []Position) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicPosition(item))
	}
	return result
}

// publicPositionsForUser 返回团队岗位，并标记哪些岗位属于当前登录账号。
func publicPositionsForUser(items []Position, currentEmail string) []map[string]any {
	result := publicPositions(items)
	for index, item := range items {
		result[index]["is_current_user"] = strings.EqualFold(item.UserEmail, currentEmail)
	}
	return result
}

// publicPosition 将岗位配置转换为前端响应结构。
func publicPosition(item Position) map[string]any {
	return map[string]any{
		"id":                  item.ID,
		"creator_email":       item.UserEmail,
		"platform_id":         normalizePositionPlatformID(item.PlatformID),
		"name":                item.Name,
		"keywords":            item.Keywords,
		"exclude_keywords":    item.ExcludeKeywords,
		"description":         item.Description,
		"greet_message":       item.GreetMessage,
		"is_and_mode":         item.IsAndMode,
		"common_config":       cloneMap(item.CommonConfig),
		"ai_config":           cloneMap(item.AIConfig),
		"keyword_config":      cloneMap(item.KeywordConfig),
		"match_limit":         item.MatchLimit,
		"enable_sound":        item.EnableSound,
		"enable_thinking":     item.EnableThinking,
		"status":              item.Status,
		"scanned_count":       item.ScannedCount,
		"daily_greeted_count": item.DailyGreetedCount,
		"daily_greeted_date":  item.DailyGreetedDate,
		"today_greeted_count": positionTodayGreetedCount(item),
		"skipped_count":       item.SkippedCount,
		"failed_count":        item.FailedCount,
		"started_at":          item.StartedAt,
		"finished_at":         item.FinishedAt,
		"created_at":          item.CreatedAt,
		"updated_at":          item.UpdatedAt,
	}
}

// positionWithRunGreeted 计算一次岗位结束后用于通知展示的今日打招呼数量。
// item 为岗位记录，greeted 为本次打招呼数量。
func positionWithRunGreeted(item Position, greeted int) Position {
	if greeted <= 0 {
		return item
	}
	today := positionBusinessDate(time.Now())
	if item.DailyGreetedDate != today {
		item.DailyGreetedDate = today
		item.DailyGreetedCount = 0
	}
	item.DailyGreetedCount += greeted
	return item
}

// positionTodayGreetedCount 返回岗位当天打招呼数量。
// item 为岗位记录，日期不是今天时返回零。
func positionTodayGreetedCount(item Position) int {
	if item.DailyGreetedDate != positionBusinessDate(time.Now()) || item.DailyGreetedCount < 0 {
		return 0
	}
	return item.DailyGreetedCount
}

// positionDefaultMode 返回岗位配置使用的默认筛选模式。
// position 为岗位配置，返回 keyword 或 ai。
func positionDefaultMode(position Position) string {
	mode := strings.TrimSpace(fmt.Sprint(position.CommonConfig["mode_default"]))
	if mode == "keyword" {
		return "keyword"
	}
	return "ai"
}

// trimStringList 清理字符串数组里的空白项。
func trimStringList(items []string) []string {
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
