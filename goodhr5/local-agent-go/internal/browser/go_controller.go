// Package browser 提供 Go 直连 CloakBrowser 的浏览器控制器。
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
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
	mu             sync.Mutex
	executablePath string
	cmd            *exec.Cmd
	port           int
	baseURL        string
	page           *goPage
	refs           map[string]ElementRef
	refSeq         int
	downloads      []DownloadRecord
	downloadsPath  string
	userDataDir    string
}

// GoControllerOptions 表示实验性 Go 浏览器库的入口配置。
type GoControllerOptions struct {
	ExecutablePath string
}

// BrowserStartOptions 表示浏览器启动参数。
type BrowserStartOptions struct {
	ExecutablePath string `json:"executable_path,omitempty"`
	UserDataDir    string `json:"user_data_dir,omitempty"`
	DownloadsPath  string `json:"downloads_path,omitempty"`
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

// ElementView 表示元素在当前浏览器视口中的位置和可见状态。
type ElementView struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	ViewportW  float64 `json:"viewport_width"`
	ViewportH  float64 `json:"viewport_height"`
	Visible    bool    `json:"visible"`
	InViewport bool    `json:"in_viewport"`
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

// 确保实验性 Go 控制器持续满足任务运行器依赖的 Worker 接口形态。
var _ interface {
	Start(context.Context) (WorkerStatus, error)
	Call(context.Context, string, any) (map[string]any, error)
} = (*GoController)(nil)

// NewGoController 创建 Go 浏览器控制器。
func NewGoController() *GoController {
	return NewGoControllerWithOptions(GoControllerOptions{})
}

// NewGoControllerWithOptions 使用明确配置创建实验性 Go 浏览器控制器。
func NewGoControllerWithOptions(options GoControllerOptions) *GoController {
	return &GoController{executablePath: strings.TrimSpace(options.ExecutablePath), refs: make(map[string]ElementRef)}
}

// Start 实现 BrowserWorker 的启动接口。
func (c *GoController) Start(ctx context.Context) (WorkerStatus, error) {
	status, err := c.StartBrowser(ctx, BrowserStartOptions{ExecutablePath: c.executablePath})
	return WorkerStatus{Running: status.Running, BaseURL: "go://cloakbrowser", Managed: true}, err
}

// Call 按 Node Worker 路由调用 Go 控制器。
func (c *GoController) Call(ctx context.Context, path string, payload any) (map[string]any, error) {
	data := mapFromAny(payload)
	switch path {
	case "/api/v1/browser/start":
		options := browserStartOptionsFromPayload(data)
		if options.ExecutablePath == "" {
			options.ExecutablePath = c.executablePath
		}
		return c.workerData(c.StartBrowser(ctx, options))
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
	case "/api/v1/page/ensure-visible":
		result, err := c.EnsureElementVisible(ctx, selectorFromPayload(data), goIntFromAny(data["distance"]), goIntFromAny(data["max_attempts"]))
		result["worker"] = WorkerModeGo
		return result, err
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
	case "/api/v1/boss/candidates/visible":
		result, err := c.EnsureElementVisible(ctx, selectorFromPayload(data), goIntFromAny(data["distance"]), goIntFromAny(data["max_attempts"]))
		result["worker"] = WorkerModeGo
		return result, err
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
