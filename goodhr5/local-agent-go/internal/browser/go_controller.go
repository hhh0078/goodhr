// Package browser 提供 Go 直连 CloakBrowser 的浏览器控制器。
package browser

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// WorkerModeNode 表示继续使用现有 Node Browser Worker。
	WorkerModeNode = "node"
	// WorkerModeGo 表示使用 Go 浏览器控制器，当前属于实验模式。
	WorkerModeGo = "go"
)

var (
	// ErrGoBrowserUnsupported 表示 Go 控制器还没有完全支持对应组合能力。
	ErrGoBrowserUnsupported = errors.New("Go 浏览器模式暂未支持这个组合操作")
)

// OperationKind 表示浏览器操作所属层级。
type OperationKind string

const (
	// OperationBasic 表示允许放在本文件的浏览器基础操作。
	OperationBasic OperationKind = "basic"
	// OperationComposite 表示通用组合动作，不建议放在本文件。
	OperationComposite OperationKind = "composite"
	// OperationPlatform 表示平台个性化动作，不建议放在本文件。
	OperationPlatform OperationKind = "platform"
)

// OperationSpec 描述一个浏览器相关方法应该放在哪一层。
type OperationSpec struct {
	Name        string        `json:"name"`
	Kind        OperationKind `json:"kind"`
	Description string        `json:"description"`
	Place       string        `json:"place"`
	Note        string        `json:"note,omitempty"`
}

// BasicOperationCatalog 是本文件允许承载的方法清单。
var BasicOperationCatalog = []OperationSpec{
	{Name: "StartBrowser", Kind: OperationBasic, Description: "启动 CloakBrowser", Place: "internal/browser/go_controller.go"},
	{Name: "StopBrowser", Kind: OperationBasic, Description: "关闭 CloakBrowser", Place: "internal/browser/go_controller.go"},
	{Name: "BrowserHealth", Kind: OperationBasic, Description: "检查浏览器是否运行", Place: "internal/browser/go_controller.go"},
	{Name: "ListPages", Kind: OperationBasic, Description: "列出当前浏览器页面", Place: "internal/browser/go_controller.go"},
	{Name: "UsePage", Kind: OperationBasic, Description: "切换当前操作页面", Place: "internal/browser/go_controller.go"},
	{Name: "CurrentURL", Kind: OperationBasic, Description: "读取当前页面地址", Place: "internal/browser/go_controller.go"},
	{Name: "OpenPage", Kind: OperationBasic, Description: "打开指定页面地址", Place: "internal/browser/go_controller.go"},
	{Name: "ReloadPage", Kind: OperationBasic, Description: "刷新当前页面", Place: "internal/browser/go_controller.go"},
	{Name: "WaitPageLoad", Kind: OperationBasic, Description: "等待页面加载完成", Place: "internal/browser/go_controller.go"},
	{Name: "FindOne", Kind: OperationBasic, Description: "按选择器查找一个元素", Place: "internal/browser/go_controller.go"},
	{Name: "FindAll", Kind: OperationBasic, Description: "按选择器查找多个元素", Place: "internal/browser/go_controller.go"},
	{Name: "RememberElement", Kind: OperationBasic, Description: "保存元素引用", Place: "internal/browser/go_controller.go"},
	{Name: "GetElementByRef", Kind: OperationBasic, Description: "按引用读取元素", Place: "internal/browser/go_controller.go"},
	{Name: "ClearElementRefs", Kind: OperationBasic, Description: "清空元素引用", Place: "internal/browser/go_controller.go"},
	{Name: "ClickElement", Kind: OperationBasic, Description: "点击元素", Place: "internal/browser/go_controller.go"},
	{Name: "FillElement", Kind: OperationBasic, Description: "输入文本", Place: "internal/browser/go_controller.go"},
	{Name: "PressKey", Kind: OperationBasic, Description: "按键盘按键", Place: "internal/browser/go_controller.go"},
	{Name: "ScrollPage", Kind: OperationBasic, Description: "滚动页面", Place: "internal/browser/go_controller.go"},
	{Name: "ScrollElement", Kind: OperationBasic, Description: "滚动元素", Place: "internal/browser/go_controller.go"},
	{Name: "ElementText", Kind: OperationBasic, Description: "读取元素文本", Place: "internal/browser/go_controller.go"},
	{Name: "ElementAttribute", Kind: OperationBasic, Description: "读取元素属性", Place: "internal/browser/go_controller.go"},
	{Name: "ElementHTML", Kind: OperationBasic, Description: "读取元素 HTML", Place: "internal/browser/go_controller.go"},
	{Name: "ScreenshotPage", Kind: OperationBasic, Description: "页面截图", Place: "internal/browser/go_controller.go"},
	{Name: "ScreenshotElement", Kind: OperationBasic, Description: "元素截图", Place: "internal/browser/go_controller.go"},
	{Name: "GetCookies", Kind: OperationBasic, Description: "导出 Cookie", Place: "internal/browser/go_controller.go"},
	{Name: "SetCookies", Kind: OperationBasic, Description: "导入 Cookie", Place: "internal/browser/go_controller.go"},
	{Name: "SetDownloadDir", Kind: OperationBasic, Description: "设置下载目录", Place: "internal/browser/go_controller.go"},
	{Name: "ListDownloads", Kind: OperationBasic, Description: "读取下载记录", Place: "internal/browser/go_controller.go"},
}

// CompositeOperationCatalog 是不建议放在本文件的通用组合动作清单。
var CompositeOperationCatalog = []OperationSpec{
	{Name: "ClickFirstVisible", Kind: OperationComposite, Description: "点击一组选项中第一个可见元素", Place: "internal/browser/actions.go", Note: "不建议放这里：它由 FindAll、ElementText、ClickElement 组合而来。"},
	{Name: "WaitAnyVisible", Kind: OperationComposite, Description: "等待任意一个选择器可见", Place: "internal/browser/actions.go", Note: "不建议放这里：它是等待策略，不是浏览器原子能力。"},
	{Name: "ExtractFields", Kind: OperationComposite, Description: "在某个元素范围内按字段配置提取文本", Place: "internal/browser/actions.go", Note: "不建议放这里：它由 FindOne、FindAll、ElementText 组合而来。"},
	{Name: "ExtractListFields", Kind: OperationComposite, Description: "批量提取列表元素字段", Place: "internal/browser/actions.go", Note: "不建议放这里：它是列表抽取流程。"},
	{Name: "ClickListItemByIndex", Kind: OperationComposite, Description: "点击列表第 N 个元素", Place: "internal/browser/actions.go", Note: "不建议放这里：它由 FindAll、ScrollElement、ClickElement 组合而来。"},
	{Name: "ScrollUntilStable", Kind: OperationComposite, Description: "持续滚动直到页面内容稳定", Place: "internal/browser/actions.go", Note: "不建议放这里：它是重试和判断策略。"},
	{Name: "ScrollUntilText", Kind: OperationComposite, Description: "持续滚动直到出现指定文本", Place: "internal/browser/actions.go", Note: "不建议放这里：它由 ScrollPage、ElementText 组合而来。"},
	{Name: "ScreenshotWithFallback", Kind: OperationComposite, Description: "优先元素截图，失败后页面截图", Place: "internal/browser/actions.go", Note: "不建议放这里：它是截图兜底策略。"},
	{Name: "CloseByKeys", Kind: OperationComposite, Description: "按 Esc 或其它键关闭弹层", Place: "internal/browser/actions.go", Note: "不建议放这里：它是通用组合动作。"},
}

