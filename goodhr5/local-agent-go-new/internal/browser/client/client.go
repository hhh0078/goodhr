// Package client 提供 Go 调用 TypeScript Browser Worker 的强类型 HTTP 客户端。
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// Client 是 Browser Worker 强类型客户端。
type Client struct {
	baseURL  string
	http     *http.Client
	recovery func() error
}

// envelope 表示 Worker 统一响应边界。
type envelope struct {
	OK      bool                     `json:"ok"`
	Data    json.RawMessage          `json:"data"`
	Error   contract.WorkerErrorBody `json:"error"`
	TraceID string                   `json:"trace_id"`
}

// New 创建 Browser Worker 客户端。
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{},
	}
}

// SetRecovery 设置操作失联后的进程回收方法，不自动重试可能已经发送的消息。
func (c *Client) SetRecovery(recovery func() error) { c.recovery = recovery }

// requestTimeout 根据逐字输入和长截图的实际工作量安排预算，普通请求仍为 45 秒。
func requestTimeout(payload any) time.Duration {
	timeout := 45 * time.Second
	if input, ok := payload.(contract.ElementInputRequest); ok {
		estimate := 30*time.Second + time.Duration(utf8.RuneCountInString(input.Text))*150*time.Millisecond
		if estimate > timeout {
			timeout = estimate
		}
	}
	if _, ok := payload.(contract.LongScreenshotRequest); ok {
		// 长截图需要回到顶部、逐段截图并用真实滚轮确认到底，不能套用普通操作的 45 秒预算。
		timeout = 180 * time.Second
	}
	if timeout > 180*time.Second {
		timeout = 180 * time.Second
	}
	return timeout
}

// recoverWorker 回收失联进程，确保旧动作不可能在后续任务中继续发送。
func (c *Client) recoverWorker(path string) {
	if c.recovery == nil {
		return
	}
	log.Printf("event=worker_recovery path=%s status=start", path)
	if err := c.recovery(); err != nil {
		log.Printf("event=worker_recovery path=%s status=failed error=%v", path, err)
	} else {
		log.Printf("event=worker_recovery path=%s status=success", path)
	}
}

// Health 检查 Worker HTTP 服务是否可用。
func (c *Client) Health(ctx context.Context) error {
	var result struct {
		Status string `json:"status"`
	}
	if err := call(ctx, c, http.MethodGet, "/health", nil, &result); err != nil {
		return err
	}
	if result.Status != "ok" {
		return fmt.Errorf("Worker 健康状态异常：%s", result.Status)
	}
	return nil
}

// StartBrowser 启动或复用 Camoufox。
func (c *Client) StartBrowser(ctx context.Context, request contract.BrowserStartRequest) (contract.BrowserStatus, error) {
	return callValue[contract.BrowserStatus](ctx, c, "/api/v1/browser/start", request)
}

// StopBrowser 关闭 Camoufox。
func (c *Client) StopBrowser(ctx context.Context) (contract.BrowserStatus, error) {
	return callValue[contract.BrowserStatus](ctx, c, "/api/v1/browser/stop", struct{}{})
}

// BrowserStatus 返回 Camoufox 状态。
func (c *Client) BrowserStatus(ctx context.Context) (contract.BrowserStatus, error) {
	var result contract.BrowserStatus
	err := call(ctx, c, http.MethodGet, "/api/v1/browser/status", nil, &result)
	return result, err
}

// RuntimeStatus 返回 Camoufox 增强浏览器二进制安装状态。
func (c *Client) RuntimeStatus(ctx context.Context) (contract.WorkerRuntimeStatus, error) {
	var result contract.WorkerRuntimeStatus
	err := call(ctx, c, http.MethodGet, "/api/v1/runtime/status", nil, &result)
	return result, err
}

// OpenPage 打开页面。
func (c *Client) OpenPage(ctx context.Context, request contract.PageOpenRequest) (contract.PageInfo, error) {
	return callValue[contract.PageInfo](ctx, c, "/api/v1/page/open", request)
}

// ListPages 返回全部标签页。
func (c *Client) ListPages(ctx context.Context) (contract.PageListResult, error) {
	var result contract.PageListResult
	err := call(ctx, c, http.MethodGet, "/api/v1/page/list", nil, &result)
	return result, err
}

// UsePage 切换标签页。
func (c *Client) UsePage(ctx context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	return callValue[contract.PageInfo](ctx, c, "/api/v1/page/use", request)
}

// ClosePage 关闭当前标签页。
func (c *Client) ClosePage(ctx context.Context) error {
	var result struct {
		Closed bool `json:"closed"`
	}
	return call(ctx, c, http.MethodPost, "/api/v1/page/close", struct{}{}, &result)
}

