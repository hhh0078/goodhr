// 本文件负责云端任务执行编排，按平台配置调用 Local Agent API 完成候选人筛选流程。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

type claimedTaskCookie struct {
	CookieID      string
	EncryptedData string
	EncryptedKeys map[string]string
	DisplayName   string
}

// TaskExecutor 负责任务的云端编排执行。
type TaskExecutor struct {
	task           TaskRun
	platformCfg    PlatformConfig
	filter         *KeywordFilter
	position       map[string]any
	aiConfig       AIConfig
	defaultPrompts DefaultPrompts
	userPrefs      UserPreferences
	agentWS        *AgentWSHub
	httpClient     *http.Client
	logCallback    func(level, message string)
	countCallback  func(scanned, greeted, skipped, failed int)
	cookies        []map[string]any
	claimedCookie  *claimedTaskCookie
	seenCandidates map[string]struct{}
	scannedCount   int
	greetedCount   int
	skippedCount   int
	failedCount    int
}

// NewTaskExecutor 创建任务编排器实例。
func NewTaskExecutor(
	task TaskRun,
	platformCfg PlatformConfig,
	position map[string]any,
	agentWS *AgentWSHub,
	aiConfig AIConfig,
	defaultPrompts DefaultPrompts,
	userPrefs UserPreferences,
	claimedCookie *claimedTaskCookie,
	logCallback func(level, message string),
	countCallback func(scanned, greeted, skipped, failed int),
) *TaskExecutor {
	var filter *KeywordFilter
	if task.Mode != "ai" && position != nil {
		keywords := toStringSlice(position["keywords"])
		exclude := toStringSlice(position["exclude"])
		isAndMode := false
		if v, ok := position["is_and_mode"].(bool); ok {
			isAndMode = v
		}
		filter = NewKeywordFilter(keywords, exclude, isAndMode, 7)
	}

	return &TaskExecutor{
		task:           task,
		platformCfg:    platformCfg,
		filter:         filter,
		position:       position,
		aiConfig:       aiConfig,
		defaultPrompts: defaultPrompts,
		userPrefs:      userPrefs,
		agentWS:        agentWS,
		httpClient:     &http.Client{Timeout: 120 * time.Second},
		logCallback:    logCallback,
		countCallback:  countCallback,
		claimedCookie:  claimedCookie,
		seenCandidates: make(map[string]struct{}),
	}
}

// Run 执行任务编排主流程。
func (e *TaskExecutor) Run(ctx context.Context) error {
	e.log("info", "任务执行开始")

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.prepareCookies(); err != nil {
		return fmt.Errorf("准备 cookie 失败: %w", err)
	}

	// 1. 启动浏览器
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.startBrowser(); err != nil {
		return fmt.Errorf("启动浏览器失败: %w", err)
	}
	defer e.stopBrowser()

	// 2. 打开平台推荐页
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.openPage(); err != nil {
		return fmt.Errorf("打开页面失败: %w", err)
	}

	// 3. 先处理当前可见候选人，处理完后再滚动下一屏
	idleRounds := 0
	for round := 1; ; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.reachedMatchLimit() {
			e.log("info", fmt.Sprintf("已达到任务上限 %d，停止继续处理", e.task.MatchLimit))
			break
		}

		e.log("info", fmt.Sprintf("开始处理第 %d 轮当前可见候选人", round))
		candidates, err := e.extractCandidates()
		if err != nil {
			return fmt.Errorf("提取候选人失败: %w", err)
		}
		if len(candidates) == 0 {
			e.log("warn", "当前可见区域未找到候选人")
		}
		newCandidates := e.filterNewCandidates(candidates)
		if len(newCandidates) == 0 {
			idleRounds++
			if idleRounds >= 2 {
				e.log("info", "连续两轮都没有新的可见候选人，结束本次任务")
				break
			}
			e.log("info", fmt.Sprintf("第 %d 轮没有新的可见候选人，准备滚动下一屏", round))
			if err := e.scrollPage(); err != nil {
				return fmt.Errorf("滚动加载失败: %w", err)
			}
			continue
		}

		idleRounds = 0
		e.log("info", fmt.Sprintf("第 %d 轮提取到 %d 个候选人，其中 %d 个为新候选人", round, len(candidates), len(newCandidates)))
		if err := e.processCandidates(ctx, newCandidates); err != nil {
			return fmt.Errorf("处理候选人失败: %w", err)
		}
		if e.reachedMatchLimit() {
			e.log("info", fmt.Sprintf("已达到任务上限 %d，停止继续处理", e.task.MatchLimit))
			break
		}
		e.log("info", fmt.Sprintf("第 %d 轮当前可见候选人处理完成，准备滚动下一屏", round))
		if err := e.scrollPage(); err != nil {
			return fmt.Errorf("滚动加载失败: %w", err)
		}
	}

	e.log("info", "任务执行完成")
	return nil
}