// PlatformOperationCatalog 是不建议放在本文件的平台个性化动作清单。
var PlatformOperationCatalog = []OperationSpec{
	{Name: "OpenEntryPage", Kind: OperationPlatform, Description: "打开平台入口页面", Place: "internal/platforms/{platform}/runtime.go", Note: "不建议放这里：入口地址和登录态判断属于平台。"},
	{Name: "PrepareEntryPage", Kind: OperationPlatform, Description: "处理平台弹窗、身份切换、页面准备", Place: "internal/platforms/{platform}/runtime.go", Note: "不建议放这里：这些是平台规则。"},
	{Name: "IsTaskEntryPage", Kind: OperationPlatform, Description: "判断是否仍在任务入口页", Place: "internal/platforms/{platform}/runtime.go", Note: "不建议放这里：判断规则因平台不同。"},
	{Name: "CurrentPositionName", Kind: OperationPlatform, Description: "读取当前岗位名称", Place: "internal/platforms/{platform}/runtime.go", Note: "不建议放这里：岗位 DOM 和文案属于平台。"},
	{Name: "ExtractCandidates", Kind: OperationPlatform, Description: "提取候选人列表", Place: "internal/platforms/{platform}/runtime.go", Note: "不建议放这里：候选人字段规则属于平台。"},
	{Name: "ScrollCandidateList", Kind: OperationPlatform, Description: "滚动候选人列表", Place: "internal/platforms/{platform}/runtime.go", Note: "不建议放这里：列表容器和加载方式属于平台。"},
	{Name: "OpenCandidateDetail", Kind: OperationPlatform, Description: "打开候选人详情", Place: "internal/platforms/{platform}/runtime.go", Note: "不建议放这里：打开方式属于平台。"},
	{Name: "ExtractCandidateDetail", Kind: OperationPlatform, Description: "提取候选人详情", Place: "internal/platforms/{platform}/runtime.go", Note: "不建议放这里：简历字段和截图区域属于平台。"},
	{Name: "GreetCandidate", Kind: OperationPlatform, Description: "给候选人打招呼", Place: "internal/platforms/{platform}/runtime.go", Note: "不建议放这里：按钮、弹窗、发送规则属于平台。"},
	{Name: "CloseCandidateDetail", Kind: OperationPlatform, Description: "关闭候选人详情", Place: "internal/platforms/{platform}/runtime.go", Note: "不建议放这里：关闭方式属于平台。"},
}

// GoController 是 Go 浏览器控制器，按 Node Worker 的路由形态提供兼容调用。
type GoController struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	port          int
	baseURL       string
	page          *goPage
	refs          map[string]ElementRef
	refSeq        int
	downloads     []DownloadRecord
	downloadsPath string
	userDataDir   string
}

// BrowserStartOptions 表示浏览器启动参数。
type BrowserStartOptions struct {
	ExecutablePath string `json:"executable_path,omitempty"`
	UserDataDir    string `json:"user_data_dir,omitempty"`
	DownloadsPath  string `json:"downloads_path,omitempty"`
	InitialURL     string `json:"url,omitempty"`
	Headless       bool   `json:"headless,omitempty"`
	Persistent     bool   `json:"persistent,omitempty"`
	ViewportWidth  int    `json:"viewport_width,omitempty"`
	ViewportHeight int    `json:"viewport_height,omitempty"`
}

// BrowserStatus 表示浏览器运行状态。
type BrowserStatus struct {
	Running       bool   `json:"running"`
	Worker        string `json:"worker"`
	Experimental  bool   `json:"experimental"`
	UserDataDir   string `json:"user_data_dir,omitempty"`
	DownloadsPath string `json:"downloads_path,omitempty"`
	CurrentURL    string `json:"current_url,omitempty"`
	Message       string `json:"message,omitempty"`
}

// PageInfo 表示一个浏览器页面。
type PageInfo struct {
	Index int    `json:"index"`
	ID    string `json:"id,omitempty"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// ElementSelector 表示元素选择器配置。
type ElementSelector struct {
	Selector string `json:"selector,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Visible  bool   `json:"visible,omitempty"`
	Index    int    `json:"index,omitempty"`
}

// ElementRef 表示一个已缓存的元素引用。
type ElementRef struct {
	ID       string    `json:"id"`
	Created  time.Time `json:"created"`
	Selector string    `json:"selector,omitempty"`
	Index    int       `json:"index,omitempty"`
}

// ElementInfo 表示页面元素的简要信息。
type ElementInfo struct {
	Index      int            `json:"index"`
	Ref        string         `json:"ref,omitempty"`
	ElementRef string         `json:"element_ref,omitempty"`
	Text       string         `json:"text,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

// ScreenshotOptions 表示截图参数。
type ScreenshotOptions struct {
	Selector string `json:"selector,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Dir      string `json:"dir,omitempty"`
	Filename string `json:"filename,omitempty"`
	FullPage bool   `json:"full_page,omitempty"`
}

// ScreenshotResult 表示截图结果。
type ScreenshotResult struct {
	Path string `json:"path"`
	File string `json:"file"`
}

// Cookie 表示浏览器 Cookie。
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain,omitempty"`
	Path     string  `json:"path,omitempty"`
	Expires  float64 `json:"expires,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
}

// DownloadRecord 表示浏览器下载记录。
type DownloadRecord struct {
	Path      string    `json:"path"`
	Filename  string    `json:"filename"`
	CreatedAt time.Time `json:"created_at"`
}

type goPage struct {
	ID                   string `json:"id"`
	URL                  string `json:"url"`
	Title                string `json:"title"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	client               *cdpClient
}

// NewGoController 创建 Go 浏览器控制器。
func NewGoController() *GoController {
	return &GoController{refs: make(map[string]ElementRef)}
}

// Start 实现 BrowserWorker 的启动接口。
func (c *GoController) Start(ctx context.Context) (WorkerStatus, error) {
	status, err := c.StartBrowser(ctx, BrowserStartOptions{})
	return WorkerStatus{Running: status.Running, BaseURL: "go://cloakbrowser", Managed: true}, err
}

