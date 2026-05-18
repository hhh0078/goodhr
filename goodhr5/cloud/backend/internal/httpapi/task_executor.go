// 本文件负责云端任务执行编排，按平台配置调用 Local Agent API 完成候选人筛选流程。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TaskExecutor 负责任务的云端编排执行。
type TaskExecutor struct {
	task         TaskRun
	platformCfg  PlatformConfig
	position     map[string]any
	agentBaseURL string
	httpClient   *http.Client
	logCallback  func(level, message string)
}

// NewTaskExecutor 创建任务编排器实例。
func NewTaskExecutor(
	task TaskRun,
	platformCfg PlatformConfig,
	position map[string]any,
	agentBaseURL string,
	logCallback func(level, message string),
) *TaskExecutor {
	return &TaskExecutor{
		task:         task,
		platformCfg:  platformCfg,
		position:     position,
		agentBaseURL: agentBaseURL,
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		logCallback:  logCallback,
	}
}

// Run 执行任务编排主流程。
func (e *TaskExecutor) Run(ctx context.Context) error {
	e.log("info", "任务执行开始")

	// 1. 启动浏览器
	if err := e.startBrowser(); err != nil {
		return fmt.Errorf("启动浏览器失败: %w", err)
	}
	defer e.stopBrowser()

	// 2. 打开平台推荐页
	if err := e.openPage(); err != nil {
		return fmt.Errorf("打开页面失败: %w", err)
	}

	// 3. 滚动加载候选人列表
	if err := e.scrollPage(); err != nil {
		return fmt.Errorf("滚动加载失败: %w", err)
	}

	// 4. 提取候选人卡片信息
	candidates, err := e.extractCandidates()
	if err != nil {
		return fmt.Errorf("提取候选人失败: %w", err)
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
	if err := e.post("/api/v1/page/open", body, nil); err != nil {
		return err
	}
	return nil
}

// scrollPage 滚动加载候选人列表。
func (e *TaskExecutor) scrollPage() error {
	e.log("info", "正在滚动加载候选人列表")
	body := map[string]any{
		"scroll_delay_min": 3,
		"scroll_delay_max": 8,
		"max_scrolls":      e.task.MatchLimit / 5,
	}
	if body["max_scrolls"].(int) < 5 {
		body["max_scrolls"] = 5
	}
	return e.post("/api/v1/page/scroll", body, nil)
}

// extractCandidates 从页面提取候选人卡片。
func (e *TaskExecutor) extractCandidates() ([]map[string]any, error) {
	e.log("info", "正在批量提取候选人信息")

	selectors := e.platformCfg.Card.ExtractFieldSelectors()
	cards := e.platformCfg.Card.Cards
	if len(cards) == 0 {
		return nil, fmt.Errorf("平台配置中无卡片选择器")
	}

	var resp struct {
		Ok         bool             `json:"ok"`
		Candidates []map[string]any `json:"candidates"`
		Count      int              `json:"count"`
	}
	body := map[string]any{
		"selectors":     selectors,
		"card_selector": cards[0],
		"mode":          "batch",
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

		// 筛选逻辑：后续根据 mode 调用关键词或 AI 筛选
		if e.task.Mode == "ai" {
			e.log("info", "AI 模式筛选（待实现）")
		} else {
			e.log("info", "关键词模式筛选（待实现）")
		}

		// 打招呼：点击 greeting 按钮
		_ = e.clickGreet()
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
	return e.post("/api/v1/page/click", map[string]any{
		"selector":     btns[0],
		"timeout":      10000,
		"delay_before": 1.0,
	}, nil)
}

// ---------- Local Agent HTTP 客户端 ----------

// post 向 Local Agent 发送 POST 请求。
func (e *TaskExecutor) post(path string, body any, result any) error {
	url := e.agentBaseURL + path

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求体失败: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(http.MethodPost, url, reqBody)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求 Local Agent 失败 (%s): %w", path, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Local Agent 错误 %d: %s", resp.StatusCode, string(respBytes))
	}

	if result != nil {
		if err := json.Unmarshal(respBytes, result); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
	}

	return nil
}

// log 记录任务执行日志。
func (e *TaskExecutor) log(level, message string) {
	if e.logCallback != nil {
		e.logCallback(level, message)
	}
}