// startBrowser 调用 Local Agent 启动 CloakBrowser。
func (e *TaskExecutor) startBrowser() error {
	e.log("info", "正在启动浏览器")
	body := map[string]any{
		"persistent":    true,
		"user_data_dir": e.task.PlatformAccountID,
		"headless":      false,
		"humanize":      true,
	}
	if len(e.cookies) > 0 {
		e.log("info", fmt.Sprintf("启动浏览器时注入 %d 条 cookie", len(e.cookies)))
		body["cookies"] = e.cookies
	}
	var resp struct {
		Ok     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if err := e.post("/api/v1/browser/start", body, &resp); err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("启动失败: %s", resp.Status)
	}
	return nil
}

// stopBrowser 调用 Local Agent 关闭浏览器。
func (e *TaskExecutor) stopBrowser() {
	e.log("info", "正在关闭浏览器")
	_ = e.post("/api/v1/browser/stop", nil, nil)
}

// openPage 打开平台推荐页。
func (e *TaskExecutor) openPage() error {
	return e.platformCfg.OpenEntryPage(e, e.cookies)
}

func (e *TaskExecutor) prepareCookies() error {
	if e.claimedCookie == nil {
		e.log("warn", "当前任务未绑定平台账号 cookie，将按未登录状态继续执行")
		return nil
	}
	e.log("info", fmt.Sprintf("准备解密任务 cookie：账号=%s cookie=%s", e.claimedCookie.DisplayName, e.claimedCookie.CookieID))
	var resp struct {
		Ok      bool             `json:"ok"`
		Cookies []map[string]any `json:"cookies"`
		Count   int              `json:"count"`
	}
	if err := e.post("/api/v1/cookies/decrypt", map[string]any{
		"encrypted_data": e.claimedCookie.EncryptedData,
		"encrypted_keys": e.claimedCookie.EncryptedKeys,
	}, &resp); err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("本地程序未返回成功状态")
	}
	e.cookies = resp.Cookies
	e.log("info", fmt.Sprintf("任务 cookie 解密成功，共 %d 条", len(e.cookies)))
	return nil
}

// scrollPage 滚动加载候选人列表。
func (e *TaskExecutor) scrollPage() error {
	return e.platformCfg.ScrollCandidateList(e, e.userPrefs)
}

// extractCandidates 从页面提取候选人卡片。
func (e *TaskExecutor) extractCandidates() ([]map[string]any, error) {
	return e.platformCfg.ListVisibleCandidates(e)
}