// Call 按 Node Worker 路由调用 Go 控制器。
func (c *GoController) Call(ctx context.Context, path string, payload any) (map[string]any, error) {
	data := mapFromAny(payload)
	switch path {
	case "/api/v1/browser/start":
		return c.workerData(c.StartBrowser(ctx, browserStartOptionsFromPayload(data)))
	case "/api/v1/browser/stop":
		return c.workerData(c.StopBrowser(ctx))
	case "/api/v1/page/list":
		pages, err := c.ListPages(ctx)
		return map[string]any{"pages": pages, "count": len(pages), "worker": WorkerModeGo}, err
	case "/api/v1/page/use":
		page, err := c.UsePage(ctx, goIntFromAny(data["index"]))
		return map[string]any{"url": page.URL, "index": page.Index, "worker": WorkerModeGo}, err
	case "/api/v1/page/open":
		page, err := c.OpenPage(ctx, stringFromAny(data["url"]))
		return map[string]any{"url": page.URL, "worker": WorkerModeGo}, err
	case "/api/v1/page/click":
		err := c.ClickElement(ctx, selectorFromPayload(data))
		return map[string]any{"clicked": err == nil, "worker": WorkerModeGo}, err
	case "/api/v1/page/type":
		err := c.FillElement(ctx, selectorFromPayload(data), stringFromAny(data["text"]))
		return map[string]any{"typed": err == nil, "worker": WorkerModeGo}, err
	case "/api/v1/page/press-key":
		err := c.PressKey(ctx, stringFromAny(data["key"]))
		return map[string]any{"pressed": err == nil, "key": stringFromAny(data["key"]), "worker": WorkerModeGo}, err
	case "/api/v1/page/scroll":
		err := c.scrollFromPayload(ctx, data)
		return map[string]any{"scrolled": err == nil, "distance": goIntFromAny(data["distance"]), "worker": WorkerModeGo}, err
	case "/api/v1/page/extract-text":
		text, err := c.ElementText(ctx, selectorFromPayload(data))
		return map[string]any{"text": text, "count": 1, "worker": WorkerModeGo}, err
	case "/api/v1/page/find-elements":
		items, err := c.FindAll(ctx, selectorFromPayload(data), data["fields"], goIntFromAny(data["max_items"]))
		return map[string]any{"items": items, "count": len(items), "worker": WorkerModeGo}, err
	case "/api/v1/page/list-click-by-index":
		err := c.ClickListItemByIndex(ctx, data)
		return map[string]any{"clicked": err == nil, "index": goIntFromAny(data["index"]), "worker": WorkerModeGo}, err
	case "/api/v1/page/screenshot":
		result, err := c.screenshotFromPayload(ctx, data)
		return map[string]any{"path": result.Path, "file": result.File, "screenshot": result, "worker": WorkerModeGo}, err
	case "/api/v1/page/cookies":
		err := c.SetCookies(ctx, cookiesFromAny(data["cookies"]))
		return map[string]any{"count": len(cookiesFromAny(data["cookies"])), "worker": WorkerModeGo}, err
	case "/api/v1/page/ai-overlay", "/api/v1/page/keyword-overlay":
		return c.MarkOverlay(ctx, data)
	case "/api/v1/boss/candidates/extract":
		return c.ExtractPlatformCandidates(ctx, data)
	case "/api/v1/boss/candidates/scroll":
		err := c.ScrollPlatformCandidateList(ctx, data)
		return map[string]any{"scrolled": err == nil, "worker": WorkerModeGo}, err
	case "/api/v1/boss/candidates/greet":
		err := c.GreetPlatformCandidate(ctx, data)
		return map[string]any{"greeted": err == nil, "worker": WorkerModeGo}, err
	case "/api/v1/boss/candidates/detail":
		return c.ExtractPlatformCandidateDetail(ctx, data)
	case "/api/v1/boss/candidates/detail/close":
		err := c.ClosePlatformCandidateDetail(ctx, data)
		return map[string]any{"closed": err == nil, "worker": WorkerModeGo}, err
	default:
		return nil, fmt.Errorf("Go 浏览器模式暂未支持此路由：%s", path)
	}
}

// CallGet 按 Node Worker GET 路由调用 Go 控制器。
func (c *GoController) CallGet(ctx context.Context, path string) (map[string]any, error) {
	switch path {
	case "/health":
		status, err := c.BrowserHealth(ctx)
		return map[string]any{
			"worker":          WorkerModeGo,
			"browser_running": status.Running,
			"go_experimental": true,
		}, err
	case "/api/v1/page/cookies":
		cookies, err := c.GetCookies(ctx)
		return map[string]any{"cookies": cookies, "count": len(cookies), "worker": WorkerModeGo}, err
	default:
		return nil, fmt.Errorf("Go 浏览器模式暂未支持此 GET 路由：%s", path)
	}
}

// StartBrowser 启动浏览器。
func (c *GoController) StartBrowser(ctx context.Context, options BrowserStartOptions) (BrowserStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isRunningLocked() {
		if options.UserDataDir == "" || options.UserDataDir == c.userDataDir {
			return c.statusLocked(), nil
		}
		_, _ = c.stopLocked()
	}

	executable := resolveBrowserExecutable(options.ExecutablePath)
	if executable == "" {
		return BrowserStatus{}, fmt.Errorf("Go 浏览器模式找不到 CloakBrowser，请传 executable_path 或设置 GOODHR_CLOAKBROWSER_PATH")
	}
	port, err := freeTCPPort()
	if err != nil {
		return BrowserStatus{}, err
	}
	if options.UserDataDir != "" {
		_ = os.MkdirAll(options.UserDataDir, 0o755)
	}
	if options.DownloadsPath != "" {
		_ = os.MkdirAll(options.DownloadsPath, 0o755)
	}

	args := []string{
		"--remote-debugging-address=127.0.0.1",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
	}
	if options.UserDataDir != "" {
		args = append(args, "--user-data-dir="+options.UserDataDir)
	}
	if options.Headless {
		args = append(args, "--headless=new")
	}
	if options.ViewportWidth > 0 && options.ViewportHeight > 0 {
		args = append(args, fmt.Sprintf("--window-size=%d,%d", options.ViewportWidth, options.ViewportHeight))
	}
	initialURL := strings.TrimSpace(options.InitialURL)
	if initialURL == "" {
		initialURL = "about:blank"
	}
	args = append(args, initialURL)

	cmd := exec.CommandContext(context.Background(), executable, args...)
	if err := cmd.Start(); err != nil {
		return BrowserStatus{}, fmt.Errorf("Go 浏览器模式启动 CloakBrowser 失败：%w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitDevTools(ctx, baseURL, 10*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return BrowserStatus{}, err
	}
	page, err := createOrFirstPage(ctx, baseURL)
	if err != nil {
		_ = cmd.Process.Kill()
		return BrowserStatus{}, err
	}
	client, err := dialCDP(ctx, page.WebSocketDebuggerURL)
	if err != nil {
		_ = cmd.Process.Kill()
		return BrowserStatus{}, err
	}
	page.client = client
	_, _ = client.Call(ctx, "Page.enable", nil)
	_, _ = client.Call(ctx, "Runtime.enable", nil)
	_, _ = client.Call(ctx, "DOM.enable", nil)

	c.cmd = cmd
	c.port = port
	c.baseURL = baseURL
	c.page = page
	c.refs = make(map[string]ElementRef)
	c.userDataDir = options.UserDataDir
	c.downloadsPath = options.DownloadsPath
	if options.DownloadsPath != "" {
		_ = c.setDownloadDirLocked(ctx, options.DownloadsPath)
	}
	return c.statusLocked(), nil
}

// StopBrowser 关闭浏览器。
func (c *GoController) StopBrowser(ctx context.Context) (BrowserStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return BrowserStatus{}, ctx.Err()
	default:
	}
	return c.stopLocked()
}

// BrowserHealth 返回浏览器运行状态。
func (c *GoController) BrowserHealth(ctx context.Context) (BrowserStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return BrowserStatus{}, ctx.Err()
	default:
	}
	if c.cmd != nil && c.cmd.ProcessState != nil && c.cmd.ProcessState.Exited() {
		_, _ = c.stopLocked()
	}
	return c.statusLocked(), nil
}