// SaveCurrentDocument 使用当前浏览器会话的登录状态保存当前文档页。
func (c *Client) SaveCurrentDocument(ctx context.Context, request contract.SaveCurrentDocumentRequest) (contract.DownloadRecord, error) {
	return callValue[contract.DownloadRecord](ctx, c, "/api/v1/page/save-current-document", request)
}

// Find 查找一个页面元素。
func (c *Client) Find(ctx context.Context, request contract.ElementFindRequest) (contract.FindResult, error) {
	return callValue[contract.FindResult](ctx, c, "/api/v1/element/find", request)
}

// FindAll 查找元素列表并读取字段。
func (c *Client) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	return callValue[[]contract.FindAllItem](ctx, c, "/api/v1/element/find-all", request)
}

// Read 查找并读取元素内容。
func (c *Client) Read(ctx context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	return callValue[contract.ReadResult](ctx, c, "/api/v1/element/read", request)
}

// Click 执行完整封装点击。
func (c *Client) Click(ctx context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	return callValue[contract.ClickResult](ctx, c, "/api/v1/element/click", request)
}

// Input 执行完整封装输入。
func (c *Client) Input(ctx context.Context, request contract.ElementInputRequest) (contract.InputResult, error) {
	return callValue[contract.InputResult](ctx, c, "/api/v1/element/input", request)
}

// Scroll 执行真实鼠标滚轮滚动。
func (c *Client) Scroll(ctx context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	path := "/api/v1/page/scroll"
	if request.Target != nil {
		path = "/api/v1/element/scroll"
	}
	return callValue[contract.ScrollResult](ctx, c, path, request)
}

// PressKey 在当前页面执行按键。
func (c *Client) PressKey(ctx context.Context, request contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	return callValue[contract.KeyboardPressResult](ctx, c, "/api/v1/keyboard/press", request)
}

// Screenshot 保存页面或元素截图。
func (c *Client) Screenshot(ctx context.Context, request contract.ScreenshotRequest) (contract.ScreenshotResult, error) {
	path := "/api/v1/page/screenshot"
	if request.Target != nil {
		path = "/api/v1/element/screenshot"
	}
	return callValue[contract.ScreenshotResult](ctx, c, path, request)
}

// LongScreenshot 使用真实鼠标滚轮保存长元素分段截图。
func (c *Client) LongScreenshot(ctx context.Context, request contract.LongScreenshotRequest) (contract.LongScreenshotResult, error) {
	return callValue[contract.LongScreenshotResult](ctx, c, "/api/v1/element/screenshot-long", request)
}

// Cookies 读取浏览器 Cookie。
func (c *Client) Cookies(ctx context.Context) (contract.CookieListResult, error) {
	var result contract.CookieListResult
	err := call(ctx, c, http.MethodGet, "/api/v1/cookies", nil, &result)
	return result, err
}

// SetCookies 导入浏览器 Cookie。
func (c *Client) SetCookies(ctx context.Context, request contract.CookieSetRequest) error {
	var result struct {
		Saved int `json:"saved"`
	}
	return call(ctx, c, http.MethodPost, "/api/v1/cookies", request, &result)
}

// Downloads 返回当前浏览器会话下载记录。
func (c *Client) Downloads(ctx context.Context) (contract.DownloadListResult, error) {
	var result contract.DownloadListResult
	err := call(ctx, c, http.MethodGet, "/api/v1/downloads", nil, &result)
	return result, err
}

// ConfigureDownloads 切换后续浏览器下载的保存目录。
func (c *Client) ConfigureDownloads(ctx context.Context, request contract.DownloadConfigureRequest) error {
	var result struct {
		Configured bool `json:"configured"`
	}
	return call(ctx, c, http.MethodPost, "/api/v1/downloads/configure", request, &result)
}

// ClearDownloads 清空 Worker 内存中的下载记录，不删除本地文件。
func (c *Client) ClearDownloads(ctx context.Context) error {
	var result struct {
		Cleared int `json:"cleared"`
	}
	return call(ctx, c, http.MethodPost, "/api/v1/downloads/clear", struct{}{}, &result)
}

// callValue 执行 POST 并返回指定强类型结果。
func callValue[T any](ctx context.Context, client *Client, path string, request any) (T, error) {
	var result T
	err := call(ctx, client, http.MethodPost, path, request, &result)
	return result, err
}