// processCandidates 逐候选人筛选和打招呼。
func (e *TaskExecutor) processCandidates(ctx context.Context, candidates []map[string]any) error {
	for i := range candidates {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if e.reachedMatchLimit() {
			e.log("info", fmt.Sprintf("已达到任务上限 %d，本轮停止继续处理候选人", e.task.MatchLimit))
			return nil
		}

		e.log("info", fmt.Sprintf("处理候选人 %d/%d", i+1, len(candidates)))
		e.incrementCounts(1, 0, 0, 0)

		baseText := strings.TrimSpace(e.platformCfg.CandidateFilterText(candidates[i]))
		shouldOpenDetail, detailReason, tokenUsage, err := e.decideOpenDetail(baseText)
		if err != nil {
			e.log("error", fmt.Sprintf("候选人 %d 详情决策失败: %v", i+1, err))
			e.incrementCounts(0, 0, 0, 1)
			continue
		}
		if detailReason != "" {
			e.log("info", fmt.Sprintf("候选人 %d 详情决策: %s（token=%d）", i+1, detailReason, tokenUsage))
		}
		detailText := ""
		if shouldOpenDetail {
			detailText, err = e.platformCfg.FetchCandidateDetailText(e, e.userPrefs, candidates[i], e.positionDetailMode())
			if err != nil {
				e.log("error", fmt.Sprintf("候选人 %d 详情提取失败: %v", i+1, err))
				e.incrementCounts(0, 0, 0, 1)
				continue
			}
		}
		filterText := e.mergeCandidateTexts(baseText, detailText)

		// 筛选逻辑
		if e.task.Mode == "ai" {
			jobDesc := e.positionDescription()
			decision, err := e.callAI(jobDesc, filterText)
			if err != nil {
				e.log("error", fmt.Sprintf("AI 筛选失败: %v", err))
				e.incrementCounts(0, 0, 0, 1)
				continue
			}
			if !decision.IsOK {
				e.log("info", fmt.Sprintf("候选人 %d AI 筛选跳过: %s", i+1, decision.Msg))
				e.incrementCounts(0, 0, 1, 0)
				continue
			}
			e.log("info", fmt.Sprintf("候选人 %d AI 通过: %s", i+1, decision.Msg))
		} else if e.filter != nil {
			result := e.filter.Filter(filterText)
			if !result.Passed {
				e.log("info", fmt.Sprintf("候选人 %d 被筛选跳过: %s", i+1, result.Reason))
				e.incrementCounts(0, 0, 1, 0)
				continue
			}
			e.log("info", fmt.Sprintf("候选人 %d 通过筛选: %s", i+1, result.Reason))
		}

		// 打招呼：交由平台动作实现
		if err := e.platformCfg.GreetCandidate(e, e.userPrefs, candidates[i]); err != nil {
			e.log("error", fmt.Sprintf("候选人 %d 打招呼失败: %v", i+1, err))
			e.incrementCounts(0, 0, 0, 1)
			continue
		}
		e.log("info", fmt.Sprintf("候选人 %d 打招呼成功", i+1))
		e.incrementCounts(0, 1, 0, 0)
		if e.task.EnableSound {
			if err := e.playSuccessSound(); err != nil {
				e.log("warn", fmt.Sprintf("播放成功提示音失败: %v", err))
			}
		}
	}
	return nil
}

// decideOpenDetail 根据任务模式决定本次是否需要打开详情。
func (e *TaskExecutor) decideOpenDetail(baseText string) (bool, string, int, error) {
	if strings.TrimSpace(baseText) == "" {
		return false, "基础信息为空，跳过详情", 0, nil
	}
	if e.task.Mode == "ai" {
		decision, err := e.callOpenDetailAI(e.positionDescription(), baseText)
		if err != nil {
			return false, "", 0, err
		}
		return decision.ShouldOpenDetail, decision.Reason, decision.TokenUsage, nil
	}
	return rollDetailOpenByProbability(e.userPrefs.DetailOpenProbability)
}

// mergeCandidateTexts 合并候选人基础信息和详情文本，供筛选流程使用。
func (e *TaskExecutor) mergeCandidateTexts(baseText, detailText string) string {
	base := strings.TrimSpace(baseText)
	detail := strings.TrimSpace(detailText)
	if detail == "" {
		return base
	}
	if base == "" {
		return detail
	}
	return base + "\n详情信息：\n" + detail
}

// positionDetailMode 返回岗位模板配置的详情读取模式。
func (e *TaskExecutor) positionDetailMode() string {
	if e.position == nil {
		return "dom"
	}
	common, _ := e.position["common_config"].(map[string]any)
	if mode, ok := common["detail_mode"].(string); ok && strings.TrimSpace(mode) != "" {
		return strings.TrimSpace(mode)
	}
	return "dom"
}

func (e *TaskExecutor) incrementCounts(scanned, greeted, skipped, failed int) {
	e.scannedCount += scanned
	e.greetedCount += greeted
	e.skippedCount += skipped
	e.failedCount += failed
	if e.countCallback != nil {
		e.countCallback(scanned, greeted, skipped, failed)
	}
}

// reachedMatchLimit 判断当前任务是否已经达到打招呼上限。
func (e *TaskExecutor) reachedMatchLimit() bool {
	if e.task.MatchLimit <= 0 {
		return false
	}
	return e.greetedCount >= e.task.MatchLimit
}