// ListPages 列出当前页面。
func (c *GoController) ListPages(ctx context.Context) ([]PageInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	targets, err := listPages(ctx, c.baseURL)
	if err != nil {
		return nil, err
	}
	pages := make([]PageInfo, 0, len(targets))
	for index, target := range targets {
		pages = append(pages, PageInfo{Index: index, ID: target.ID, URL: target.URL, Title: target.Title})
	}
	return pages, nil
}

// UsePage 切换当前页面。
func (c *GoController) UsePage(ctx context.Context, index int) (PageInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	targets, err := listPages(ctx, c.baseURL)
	if err != nil {
		return PageInfo{}, err
	}
	if len(targets) == 0 {
		return PageInfo{}, fmt.Errorf("Go 浏览器模式没有可用页面")
	}
	if index < 0 || index >= len(targets) {
		index = 0
	}
	target := targets[index]
	if c.page != nil && c.page.client != nil {
		_ = c.page.client.Close()
	}
	client, err := dialCDP(ctx, target.WebSocketDebuggerURL)
	if err != nil {
		return PageInfo{}, err
	}
	target.client = client
	c.page = target
	c.refs = make(map[string]ElementRef)
	return PageInfo{Index: index, ID: target.ID, URL: target.URL, Title: target.Title}, nil
}

// CurrentURL 读取当前页面地址。
func (c *GoController) CurrentURL(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentURLLocked(ctx)
}

// currentURLLocked 在已持有锁时读取当前页面地址。
// ctx 为调用上下文，返回当前页面地址。
func (c *GoController) currentURLLocked(ctx context.Context) (string, error) {
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return "", err
	}
	value, err := c.evalLocked(ctx, "location.href")
	if err != nil {
		return page.URL, nil
	}
	if text, ok := value.(string); ok {
		page.URL = text
	}
	return page.URL, nil
}

// OpenPage 打开指定页面地址。
func (c *GoController) OpenPage(ctx context.Context, rawURL string) (PageInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return PageInfo{}, err
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return PageInfo{}, fmt.Errorf("页面地址不能为空")
	}
	currentURL, _ := c.currentURLLocked(ctx)
	if samePageURL(currentURL, rawURL) {
		page.URL = currentURL
		return PageInfo{Index: 0, ID: page.ID, URL: currentURL, Title: page.Title}, nil
	}
	if _, err := page.client.Call(ctx, "Page.navigate", map[string]any{"url": rawURL}); err != nil {
		return PageInfo{}, err
	}
	_ = c.waitReadyLocked(ctx, 30*time.Second)
	page.URL = rawURL
	c.refs = make(map[string]ElementRef)
	return PageInfo{Index: 0, ID: page.ID, URL: rawURL, Title: page.Title}, nil
}

// ReloadPage 刷新当前页面。
func (c *GoController) ReloadPage(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	if _, err := page.client.Call(ctx, "Page.reload", map[string]any{"ignoreCache": false}); err != nil {
		return err
	}
	return c.waitReadyLocked(ctx, 30*time.Second)
}

// WaitPageLoad 等待页面加载完成。
func (c *GoController) WaitPageLoad(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waitReadyLocked(ctx, 30*time.Second)
}

// FindOne 查找一个元素。
func (c *GoController) FindOne(ctx context.Context, selector ElementSelector) (ElementRef, error) {
	items, err := c.FindAll(ctx, selector, nil, 1)
	if err != nil {
		return ElementRef{}, err
	}
	if len(items) == 0 {
		return ElementRef{}, fmt.Errorf("Go 浏览器模式未找到元素：%s", selector.Selector)
	}
	return c.GetElementByRef(items[0].Ref)
}

// FindAll 查找多个元素。
func (c *GoController) FindAll(ctx context.Context, selector ElementSelector, fields any, maxItems int) ([]ElementInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.ensurePageLocked(ctx); err != nil {
		return nil, err
	}
	css := strings.TrimSpace(selector.Selector)
	if css == "" && selector.Ref != "" {
		if ref, ok := c.refs[selector.Ref]; ok {
			css = ref.Selector
		}
	}
	if css == "" {
		return nil, fmt.Errorf("Go 浏览器模式查找元素时选择器不能为空")
	}
	expr := fmt.Sprintf(`(() => {
const selector = %s;
const maxItems = %d;
const fields = %s;
function visible(el) {
  const r = el.getBoundingClientRect();
  const s = getComputedStyle(el);
  return r.width > 0 && r.height > 0 && s.display !== "none" && s.visibility !== "hidden";
}
function firstSelector(v) {
  if (!v) return "";
  if (typeof v === "string") return v;
  if (Array.isArray(v)) {
    for (const item of v) { const s = firstSelector(item); if (s) return s; }
  }
  if (typeof v === "object") {
    for (const k of ["selector", "css", "path", "selectors", "element"]) {
      const s = firstSelector(v[k]); if (s) return s;
    }
  }
  return "";
}
function readFields(root) {
  const out = {};
  if (!Array.isArray(fields)) return out;
  for (const group of fields) {
    if (!group || typeof group !== "object") continue;
    for (const [name, cfg] of Object.entries(group)) {
      const s = firstSelector(cfg);
      const el = s ? root.querySelector(s) : null;
      out[name] = el ? (el.innerText || el.textContent || "").trim() : "";
    }
  }
  return out;
}
return Array.from(document.querySelectorAll(selector)).map((el, domIndex) => ({ el, domIndex }))
  .filter((item) => %t ? visible(item.el) : true)
  .slice(0, maxItems > 0 ? maxItems : undefined)
  .map((item, index) => ({
    index,
    dom_index: item.domIndex,
    text: (item.el.innerText || item.el.textContent || "").trim(),
    fields: readFields(item.el)
  }));
})()`, jsString(css), maxItems, jsJSON(fields), selector.Visible)
	value, err := c.evalLocked(ctx, expr)
	if err != nil {
		return nil, err
	}
	rawItems, _ := value.([]any)
	items := make([]ElementInfo, 0, len(rawItems))
	for _, raw := range rawItems {
		item, _ := raw.(map[string]any)
		domIndex := goIntFromAny(item["dom_index"])
		ref := c.RememberElementLocked(css, domIndex)
		fieldsMap, _ := item["fields"].(map[string]any)
		items = append(items, ElementInfo{
			Index:      goIntFromAny(item["index"]),
			Ref:        ref.ID,
			ElementRef: ref.ID,
			Text:       strings.TrimSpace(stringFromAny(item["text"])),
			Fields:     fieldsMap,
		})
	}
	return items, nil
}

// RememberElement 保存元素引用。
func (c *GoController) RememberElement(selector string) ElementRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.RememberElementLocked(selector, 0)
}

// RememberElementLocked 保存元素引用。
func (c *GoController) RememberElementLocked(selector string, index int) ElementRef {
	c.refSeq++
	ref := ElementRef{ID: fmt.Sprintf("go-el-%d", c.refSeq), Created: time.Now(), Selector: selector, Index: index}
	c.refs[ref.ID] = ref
	return ref
}

// GetElementByRef 根据引用读取元素。
func (c *GoController) GetElementByRef(ref string) (ElementRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.refs[ref]
	if !ok {
		return ElementRef{}, fmt.Errorf("Go 浏览器模式找不到元素引用：%s", ref)
	}
	return item, nil
}

// ClearElementRefs 清空元素引用。
func (c *GoController) ClearElementRefs() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refs = make(map[string]ElementRef)
}

