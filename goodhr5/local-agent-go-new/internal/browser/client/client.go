// Package client 提供 Go 调用 TypeScript Browser Worker 的强类型 HTTP 客户端。
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// Client 是 Browser Worker 强类型客户端。
type Client struct {
	baseURL string
	http    *http.Client
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
		http:    &http.Client{Timeout: 45 * time.Second},
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

// StartBrowser 启动或复用 CloakBrowser。
func (c *Client) StartBrowser(ctx context.Context, request contract.BrowserStartRequest) (contract.BrowserStatus, error) {
	return callValue[contract.BrowserStatus](ctx, c, "/api/v1/browser/start", request)
}

// StopBrowser 关闭 CloakBrowser。
func (c *Client) StopBrowser(ctx context.Context) (contract.BrowserStatus, error) {
	return callValue[contract.BrowserStatus](ctx, c, "/api/v1/browser/stop", struct{}{})
}

// BrowserStatus 返回 CloakBrowser 状态。
func (c *Client) BrowserStatus(ctx context.Context) (contract.BrowserStatus, error) {
	var result contract.BrowserStatus
	err := call(ctx, c, http.MethodGet, "/api/v1/browser/status", nil, &result)
	return result, err
}

// RuntimeStatus 返回 CloakBrowser 增强浏览器二进制安装状态。
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

// ShowOverlay 显示通用页面提示浮层。
func (c *Client) ShowOverlay(ctx context.Context, request contract.OverlayShowRequest) (contract.OverlayResult, error) {
	return callValue[contract.OverlayResult](ctx, c, "/api/v1/overlay/show", request)
}

// CloseOverlay 关闭通用页面提示浮层。
func (c *Client) CloseOverlay(ctx context.Context, request contract.OverlayCloseRequest) (contract.OverlayResult, error) {
	return callValue[contract.OverlayResult](ctx, c, "/api/v1/overlay/close", request)
}

// callValue 执行 POST 并返回指定强类型结果。
func callValue[T any](ctx context.Context, client *Client, path string, request any) (T, error) {
	var result T
	err := call(ctx, client, http.MethodPost, path, request, &result)
	return result, err
}

// call 执行 Worker HTTP 请求并在边界解析统一响应。
func call(ctx context.Context, client *Client, method string, path string, payload any, result any) error {
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
	if traceID := traceIDFromContext(ctx); traceID != "" {
		request.Header.Set("X-Trace-ID", traceID)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("连接 Browser Worker 失败：%w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 4<<20)
	var wrapped envelope
	if err := json.NewDecoder(limited).Decode(&wrapped); err != nil {
		return fmt.Errorf("解析 Worker 响应失败：%w", err)
	}
	if !wrapped.OK {
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
