// 本文件负责云端任务执行编排，按平台配置调用 Local Agent API 完成候选人筛选流程。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	task          TaskRun
	platformCfg   PlatformConfig
	filter        *KeywordFilter
	position      map[string]any
	aiConfig      AIConfig
	userPrefs     UserPreferences
	agentWS       *AgentWSHub
	httpClient    *http.Client
	logCallback   func(level, message string)
	cookies       []map[string]any
	claimedCookie *claimedTaskCookie
}

// NewTaskExecutor 创建任务编排器实例。
func NewTaskExecutor(
	task TaskRun,
	platformCfg PlatformConfig,
	position map[string]any,
	agentWS *AgentWSHub,
	aiConfig AIConfig,
	userPrefs UserPreferences,
	claimedCookie *claimedTaskCookie,
	logCallback func(level, message string),
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
		task:          task,
		platformCfg:   platformCfg,
		filter:        filter,
		position:      position,
		aiConfig:      aiConfig,
		userPrefs:     userPrefs,
		agentWS:       agentWS,
		httpClient:    &http.Client{Timeout: 120 * time.Second},
		logCallback:   logCallback,
		claimedCookie: claimedCookie,
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

	// 3. 滚动加载候选人列表
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.scrollPage(); err != nil {
		return fmt.Errorf("滚动加载失败: %w", err)
	}

	// 4. 提取候选人卡片信息
	if err := ctx.Err(); err != nil {
		return err
	}
	candidates, err := e.extractCandidates()
	if err != nil {
		return fmt.Errorf("提取候选人失败: %w", err)
	}
	if len(candidates) == 0 {
		e.log("warn", "提取到 0 个候选人，可能未登录平台或页面无数据，请检查平台登录状态")
	}
	e.log("info", fmt.Sprintf("提取到 %d 个候选人", len(candidates)))

	// 5. 逐候选人处理
	if err := e.processCandidates(ctx, candidates); err != nil {
		return fmt.Errorf("处理候选人失败: %w", err)
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
	pages := e.platformCfg.Pages
	if len(pages) == 0 {
		return fmt.Errorf("平台配置中没有合法页面")
	}
	url := pages[0].URL
	e.log("info", fmt.Sprintf("正在打开页面: %s", url))

	body := map[string]any{
		"url": url,
	}
	if len(e.cookies) > 0 {
		e.log("info", fmt.Sprintf("打开页面前补充注入 %d 条 cookie", len(e.cookies)))
		body["cookies"] = e.cookies
	}
	if err := e.post("/api/v1/page/open", body, nil); err != nil {
		return err
	}
	return nil
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
	e.log("info", "正在滚动加载候选人列表")
	body := map[string]any{
		"scroll_delay_min": e.userPrefs.ScrollDelayMin,
		"scroll_delay_max": e.userPrefs.ScrollDelayMax,
		"max_scrolls":      e.task.MatchLimit / 5,
	}
	if element := e.platformCfg.Card.ScrollElement(); element != nil {
		body["element"] = element
	}
	if body["max_scrolls"].(int) < 5 {
		body["max_scrolls"] = 5
	}
	return e.post("/api/v1/page/scroll", body, nil)
}

// extractCandidates 从页面提取候选人卡片。
func (e *TaskExecutor) extractCandidates() ([]map[string]any, error) {
	e.log("info", "正在批量提取候选人信息")

	selectors := e.platformCfg.Card.ExtractFieldElements()
	cardElement := e.platformCfg.Card.CardElement()
	if len(selectors) == 0 {
		return nil, fmt.Errorf("平台配置中无候选人字段选择器")
	}
	if cardElement == nil {
		return nil, fmt.Errorf("平台配置中无候选人卡片定位配置")
	}

	var resp struct {
		Ok         bool             `json:"ok"`
		Candidates []map[string]any `json:"candidates"`
		Count      int              `json:"count"`
	}
	body := map[string]any{
		"selectors":    selectors,
		"card_element": cardElement,
		"mode":         "batch",
	}
	if err := e.post("/api/v1/page/extract", body, &resp); err != nil {
		return nil, err
	}
	if resp.Candidates == nil {
		resp.Candidates = []map[string]any{}
	}
	return resp.Candidates, nil
}

// processCandidates 逐候选人筛选和打招呼。
func (e *TaskExecutor) processCandidates(ctx context.Context, candidates []map[string]any) error {
	for i := range candidates {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		e.log("info", fmt.Sprintf("处理候选人 %d/%d", i+1, len(candidates)))

		// 筛选逻辑
		if e.task.Mode == "ai" {
			text := candidateText(candidates[i])
			jobDesc := e.positionDescription()
			decision, err := e.callAI(jobDesc, text)
			if err != nil {
				e.log("error", fmt.Sprintf("AI 筛选失败: %v", err))
				continue
			}
			if !decision.IsOK {
				e.log("info", fmt.Sprintf("候选人 %d AI 筛选跳过: %s", i+1, decision.Msg))
				continue
			}
			e.log("info", fmt.Sprintf("候选人 %d AI 通过: %s", i+1, decision.Msg))
		} else if e.filter != nil {
			text := candidateText(candidates[i])
			result := e.filter.Filter(text)
			if !result.Passed {
				e.log("info", fmt.Sprintf("候选人 %d 被筛选跳过: %s", i+1, result.Reason))
				continue
			}
			e.log("info", fmt.Sprintf("候选人 %d 通过筛选: %s", i+1, result.Reason))
		}

		// 打招呼：点击 greeting 按钮
		if err := e.clickGreet(); err != nil {
			e.log("error", fmt.Sprintf("候选人 %d 打招呼失败: %v", i+1, err))
			continue
		}
		if e.task.EnableSound {
			if err := e.playSuccessSound(); err != nil {
				e.log("warn", fmt.Sprintf("播放成功提示音失败: %v", err))
			}
		}
	}
	return nil
}

// clickGreet 点击打招呼按钮。
func (e *TaskExecutor) clickGreet() error {
	btns := e.platformCfg.Actions.GreetBtn
	if len(btns) == 0 {
		e.log("warn", "无打招呼按钮选择器")
		return nil
	}
	body := map[string]any{
		"timeout":      10000,
		"delay_before": greetDelayBefore(e.userPrefs),
	}
	if element := actionElementPayload(btns); element != nil {
		body["element"] = element
	} else {
		body["selector"] = btns[0]
	}
	return e.post("/api/v1/page/click", body, nil)
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

// candidateText 将候选人字段拼接为可供筛选的文本。
func candidateText(candidate map[string]any) string {
	var parts []string
	for _, v := range candidate {
		if s, ok := v.(string); ok && s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
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

// ---------- AI 筛选 ----------

type AIRequest struct {
	Model       string  `json:"model"`
	Messages    []AIMsg `json:"messages"`
	Temperature float64 `json:"temperature"`
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
}
type AIDecision struct {
	IsOK bool   `json:"isok"`
	Msg  string `json:"msg"`
}

const defaultAIPrompt = `你是一个资深的HR专家。请根据候选人的基本信息判断是否值得查看其详细信息。

重要提示：
1. 这个API仅用于岗位与候选人的筛选。
2. 请根据岗位要求判断是否值得查看这位候选人的详细信息。
3. 必须返回JSON格式，包含isok和msg两个字段。
4. isok字段只能是true或false。
5. msg字段是决策原因，10个字以内。

岗位要求：
%s

候选人基本信息：
%s

请判断是否值得查看这位候选人的详细信息，返回JSON格式：{"isok": true, "msg": "符合基本要求"}`

// positionDescription 从岗位信息中提取职位要求文本。
func (e *TaskExecutor) positionDescription() string {
	if e.position == nil {
		return ""
	}
	if desc, ok := e.position["name"].(string); ok && desc != "" {
		return desc
	}
	return ""
}

// callAI 调用 AI API 对候选人进行筛选。
func (e *TaskExecutor) callAI(jobDesc, candidateText string) (AIDecision, error) {
	model := "gpt-5.1-chat"
	baseURL := "https://ai.58it.cn/v1/chat/completions"
	temperature := 0.3

	if e.userPrefs.AIModel != "" {
		model = e.userPrefs.AIModel
	} else if e.aiConfig.Model != "" {
		model = e.aiConfig.Model
	}
	if e.aiConfig.BaseURL != "" {
		baseURL = e.aiConfig.BaseURL
	}
	if e.aiConfig.Temperature > 0 {
		temperature = e.aiConfig.Temperature
	}

	prompt := fmt.Sprintf(defaultAIPrompt, jobDesc, candidateText)
	reqBody := AIRequest{Model: model, Messages: []AIMsg{{Role: "user", Content: prompt}}, Temperature: temperature}
	data, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if e.aiConfig.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.aiConfig.APIKey)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return AIDecision{}, fmt.Errorf("AI API 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return AIDecision{}, fmt.Errorf("AI API 错误 %d", resp.StatusCode)
	}
	var aiResp AIResponse
	json.Unmarshal(body, &aiResp)
	if len(aiResp.Choices) == 0 {
		return AIDecision{}, fmt.Errorf("AI 未返回结果")
	}
	content := aiResp.Choices[0].Message.Content
	var decision AIDecision
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		start := strings.Index(content, "{")
		end := strings.LastIndex(content, "}")
		if start >= 0 && end > start {
			json.Unmarshal([]byte(content[start:end+1]), &decision)
		}
	}
	return decision, nil
}