// ClickElement 点击元素。
func (c *GoController) ClickElement(ctx context.Context, selector ElementSelector) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	expr, err := c.elementExprLocked(selector, "el.scrollIntoView({block:'center', inline:'nearest'}); el.click(); return true;")
	if err != nil {
		return err
	}
	_, err = c.evalLocked(ctx, expr)
	return err
}

// FillElement 输入文本。
func (c *GoController) FillElement(ctx context.Context, selector ElementSelector, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	body := fmt.Sprintf(`el.scrollIntoView({block:'center', inline:'nearest'});
el.focus();
if ("value" in el) {
  el.value = %s;
  el.dispatchEvent(new Event("input", {bubbles:true}));
  el.dispatchEvent(new Event("change", {bubbles:true}));
} else {
  el.textContent = %s;
}
return true;`, jsString(text), jsString(text))
	expr, err := c.elementExprLocked(selector, body)
	if err != nil {
		return err
	}
	_, err = c.evalLocked(ctx, expr)
	return err
}

// PressKey 按下键盘按键。
func (c *GoController) PressKey(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	info := keyInfo(key)
	_, err = page.client.Call(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type":                  "keyDown",
		"key":                   info.Key,
		"code":                  info.Code,
		"windowsVirtualKeyCode": info.CodeValue,
	})
	if err != nil {
		return err
	}
	_, err = page.client.Call(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type":                  "keyUp",
		"key":                   info.Key,
		"code":                  info.Code,
		"windowsVirtualKeyCode": info.CodeValue,
	})
	return err
}

// ScrollPage 滚动页面。
func (c *GoController) ScrollPage(ctx context.Context, distance int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if distance == 0 {
		distance = 700
	}
	_, err := c.evalLocked(ctx, fmt.Sprintf(`window.scrollBy(0, %d); true`, distance))
	return err
}

// ScrollElement 滚动元素。
func (c *GoController) ScrollElement(ctx context.Context, selector ElementSelector, distance int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if distance == 0 {
		distance = 700
	}
	body := fmt.Sprintf("el.scrollBy(0, %d); return true;", distance)
	expr, err := c.elementExprLocked(selector, body)
	if err != nil {
		return err
	}
	_, err = c.evalLocked(ctx, expr)
	return err
}

// ElementText 读取元素文本。
func (c *GoController) ElementText(ctx context.Context, selector ElementSelector) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if selector.Selector == "" && selector.Ref == "" {
		value, err := c.evalLocked(ctx, `(document.body && (document.body.innerText || document.body.textContent) || "").trim()`)
		return stringFromAny(value), err
	}
	expr, err := c.elementExprLocked(selector, "return (el.innerText || el.textContent || '').trim();")
	if err != nil {
		return "", err
	}
	value, err := c.evalLocked(ctx, expr)
	return stringFromAny(value), err
}

// ElementAttribute 读取元素属性。
func (c *GoController) ElementAttribute(ctx context.Context, selector ElementSelector, name string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expr, err := c.elementExprLocked(selector, fmt.Sprintf("return el.getAttribute(%s) || '';", jsString(name)))
	if err != nil {
		return "", err
	}
	value, err := c.evalLocked(ctx, expr)
	return stringFromAny(value), err
}

// ElementHTML 读取元素 HTML。
func (c *GoController) ElementHTML(ctx context.Context, selector ElementSelector) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expr, err := c.elementExprLocked(selector, "return el.outerHTML || '';")
	if err != nil {
		return "", err
	}
	value, err := c.evalLocked(ctx, expr)
	return stringFromAny(value), err
}

// ScreenshotPage 截取页面图片。
func (c *GoController) ScreenshotPage(ctx context.Context, options ScreenshotOptions) (ScreenshotResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.screenshotLocked(ctx, options, nil)
}

// ScreenshotElement 截取元素图片。
func (c *GoController) ScreenshotElement(ctx context.Context, options ScreenshotOptions) (ScreenshotResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	selector := ElementSelector{Selector: options.Selector, Ref: options.Ref}
	clip, err := c.elementClipLocked(ctx, selector)
	if err != nil {
		return ScreenshotResult{}, err
	}
	return c.screenshotLocked(ctx, options, clip)
}

// GetCookies 导出 Cookie。
func (c *GoController) GetCookies(ctx context.Context) ([]Cookie, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return nil, err
	}
	result, err := page.client.Call(ctx, "Network.getAllCookies", nil)
	if err != nil {
		return nil, err
	}
	rawCookies, _ := result["cookies"].([]any)
	cookies := make([]Cookie, 0, len(rawCookies))
	for _, raw := range rawCookies {
		item, _ := raw.(map[string]any)
		cookies = append(cookies, Cookie{
			Name:     stringFromAny(item["name"]),
			Value:    stringFromAny(item["value"]),
			Domain:   stringFromAny(item["domain"]),
			Path:     stringFromAny(item["path"]),
			Expires:  floatFromAny(item["expires"]),
			HTTPOnly: goBoolFromAny(item["httpOnly"]),
			Secure:   goBoolFromAny(item["secure"]),
		})
	}
	return cookies, nil
}

// SetCookies 导入 Cookie。
func (c *GoController) SetCookies(ctx context.Context, cookies []Cookie) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	params := make([]map[string]any, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name == "" {
			continue
		}
		item := map[string]any{"name": cookie.Name, "value": cookie.Value}
		if cookie.Domain != "" {
			item["domain"] = cookie.Domain
		}
		if cookie.Path != "" {
			item["path"] = cookie.Path
		}
		if cookie.Expires > 0 {
			item["expires"] = cookie.Expires
		}
		item["httpOnly"] = cookie.HTTPOnly
		item["secure"] = cookie.Secure
		params = append(params, item)
	}
	_, err = page.client.Call(ctx, "Network.setCookies", map[string]any{"cookies": params})
	return err
}

// SetDownloadDir 设置下载目录。
func (c *GoController) SetDownloadDir(ctx context.Context, dir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.setDownloadDirLocked(ctx, dir)
}

// ListDownloads 读取下载记录。
func (c *GoController) ListDownloads(ctx context.Context) ([]DownloadRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return append([]DownloadRecord(nil), c.downloads...), nil
}

// ClickListItemByIndex 是通用组合动作，不建议长期放这里。
// 建议迁到 internal/browser/actions.go，由 FindAll、ScrollElement、ClickElement 组合。
func (c *GoController) ClickListItemByIndex(ctx context.Context, payload map[string]any) error {
	selector := firstSelectorFromAny(payload["item"])
	if selector == "" {
		selector = firstSelectorFromAny(payload["element"])
	}
	if selector == "" {
		selector = firstSelectorFromAny(payload)
	}
	if selector == "" {
		return fmt.Errorf("Go 浏览器模式列表点击缺少选择器")
	}
	index := goIntFromAny(payload["index"])
	ref := ElementSelector{Selector: selector, Index: index}
	if clickTarget := firstSelectorFromAny(payload["click_target"]); clickTarget != "" {
		ref = ElementSelector{Selector: selector + " " + clickTarget, Index: index}
	}
	if clickTarget := firstSelectorFromAny(payload["clickTarget"]); clickTarget != "" {
		ref = ElementSelector{Selector: selector + " " + clickTarget, Index: index}
	}
	return c.ClickElement(ctx, ref)
}