// call 执行 Worker HTTP 请求并在边界解析统一响应。
func call(ctx context.Context, client *Client, method string, path string, payload any, result any) (callErr error) {
	started := time.Now()
	responseStatus := 0
	responseBytes := 0
	expectedMissing := false
	defer func() {
		entry := map[string]any{
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "category": "browser_call",
			"is_error": callErr != nil, "error_message": "", "action": path,
			"message": "Browser Worker 请求完成", "duration_ms": time.Since(started).Milliseconds(),
			"trace_id": traceIDFromContext(ctx),
			"input":    map[string]any{"method": method, "path": path, "has_payload": payload != nil},
			"output":   map[string]any{"http_status": responseStatus, "response_bytes": responseBytes, "expected_missing": expectedMissing},
		}
		if expectedMissing {
			entry["message"] = "可选位置没有出现，检查完成"
		}
		if callErr != nil {
			entry["error_message"] = callErr.Error()
		}
		if encoded, err := json.Marshal(entry); err == nil {
			log.Print(string(encoded))
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout(payload))
	defer cancel()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("编码 Worker 请求失败：%w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("创建 Worker 请求失败：%w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline)
	budget := remaining - min(2*time.Second, remaining/10)
	if budget < time.Millisecond {
		budget = time.Millisecond
	}
	request.Header.Set("X-Worker-Timeout-Ms", strconv.FormatInt(budget.Milliseconds(), 10))
	if traceID := traceIDFromContext(ctx); traceID != "" {
		request.Header.Set("X-Trace-ID", traceID)
	}
	response, err := client.http.Do(request)
	if err != nil {
		if path != "/health" && path != "/api/v1/downloads" {
			log.Printf("event=worker_request_failed path=%s trace_id=%s duration_ms=%d error=%v", path, traceIDFromContext(ctx), time.Since(started).Milliseconds(), err)
		}
		if method == http.MethodPost && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
			client.recoverWorker(path)
			return &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE", Message: "浏览器连接中断，当前任务已停止，重新开始时会恢复浏览器", Action: path, Step: "http_request", TraceID: traceIDFromContext(ctx)}}
		}
		return fmt.Errorf("连接 Browser Worker 失败：%w", err)
	}
	defer response.Body.Close()
	responseStatus = response.StatusCode
	limited := io.LimitReader(response.Body, 4<<20)
	var wrapped envelope
	if err := json.NewDecoder(limited).Decode(&wrapped); err != nil {
		return fmt.Errorf("解析 Worker 响应失败：%w", err)
	}
	responseBytes = len(wrapped.Data)
	if !wrapped.OK {
		// 明确声明可选的列表检查遇到元素缺失时返回空列表；浏览器故障等其他错误必须继续保留。
		if optional, ok := payload.(contract.ElementFindAllRequest); ok && optional.ExpectedMissing && wrapped.Error.Code == "ELEMENT_NOT_FOUND" {
			expectedMissing = true
			if result != nil {
				if err := json.Unmarshal([]byte("[]"), result); err != nil {
					return fmt.Errorf("解析可选位置检查结果失败：%w", err)
				}
			}
			return nil
		}
		log.Printf("event=worker_action_failed path=%s trace_id=%s duration_ms=%d code=%s step=%s", path, traceIDFromContext(ctx), time.Since(started).Milliseconds(), wrapped.Error.Code, wrapped.Error.Step)
		if wrapped.Error.Code == "WORKER_UNRESPONSIVE" || windowErrorWithoutInput(wrapped.Error) {
			client.recoverWorker(path)
		}
		return &contract.WorkerError{Status: response.StatusCode, Body: wrapped.Error}
	}
	if result == nil || len(wrapped.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(wrapped.Data, result); err != nil {
		return fmt.Errorf("解析 Worker 结果失败：%w", err)
	}
	return nil
}

// windowErrorWithoutInput 确认窗口失活前没有发出原生输入，才回收浏览器供下次岗位使用。
func windowErrorWithoutInput(body contract.WorkerErrorBody) bool {
	if body.Code != "BROWSER_WINDOW_NOT_READY" {
		return false
	}
	var details struct {
		NativeMouse struct {
			EventsSent         *int `json:"events_sent"`
			EventsAcknowledged *int `json:"events_acknowledged"`
		} `json:"native_mouse"`
	}
	if err := json.Unmarshal(body.Details, &details); err != nil {
		return false
	}
	return details.NativeMouse.EventsSent != nil && *details.NativeMouse.EventsSent == 0 &&
		details.NativeMouse.EventsAcknowledged != nil && *details.NativeMouse.EventsAcknowledged == 0
}

type traceKey struct{}

// WithTraceID 把任务 Trace ID 写入上下文。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey{}, strings.TrimSpace(traceID))
}

// traceIDFromContext 从上下文读取 Worker Trace ID。
func traceIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(traceKey{}).(string)
	return value
}
