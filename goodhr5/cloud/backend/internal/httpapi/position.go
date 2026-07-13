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
	systemConfigs SystemConfigStore
	aiConfigStore AIConfigStore
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
}

type optimizeRequirementRequest struct {
	Text string `json:"text"`
}

// NewPositionService 创建岗位配置 API 服务，并注入认证服务和岗位存储。
func NewPositionService(auth *AuthService, store PositionStore, systemConfigs SystemConfigStore, aiConfigStore AIConfigStore) *PositionService {
	return &PositionService{
		auth:          auth,
		store:         store,
		systemConfigs: systemConfigs,
		aiConfigStore: aiConfigStore,
		httpClient:    &http.Client{Timeout: 120 * time.Second},
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

	// 调用岗位存储读取当前用户的岗位配置，供后续任务选择和复用。
	items, err := s.store.ListPositions("", session.Email, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list positions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"positions": publicPositions(items),
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
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"optimized": optimized,
	})
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

	// 调用岗位存储保存岗位配置，用于后续任务快速选择筛选条件。
	saved, err := s.store.SavePosition(position)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save position")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"position": publicPosition(saved),
	})
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

	// 调用岗位存储删除岗位配置，避免继续出现在任务配置候选项里。
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
	}

	if position.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return Position{}, false
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

// publicPosition 将岗位配置转换为前端响应结构。
func publicPosition(item Position) map[string]any {
	return map[string]any{
		"id":               item.ID,
		"platform_id":      normalizePositionPlatformID(item.PlatformID),
		"name":             item.Name,
		"keywords":         item.Keywords,
		"exclude_keywords": item.ExcludeKeywords,
		"description":      item.Description,
		"greet_message":    item.GreetMessage,
		"is_and_mode":      item.IsAndMode,
		"common_config":    cloneMap(item.CommonConfig),
		"ai_config":        cloneMap(item.AIConfig),
		"keyword_config":   cloneMap(item.KeywordConfig),
		"created_at":       item.CreatedAt,
		"updated_at":       item.UpdatedAt,
	}
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