// MarkOverlay 是通用可视化组合动作，不建议长期放这里。
// 建议迁到 internal/browser/actions.go，由基础 DOM 操作组合。
func (c *GoController) MarkOverlay(ctx context.Context, payload map[string]any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	label := stringFromAny(payload["label"])
	if label == "" {
		label = "GoodHR"
	}
	_, err := c.evalLocked(ctx, fmt.Sprintf(`(() => {
const badge = document.createElement("div");
badge.textContent = %s;
badge.style.cssText = "position:fixed;right:16px;bottom:16px;z-index:2147483647;background:#16794c;color:#fff;padding:8px 10px;border-radius:8px;font-size:12px;box-shadow:0 6px 16px rgba(0,0,0,.18)";
document.body.appendChild(badge);
setTimeout(() => badge.remove(), 2200);
return true;
})()`, jsString(label)))
	return map[string]any{"applied": err == nil, "worker": WorkerModeGo}, err
}

// ExtractPlatformCandidates 是平台个性化动作，不建议放这里。
// 建议迁到 internal/platforms/{platform}/runtime.go；这里仅按传入选择器做兼容复刻。
func (c *GoController) ExtractPlatformCandidates(ctx context.Context, payload map[string]any) (map[string]any, error) {
	selector := selectorFromPayload(payload)
	if selector.Selector == "" {
		selector.Selector = firstSelectorFromAny(mapValue(payload["rules"])["cards"])
	}
	items, err := c.FindAll(ctx, selector, payload["fields"], goIntFromAny(payload["max_items"]))
	if err != nil {
		return nil, err
	}
	candidates := make([]map[string]any, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, map[string]any{
			"card_index":  item.Index,
			"element_ref": item.ElementRef,
			"text":        item.Text,
			"fields":      item.Fields,
		})
	}
	return map[string]any{"candidates": candidates, "items": candidates, "count": len(candidates), "worker": WorkerModeGo}, nil
}

// ScrollPlatformCandidateList 是平台个性化动作，不建议放这里。
// 建议迁到 internal/platforms/{platform}/runtime.go；这里仅按传入选择器做兼容复刻。
func (c *GoController) ScrollPlatformCandidateList(ctx context.Context, payload map[string]any) error {
	selector := selectorFromPayload(payload)
	if selector.Selector == "" {
		selector.Selector = firstSelectorFromAny(mapValue(payload["rules"])["list"])
	}
	if selector.Selector != "" {
		return c.ScrollElement(ctx, selector, goIntFromAny(payload["distance"]))
	}
	return c.ScrollPage(ctx, goIntFromAny(payload["distance"]))
}

// GreetPlatformCandidate 是平台个性化动作，不建议放这里。
// 建议迁到 internal/platforms/{platform}/runtime.go；这里仅按传入按钮选择器做兼容复刻。
func (c *GoController) GreetPlatformCandidate(ctx context.Context, payload map[string]any) error {
	if ref := stringFromAny(payload["element_ref"]); ref != "" {
		if greetBtn := firstSelectorFromAny(payload["greet_button"]); greetBtn != "" {
			item, err := c.GetElementByRef(ref)
			if err == nil && item.Selector != "" {
				return c.ClickElement(ctx, ElementSelector{Selector: item.Selector + " " + greetBtn, Index: item.Index})
			}
		}
		return c.ClickElement(ctx, ElementSelector{Ref: ref})
	}
	return c.ClickListItemByIndex(ctx, payload)
}

// ExtractPlatformCandidateDetail 是平台个性化动作，不建议放这里。
// 建议迁到 internal/platforms/{platform}/runtime.go；这里仅按详情容器选择器提取文本和截图。
func (c *GoController) ExtractPlatformCandidateDetail(ctx context.Context, payload map[string]any) (map[string]any, error) {
	selector := selectorFromPayload(payload)
	if selector.Selector == "" {
		selector.Selector = firstSelectorFromAny(mapValue(payload["rules"])["detail_containers"])
	}
	text, textErr := c.ElementText(ctx, selector)
	screen, shotErr := c.screenshotFromPayload(ctx, payload)
	if textErr != nil && shotErr != nil {
		return nil, textErr
	}
	return map[string]any{
		"detail_text": text,
		"text":        text,
		"screenshot":  map[string]any{"path": screen.Path, "file": screen.File},
		"source":      "go-compatible",
		"worker":      WorkerModeGo,
	}, nil
}

// ClosePlatformCandidateDetail 是平台个性化动作，不建议放这里。
// 建议迁到 internal/platforms/{platform}/runtime.go；这里仅默认按 Escape 兼容关闭。
func (c *GoController) ClosePlatformCandidateDetail(ctx context.Context, payload map[string]any) error {
	key := stringFromAny(payload["key"])
	if key == "" {
		key = "Escape"
	}
	return c.PressKey(ctx, key)
}

func (c *GoController) scrollFromPayload(ctx context.Context, payload map[string]any) error {
	selector := selectorFromPayload(payload)
	if selector.Selector != "" || selector.Ref != "" {
		return c.ScrollElement(ctx, selector, goIntFromAny(payload["distance"]))
	}
	return c.ScrollPage(ctx, goIntFromAny(payload["distance"]))
}

func (c *GoController) screenshotFromPayload(ctx context.Context, payload map[string]any) (ScreenshotResult, error) {
	options := ScreenshotOptions{
		Selector: firstSelectorFromAny(payload),
		Ref:      stringFromAny(payload["ref"]),
		Dir:      stringFromAny(payload["dir"]),
		Filename: stringFromAny(payload["filename"]),
		FullPage: goBoolFromAny(payload["full_page"]),
	}
	if options.Dir == "" {
		options.Dir = stringFromAny(payload["directory"])
	}
	if options.Selector != "" || options.Ref != "" {
		if result, err := c.ScreenshotElement(ctx, options); err == nil {
			return result, nil
		}
	}
	return c.ScreenshotPage(ctx, options)
}

func (c *GoController) workerData(data any, err error) (map[string]any, error) {
	if data == nil {
		return map[string]any{"worker": WorkerModeGo}, err
	}
	if m, ok := data.(map[string]any); ok {
		m["worker"] = WorkerModeGo
		return m, err
	}
	raw, marshalErr := json.Marshal(data)
	if marshalErr != nil {
		return map[string]any{"worker": WorkerModeGo}, err
	}
	var result map[string]any
	if unmarshalErr := json.Unmarshal(raw, &result); unmarshalErr != nil {
		return map[string]any{"worker": WorkerModeGo}, err
	}
	result["worker"] = WorkerModeGo
	return result, err
}

func (c *GoController) statusLocked() BrowserStatus {
	url := ""
	if c.page != nil {
		url = c.page.URL
	}
	return BrowserStatus{
		Running:       c.isRunningLocked(),
		Worker:        WorkerModeGo,
		Experimental:  true,
		UserDataDir:   c.userDataDir,
		DownloadsPath: c.downloadsPath,
		CurrentURL:    url,
	}
}