// filterNewCandidates 过滤掉当前任务轮次里已经处理过的候选人。
func (e *TaskExecutor) filterNewCandidates(candidates []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		key := e.platformCfg.CandidateFingerprint(candidate)
		if key == "" {
			result = append(result, candidate)
			continue
		}
		if _, exists := e.seenCandidates[key]; exists {
			continue
		}
		e.seenCandidates[key] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func (e *TaskExecutor) playSuccessSound() error {
	return e.post("/api/v1/sound/play", map[string]any{
		"kind": "success",
	}, nil)
}

// ---------- Local Agent WebSocket 客户端 ----------

// post 通过 WebSocket 向 Local Agent 发送浏览器操作请求。
func (e *TaskExecutor) post(path string, body any, result any) error {
	if e.agentWS == nil {
		return fmt.Errorf("Local Agent WebSocket 未初始化")
	}
	e.log("info", fmt.Sprintf("正在请求本地程序：%s", path))
	payload := map[string]any{
		"path": path,
		"body": body,
	}
	resp, err := e.agentWS.SendCommand(e.task.UserEmail, AgentWSMessage{
		Type:    "local.http.post",
		TaskID:  e.task.ID,
		Payload: payload,
	}, 3)
	if err != nil {
		e.log("error", fmt.Sprintf("本地程序请求失败：%s，err=%v", path, err))
		if detail := localAgentReplyDetail(resp); detail != "" {
			e.log("error", fmt.Sprintf("本地程序详细错误：%s", detail))
		}
		return fmt.Errorf("请求 Local Agent 失败 (%s): %w", path, err)
	}
	e.log("info", fmt.Sprintf("本地程序响应成功：%s", path))

	if result != nil {
		respBytes, err := json.Marshal(resp.Payload)
		if err != nil {
			return fmt.Errorf("序列化 Local Agent 响应失败: %w", err)
		}
		if err := json.Unmarshal(respBytes, result); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
	}

	return nil
}

func localAgentReplyDetail(resp AgentWSMessage) string {
	if len(resp.Payload) == 0 {
		return ""
	}
	if traceback, ok := resp.Payload["traceback"].(string); ok && strings.TrimSpace(traceback) != "" {
		return strings.TrimSpace(traceback)
	}
	if detail, ok := resp.Payload["detail"].(string); ok && strings.TrimSpace(detail) != "" {
		return strings.TrimSpace(detail)
	}
	return ""
}

// log 记录任务执行日志。
func (e *TaskExecutor) log(level, message string) {
	if e.logCallback != nil {
		e.logCallback(level, message)
	}
}

// toStringSlice 将 interface{} 转为 []string。
func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func greetDelayBefore(prefs UserPreferences) float64 {
	if prefs.GreetDelayMax > prefs.GreetDelayMin && prefs.GreetDelayMin >= 0 {
		return (prefs.GreetDelayMin + prefs.GreetDelayMax) / 2
	}
	if prefs.GreetDelayMin >= 0 {
		return prefs.GreetDelayMin
	}
	return 1
}

func detailDelayBefore(prefs UserPreferences) float64 {
	if prefs.DetailViewDelayMax > prefs.DetailViewDelayMin && prefs.DetailViewDelayMin >= 0 {
		return (prefs.DetailViewDelayMin + prefs.DetailViewDelayMax) / 2
	}
	if prefs.DetailViewDelayMin >= 0 {
		return prefs.DetailViewDelayMin
	}
	return 1
}

// ---------- AI 筛选 ----------

type AIRequest struct {
	Model          string            `json:"model"`
	Messages       []AIMsg           `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
}
type AIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
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
type AIDecision struct {
	IsOK bool   `json:"isok"`
	Msg  string `json:"msg"`
}

type OpenDetailDecision struct {
	ShouldOpenDetail bool   `json:"should_open_detail"`
	Reason           string `json:"reason"`
	TokenUsage       int    `json:"token_usage"`
}

const defaultAIFilterPrompt = `你是一个资深的HR专家。请根据岗位要求判断候选人是否值得继续沟通。

重要提示：
1. 这个API仅用于岗位与候选人的筛选。
2. 请根据岗位要求判断候选人是否值得继续沟通。
3. 必须返回JSON格式，包含isok和msg两个字段。
4. isok字段只能是true或false。
5. msg字段是决策原因，10个字以内。

岗位要求：
%s

候选人基本信息：
%s

请判断是否值得继续沟通，返回JSON格式：{"isok": true, "msg": "符合基本要求"}`

const defaultOpenDetailPrompt = `你是一个资深的HR专家。请根据岗位要求和候选人的基础信息，判断这次是否值得打开候选人详情。

重要提示：
1. 仅根据“当前基础信息”来决定是否需要打开详情，不要直接给出最终录用判断。
2. 必须返回JSON格式，包含should_open_detail和reason两个字段。
3. should_open_detail字段只能是true或false。
4. reason字段控制在20字以内。

岗位要求：
%s

候选人基础信息：
%s

请返回JSON：{"should_open_detail": true, "reason": "基础信息值得深看"}`

// positionDescription 从岗位信息中提取职位要求文本。
func (e *TaskExecutor) positionDescription() string {
	if requirement := e.positionAIConfigString("position_requirement"); requirement != "" {
		return requirement
	}
	if e.position == nil {
		return ""
	}
	if desc, ok := e.position["name"].(string); ok && desc != "" {
		return desc
	}
	return ""
}

// positionAIConfigString 读取岗位模板中的 AI 文本配置。
func (e *TaskExecutor) positionAIConfigString(keys ...string) string {
	if e.position == nil {
		return ""
	}
	aiConfig, _ := e.position["ai_config"].(map[string]any)
	for _, key := range keys {
		if value, ok := aiConfig[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// aiRequestConfig 返回当前任务使用的 AI 请求配置。
func (e *TaskExecutor) aiRequestConfig() (string, string, float64) {
	model := strings.TrimSpace(e.aiConfig.Model)
	baseURL := strings.TrimSpace(e.aiConfig.BaseURL)
	temperature := e.aiConfig.Temperature

	if e.userPrefs.AIModel != "" {
		model = e.userPrefs.AIModel
	}
	return model, baseURL, temperature
}

// doAIChat 调用 AI API，返回原始文本和 token 消耗。
func (e *TaskExecutor) doAIChat(prompt string, forceJSON bool) (string, int, error) {
	model, baseURL, temperature := e.aiRequestConfig()
	if baseURL == "" {
		return "", 0, fmt.Errorf("AI 配置缺少 base_url")
	}
	if model == "" {
		return "", 0, fmt.Errorf("AI 配置缺少 model")
	}
	if e.aiConfig.APIKey == "" {
		return "", 0, fmt.Errorf("AI 配置缺少 API Key")
	}
	reqBody := AIRequest{
		Model:       model,
		Messages:    []AIMsg{{Role: "user", Content: prompt}},
		Temperature: temperature,
	}
	if forceJSON {
		reqBody.ResponseFormat = map[string]string{"type": "json_object"}
	}
	data, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.aiConfig.APIKey)
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("AI API 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("AI API 错误 %d", resp.StatusCode)
	}
	var aiResp AIResponse
	if err := json.Unmarshal(body, &aiResp); err != nil {
		return "", 0, fmt.Errorf("解析 AI 响应失败: %w", err)
	}
	if len(aiResp.Choices) == 0 {
		return "", aiResp.Usage.TotalTokens, fmt.Errorf("AI 未返回结果")
	}
	return strings.TrimSpace(aiResp.Choices[0].Message.Content), aiResp.Usage.TotalTokens, nil
}

// decodeJSONWithRetry 解析 AI JSON 输出，失败时要求 AI 重新只输出一次合法 JSON。
func (e *TaskExecutor) decodeJSONWithRetry(raw string, target any) error {
	if tryDecodeJSON(raw, target) == nil {
		return nil
	}
	repairPrompt := fmt.Sprintf(
		"下面这段内容本应是一个合法 JSON，但当前无法解析。请只返回一次合法 JSON，不要添加解释。\n原始输出：\n%s",
		raw,
	)
	repaired, _, err := e.doAIChat(repairPrompt, true)
	if err != nil {
		return err
	}
	if err := tryDecodeJSON(repaired, target); err != nil {
		return fmt.Errorf("AI JSON 解析失败: %w", err)
	}
	return nil
}

// callAI 调用 AI API 对候选人进行筛选。
func (e *TaskExecutor) callAI(jobDesc, candidateText string) (AIDecision, error) {
	prompt := fmt.Sprintf(defaultAIFilterPrompt, jobDesc, candidateText)
	if customPrompt := e.effectivePrompt(e.defaultPrompts.FilterPrompt, "filter_prompt", "click_prompt"); customPrompt != "" {
		prompt = buildPromptFromTemplate(customPrompt, jobDesc, candidateText, prompt, "补充规则")
	}
	content, _, err := e.doAIChat(prompt, true)
	if err != nil {
		return AIDecision{}, err
	}
	var decision AIDecision
	if err := e.decodeJSONWithRetry(content, &decision); err != nil {
		return AIDecision{}, err
	}
	return decision, nil
}

// callOpenDetailAI 调用 AI 判断是否需要打开详情。
func (e *TaskExecutor) callOpenDetailAI(jobDesc, candidateText string) (OpenDetailDecision, error) {
	prompt := fmt.Sprintf(defaultOpenDetailPrompt, jobDesc, candidateText)
	if customPrompt := e.effectivePrompt(e.defaultPrompts.OpenDetailPrompt, "open_detail_prompt"); customPrompt != "" {
		prompt = buildPromptFromTemplate(customPrompt, jobDesc, candidateText, prompt, "补充要求")
	}
	content, tokens, err := e.doAIChat(prompt, true)
	if err != nil {
		return OpenDetailDecision{}, err
	}
	var decision OpenDetailDecision
	if err := e.decodeJSONWithRetry(content, &decision); err != nil {
		return OpenDetailDecision{}, err
	}
	decision.Reason = truncateText(strings.TrimSpace(decision.Reason), 20)
	decision.TokenUsage = tokens
	return decision, nil
}

// effectivePrompt 读取岗位模板提示词，为空时使用系统默认提示词。
func (e *TaskExecutor) effectivePrompt(systemDefault string, keys ...string) string {
	if prompt := e.positionAIConfigString(keys...); prompt != "" {
		return prompt
	}
	return strings.TrimSpace(systemDefault)
}

// buildPromptFromTemplate 根据占位符判断提示词是完整模板还是补充规则。
func buildPromptFromTemplate(template, jobDesc, candidateText, fallback, extraTitle string) string {
	text := strings.TrimSpace(template)
	if text == "" {
		return fallback
	}
	if strings.Contains(text, "${岗位信息}") || strings.Contains(text, "${候选人信息}") {
		text = strings.ReplaceAll(text, "${岗位信息}", jobDesc)
		text = strings.ReplaceAll(text, "${候选人信息}", candidateText)
		return text
	}
	return fallback + "\n\n" + extraTitle + "：\n" + text
}

// tryDecodeJSON 尝试从 AI 文本中解析 JSON。
func tryDecodeJSON(raw string, target any) error {
	text := strings.TrimSpace(raw)
	if text == "" {
		return errors.New("empty json text")
	}
	if err := json.Unmarshal([]byte(text), target); err == nil {
		return nil
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return json.Unmarshal([]byte(text[start:end+1]), target)
	}
	return errors.New("json block not found")
}

// rollDetailOpenByProbability 用概率决定关键词模式是否打开详情。
func rollDetailOpenByProbability(probability int) (bool, string, int, error) {
	if probability <= 0 {
		return false, "详情概率为0%，跳过详情", 0, nil
	}
	if probability >= 100 {
		return true, "详情概率为100%，打开详情", 0, nil
	}
	roll := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(100) + 1
	shouldOpen := roll <= probability
	decision := "跳过详情"
	if shouldOpen {
		decision = "打开详情"
	}
	return shouldOpen, fmt.Sprintf("详情概率 %d%%，本次随机值 %d，%s", probability, roll, decision), 0, nil
}

// truncateText 按最大长度截断文本。
func truncateText(text string, maxLen int) string {
	value := strings.TrimSpace(text)
	if maxLen <= 0 || len([]rune(value)) <= maxLen {
		return value
	}
	return string([]rune(value)[:maxLen])
}