func (c *GoController) stopLocked() (BrowserStatus, error) {
	if c.page != nil && c.page.client != nil {
		_ = c.page.client.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
	c.cmd = nil
	c.port = 0
	c.baseURL = ""
	c.page = nil
	c.refs = make(map[string]ElementRef)
	c.userDataDir = ""
	c.downloadsPath = ""
	return BrowserStatus{Running: false, Worker: WorkerModeGo, Experimental: true}, nil
}

func (c *GoController) isRunningLocked() bool {
	return c.cmd != nil && c.cmd.Process != nil && (c.cmd.ProcessState == nil || !c.cmd.ProcessState.Exited()) && c.page != nil && c.page.client != nil
}

func (c *GoController) ensurePageLocked(ctx context.Context) (*goPage, error) {
	if !c.isRunningLocked() {
		return nil, fmt.Errorf("Go 浏览器尚未启动")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return c.page, nil
}

func (c *GoController) evalLocked(ctx context.Context, expression string) (any, error) {
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return nil, err
	}
	result, err := page.client.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return nil, err
	}
	if details, ok := result["exceptionDetails"]; ok {
		return nil, fmt.Errorf("页面执行失败：%v", details)
	}
	remote, _ := result["result"].(map[string]any)
	if value, ok := remote["value"]; ok {
		return value, nil
	}
	return nil, nil
}

func (c *GoController) waitReadyLocked(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		value, err := c.evalLocked(ctx, `document.readyState`)
		if err == nil {
			state := stringFromAny(value)
			if state == "complete" || state == "interactive" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return nil
}

func (c *GoController) elementExprLocked(selector ElementSelector, body string) (string, error) {
	css := strings.TrimSpace(selector.Selector)
	index := selector.Index
	if selector.Ref != "" {
		ref, ok := c.refs[selector.Ref]
		if !ok {
			return "", fmt.Errorf("Go 浏览器模式找不到元素引用：%s", selector.Ref)
		}
		css = ref.Selector
		index = ref.Index
	}
	if css == "" {
		return "", fmt.Errorf("Go 浏览器模式缺少选择器")
	}
	return fmt.Sprintf(`(() => {
const nodes = Array.from(document.querySelectorAll(%s));
const el = nodes[%d] || nodes[0];
if (!el) throw new Error("元素不存在：%s");
%s
})()`, jsString(css), maxInt(0, index), escapeJSMessage(css), body), nil
}

func (c *GoController) elementClipLocked(ctx context.Context, selector ElementSelector) (map[string]any, error) {
	expr, err := c.elementExprLocked(selector, `const r = el.getBoundingClientRect();
return {x: Math.max(0, r.left), y: Math.max(0, r.top), width: Math.max(1, r.width), height: Math.max(1, r.height), scale: 1};`)
	if err != nil {
		return nil, err
	}
	value, err := c.evalLocked(ctx, expr)
	if err != nil {
		return nil, err
	}
	clip, _ := value.(map[string]any)
	if clip == nil {
		return nil, fmt.Errorf("Go 浏览器模式获取元素截图区域失败")
	}
	return clip, nil
}

func (c *GoController) screenshotLocked(ctx context.Context, options ScreenshotOptions, clip map[string]any) (ScreenshotResult, error) {
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return ScreenshotResult{}, err
	}
	dir := strings.TrimSpace(options.Dir)
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "goodhr-screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ScreenshotResult{}, err
	}
	filename := safeFilename(options.Filename)
	if filename == "" {
		filename = fmt.Sprintf("go-screenshot-%d.png", time.Now().UnixMilli())
	}
	params := map[string]any{"format": "png", "fromSurface": true, "captureBeyondViewport": true}
	if clip != nil {
		params["clip"] = clip
	}
	result, err := page.client.Call(ctx, "Page.captureScreenshot", params)
	if err != nil {
		return ScreenshotResult{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(stringFromAny(result["data"]))
	if err != nil {
		return ScreenshotResult{}, err
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return ScreenshotResult{}, err
	}
	return ScreenshotResult{Path: path, File: path}, nil
}

func (c *GoController) setDownloadDirLocked(ctx context.Context, dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	c.downloadsPath = dir
	_, err = page.client.Call(ctx, "Browser.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": dir})
	return err
}

type cdpClient struct {
	conn    *websocketConn
	mu      sync.Mutex
	nextID  int
	pending map[int]chan cdpMessage
	closed  chan struct{}
}

type cdpMessage struct {
	ID     int            `json:"id,omitempty"`
	Method string         `json:"method,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	Result map[string]any `json:"result,omitempty"`
	Error  *cdpError      `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func dialCDP(ctx context.Context, wsURL string) (*cdpClient, error) {
	conn, err := dialWebSocket(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	client := &cdpClient{conn: conn, pending: make(map[int]chan cdpMessage), closed: make(chan struct{})}
	go client.readLoop()
	return client, nil
}

func (c *cdpClient) Call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan cdpMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	payload := map[string]any{"id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	raw, _ := json.Marshal(payload)
	if err := c.conn.WriteText(raw); err != nil {
		c.removePending(id)
		return nil, err
	}
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return nil, fmt.Errorf("%s", msg.Error.Message)
		}
		if msg.Result == nil {
			msg.Result = map[string]any{}
		}
		return msg.Result, nil
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case <-c.closed:
		return nil, io.ErrClosedPipe
	}
}

func (c *cdpClient) Close() error {
	close(c.closed)
	return c.conn.Close()
}

func (c *cdpClient) removePending(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *cdpClient) readLoop() {
	for {
		raw, err := c.conn.ReadText()
		if err != nil {
			return
		}
		var msg cdpMessage
		if err := json.Unmarshal(raw, &msg); err != nil || msg.ID == 0 {
			continue
		}
		c.mu.Lock()
		ch := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

type websocketConn struct {
	conn net.Conn
	mu   sync.Mutex
}

func dialWebSocket(ctx context.Context, rawURL string) (*websocketConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	_, _ = rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, u.Host, key)
	if _, err := conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !strings.Contains(status, " 101 ") {
		_ = conn.Close()
		return nil, fmt.Errorf("WebSocket 握手失败：%s", strings.TrimSpace(status))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	return &websocketConn{conn: conn}, nil
}

func (c *websocketConn) WriteText(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var frame bytes.Buffer
	frame.WriteByte(0x81)
	length := len(payload)
	switch {
	case length < 126:
		frame.WriteByte(byte(length) | 0x80)
	case length <= math.MaxUint16:
		frame.WriteByte(126 | 0x80)
		_ = binary.Write(&frame, binary.BigEndian, uint16(length))
	default:
		frame.WriteByte(127 | 0x80)
		_ = binary.Write(&frame, binary.BigEndian, uint64(length))
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	frame.Write(mask)
	for i, b := range payload {
		frame.WriteByte(b ^ mask[i%4])
	}
	_, err := c.conn.Write(frame.Bytes())
	return err
}

func (c *websocketConn) ReadText() ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return nil, err
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	if length == 126 {
		var v uint16
		if err := binary.Read(c.conn, binary.BigEndian, &v); err != nil {
			return nil, err
		}
		length = uint64(v)
	} else if length == 127 {
		if err := binary.Read(c.conn, binary.BigEndian, &length); err != nil {
			return nil, err
		}
	}
	var mask []byte
	if masked {
		mask = make([]byte, 4)
		if _, err := io.ReadFull(c.conn, mask); err != nil {
			return nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	switch opcode {
	case 0x1, 0x2:
		return payload, nil
	case 0x8:
		return nil, io.EOF
	case 0x9:
		_ = c.writeControl(0xA, payload)
		return c.ReadText()
	default:
		return c.ReadText()
	}
}

func (c *websocketConn) writeControl(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	frame := []byte{0x80 | opcode, byte(len(payload)) | 0x80}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	frame = append(frame, mask...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := c.conn.Write(frame)
	return err
}

func (c *websocketConn) Close() error {
	return c.conn.Close()
}

func waitDevTools(ctx context.Context, baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/json/version", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("等待 CloakBrowser 调试端口超时")
}

func createOrFirstPage(ctx context.Context, baseURL string) (*goPage, error) {
	pages, _ := listPages(ctx, baseURL)
	if len(pages) > 0 {
		return pages[0], nil
	}
	for _, method := range []string{http.MethodPut, http.MethodGet} {
		req, _ := http.NewRequestWithContext(ctx, method, baseURL+"/json/new?about:blank", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		var page goPage
		err = json.NewDecoder(resp.Body).Decode(&page)
		_ = resp.Body.Close()
		if err == nil && page.WebSocketDebuggerURL != "" {
			return &page, nil
		}
	}
	return nil, fmt.Errorf("Go 浏览器模式创建页面失败")
}

func listPages(ctx context.Context, baseURL string) ([]*goPage, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("Go 浏览器尚未启动")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/json/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var pages []*goPage
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		return nil, err
	}
	filtered := pages[:0]
	for _, page := range pages {
		if page.WebSocketDebuggerURL != "" {
			filtered = append(filtered, page)
		}
	}
	return filtered, nil
}

func freeTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func resolveBrowserExecutable(raw string) string {
	candidates := []string{strings.TrimSpace(raw), strings.TrimSpace(os.Getenv("GOODHR_CLOAKBROWSER_PATH"))}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(base, "runtime", "cloakbrowser", browserExecutableName()),
			filepath.Join(base, "cloakbrowser", browserExecutableName()),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "runtime", "cloakbrowser", browserExecutableName()),
			filepath.Join(wd, "dist", "runtime", "cloakbrowser", browserExecutableName()),
		)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "Library", "Application Support", "GoodHR", "runtime", "cloakbrowser", browserExecutableName()),
			filepath.Join(home, "AppData", "Roaming", "GoodHR", "runtime", "cloakbrowser", browserExecutableName()),
		)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func browserExecutableName() string {
	switch goruntime.GOOS {
	case "windows":
		return "CloakBrowser.exe"
	case "darwin":
		return filepath.Join("CloakBrowser.app", "Contents", "MacOS", "CloakBrowser")
	default:
		return "CloakBrowser"
	}
}

// samePageURL 判断两个地址是否指向同一页面。
// current 为当前地址，target 为目标地址，比较时忽略 hash 和末尾斜杠。
func samePageURL(current string, target string) bool {
	left := normalizePageURL(current)
	right := normalizePageURL(target)
	return left != "" && right != "" && left == right
}

// normalizePageURL 归一化页面地址。
// value 为原始地址，返回去掉 hash 和末尾斜杠后的地址。
func normalizePageURL(value string) string {
	text := strings.TrimSpace(value)
	if text == "" || text == "about:blank" {
		return ""
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.Scheme == "" {
		return strings.TrimRight(strings.Split(text, "#")[0], "/")
	}
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func browserStartOptionsFromPayload(payload map[string]any) BrowserStartOptions {
	return BrowserStartOptions{
		ExecutablePath: stringFromAny(firstNonEmpty(payload["executable_path"], payload["browser_path"], payload["cloakbrowser_path"])),
		UserDataDir:    stringFromAny(payload["user_data_dir"]),
		DownloadsPath:  stringFromAny(payload["downloads_path"]),
		InitialURL:     stringFromAny(payload["url"]),
		Headless:       goBoolFromAny(payload["headless"]),
		Persistent:     goBoolFromAny(payload["persistent"]),
		ViewportWidth:  goIntFromAny(payload["viewport_width"]),
		ViewportHeight: goIntFromAny(payload["viewport_height"]),
	}
}

func selectorFromPayload(payload map[string]any) ElementSelector {
	selector := ElementSelector{
		Selector: firstSelectorFromAny(payload),
		Ref:      stringFromAny(firstNonEmpty(payload["element_ref"], payload["ref"])),
		Visible:  goBoolFromAny(payload["visible"]),
		Index:    goIntFromAny(payload["index"]),
	}
	if selector.Selector == "" {
		selector.Selector = firstSelectorFromAny(payload["element"])
	}
	return selector
}

func firstSelectorFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []string:
		for _, item := range v {
			if text := strings.TrimSpace(item); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range v {
			if text := firstSelectorFromAny(item); text != "" {
				return text
			}
		}
	case map[string]any:
		for _, key := range []string{"selector", "css", "path", "selectors"} {
			if text := firstSelectorFromAny(v[key]); text != "" {
				return text
			}
		}
		if text := firstSelectorFromAny(v["element"]); text != "" {
			return text
		}
	}
	return ""
}

func cookiesFromAny(value any) []Cookie {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	cookies := make([]Cookie, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		cookies = append(cookies, Cookie{
			Name:     stringFromAny(m["name"]),
			Value:    stringFromAny(m["value"]),
			Domain:   stringFromAny(m["domain"]),
			Path:     stringFromAny(m["path"]),
			Expires:  floatFromAny(m["expires"]),
			HTTPOnly: goBoolFromAny(m["httpOnly"]),
			Secure:   goBoolFromAny(m["secure"]),
		})
	}
	return cookies
}

func keyInfo(key string) struct {
	Key       string
	Code      string
	CodeValue int
} {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "enter":
		return struct {
			Key       string
			Code      string
			CodeValue int
		}{"Enter", "Enter", 13}
	case "escape", "esc":
		return struct {
			Key       string
			Code      string
			CodeValue int
		}{"Escape", "Escape", 27}
	case "tab":
		return struct {
			Key       string
			Code      string
			CodeValue int
		}{"Tab", "Tab", 9}
	case "backspace":
		return struct {
			Key       string
			Code      string
			CodeValue int
		}{"Backspace", "Backspace", 8}
	default:
		if key == "" {
			key = "Escape"
		}
		return struct {
			Key       string
			Code      string
			CodeValue int
		}{key, key, 0}
	}
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return map[string]any{}
	}
	return result
}

func mapValue(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func firstNonEmpty(values ...any) any {
	for _, value := range values {
		if strings.TrimSpace(stringFromAny(value)) != "" {
			return value
		}
	}
	return nil
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	default:
		text := strings.TrimSpace(fmt.Sprint(v))
		if text == "<nil>" {
			return ""
		}
		return text
	}
}

func goIntFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		parsed, _ := strconv.Atoi(v.String())
		return parsed
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(v))
		return parsed
	default:
		return 0
	}
}

func floatFromAny(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		parsed, _ := strconv.ParseFloat(v.String(), 64)
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed
	default:
		return 0
	}
}

func goBoolFromAny(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(v))
		return parsed
	default:
		return false
	}
}

func jsString(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func jsJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil || string(raw) == "null" {
		return "[]"
	}
	return string(raw)
}

func escapeJSMessage(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

func safeFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." {
		return fmt.Sprintf("go-screenshot-%d.png", time.Now().UnixMilli())
	}
	return name
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
