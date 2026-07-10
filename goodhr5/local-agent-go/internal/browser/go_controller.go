// Package browser 定义 Go 直连浏览器的基础控制器边界。
package browser

import (
	"context"
	"errors"
	"fmt"
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
	// ErrGoBrowserNotReady 表示 Go 浏览器控制器还没有接入真实 CDP 实现。
	ErrGoBrowserNotReady = errors.New("Go 浏览器模式还在试运行，我还没完全接上浏览器控制")
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

// GoController 是 Go 浏览器控制器的基础结构。
type GoController struct {
	mu          sync.Mutex
	running     bool
	currentURL  string
	currentPage int
	refs        map[string]ElementRef
	refSeq      int
	downloads   []DownloadRecord
}

// BrowserStartOptions 表示浏览器启动参数。
type BrowserStartOptions struct {
	ExecutablePath string `json:"executable_path,omitempty"`
	UserDataDir    string `json:"user_data_dir,omitempty"`
	DownloadsPath  string `json:"downloads_path,omitempty"`
	Headless       bool   `json:"headless,omitempty"`
	Persistent     bool   `json:"persistent,omitempty"`
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
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// ElementSelector 表示元素选择器配置。
type ElementSelector struct {
	Selector string `json:"selector,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Visible  bool   `json:"visible,omitempty"`
}

// ElementRef 表示一个已缓存的元素引用。
type ElementRef struct {
	ID       string    `json:"id"`
	Created  time.Time `json:"created"`
	Selector string    `json:"selector,omitempty"`
}

// ElementInfo 表示页面元素的简要信息。
type ElementInfo struct {
	Index int    `json:"index"`
	Ref   string `json:"ref,omitempty"`
	Text  string `json:"text,omitempty"`
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

// NewGoController 创建 Go 浏览器控制器。
func NewGoController() *GoController {
	return &GoController{refs: make(map[string]ElementRef)}
}

// StartBrowser 启动浏览器。
func (c *GoController) StartBrowser(ctx context.Context, options BrowserStartOptions) (BrowserStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return BrowserStatus{}, ctx.Err()
	default:
	}
	c.running = true
	return BrowserStatus{
		Running:       true,
		Worker:        WorkerModeGo,
		Experimental:  true,
		UserDataDir:   options.UserDataDir,
		DownloadsPath: options.DownloadsPath,
		Message:       ErrGoBrowserNotReady.Error(),
	}, ErrGoBrowserNotReady
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
	c.running = false
	c.currentURL = ""
	c.currentPage = 0
	c.refs = make(map[string]ElementRef)
	return BrowserStatus{Running: false, Worker: WorkerModeGo, Experimental: true}, nil
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
	return BrowserStatus{Running: c.running, Worker: WorkerModeGo, Experimental: true, CurrentURL: c.currentURL}, nil
}

// ListPages 列出当前页面。
func (c *GoController) ListPages(ctx context.Context) ([]PageInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if !c.running {
		return nil, ErrGoBrowserNotReady
	}
	return []PageInfo{{Index: c.currentPage, URL: c.currentURL}}, nil
}

// UsePage 切换当前页面。
func (c *GoController) UsePage(ctx context.Context, index int) (PageInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return PageInfo{}, ctx.Err()
	default:
	}
	c.currentPage = index
	return PageInfo{Index: index, URL: c.currentURL}, ErrGoBrowserNotReady
}

// CurrentURL 读取当前页面地址。
func (c *GoController) CurrentURL(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	return c.currentURL, nil
}

// OpenPage 打开指定页面地址。
func (c *GoController) OpenPage(ctx context.Context, rawURL string) (PageInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return PageInfo{}, ctx.Err()
	default:
	}
	if rawURL == "" {
		return PageInfo{}, fmt.Errorf("页面地址不能为空")
	}
	c.currentURL = rawURL
	return PageInfo{Index: c.currentPage, URL: rawURL}, ErrGoBrowserNotReady
}

// ReloadPage 刷新当前页面。
func (c *GoController) ReloadPage(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrGoBrowserNotReady
	}
}

// WaitPageLoad 等待页面加载完成。
func (c *GoController) WaitPageLoad(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrGoBrowserNotReady
	}
}

// FindOne 查找一个元素。
func (c *GoController) FindOne(ctx context.Context, selector ElementSelector) (ElementRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return ElementRef{}, ctx.Err()
	default:
	}
	if selector.Ref != "" {
		if ref, ok := c.refs[selector.Ref]; ok {
			return ref, nil
		}
	}
	return ElementRef{}, ErrGoBrowserNotReady
}

// FindAll 查找多个元素。
func (c *GoController) FindAll(ctx context.Context, selector ElementSelector) ([]ElementInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrGoBrowserNotReady
	}
}

// RememberElement 保存元素引用。
func (c *GoController) RememberElement(selector string) ElementRef {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.refSeq++
	ref := ElementRef{ID: fmt.Sprintf("go-el-%d", c.refSeq), Created: time.Now(), Selector: selector}
	c.refs[ref.ID] = ref
	return ref
}

// GetElementByRef 根据引用读取元素。
func (c *GoController) GetElementByRef(ref string) (ElementRef, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.refs[ref]
	return item, ok
}

// ClearElementRefs 清空元素引用。
func (c *GoController) ClearElementRefs() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.refs = make(map[string]ElementRef)
}

// ClickElement 点击元素。
func (c *GoController) ClickElement(ctx context.Context, selector ElementSelector) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrGoBrowserNotReady
	}
}

// FillElement 输入文本。
func (c *GoController) FillElement(ctx context.Context, selector ElementSelector, text string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrGoBrowserNotReady
	}
}

// PressKey 按下键盘按键。
func (c *GoController) PressKey(ctx context.Context, key string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrGoBrowserNotReady
	}
}

// ScrollPage 滚动页面。
func (c *GoController) ScrollPage(ctx context.Context, distance int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrGoBrowserNotReady
	}
}

// ScrollElement 滚动元素。
func (c *GoController) ScrollElement(ctx context.Context, selector ElementSelector, distance int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrGoBrowserNotReady
	}
}

// ElementText 读取元素文本。
func (c *GoController) ElementText(ctx context.Context, selector ElementSelector) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
		return "", ErrGoBrowserNotReady
	}
}

// ElementAttribute 读取元素属性。
func (c *GoController) ElementAttribute(ctx context.Context, selector ElementSelector, name string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
		return "", ErrGoBrowserNotReady
	}
}

// ElementHTML 读取元素 HTML。
func (c *GoController) ElementHTML(ctx context.Context, selector ElementSelector) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
		return "", ErrGoBrowserNotReady
	}
}

// ScreenshotPage 截取页面图片。
func (c *GoController) ScreenshotPage(ctx context.Context, options ScreenshotOptions) (ScreenshotResult, error) {
	select {
	case <-ctx.Done():
		return ScreenshotResult{}, ctx.Err()
	default:
		return ScreenshotResult{}, ErrGoBrowserNotReady
	}
}

// ScreenshotElement 截取元素图片。
func (c *GoController) ScreenshotElement(ctx context.Context, options ScreenshotOptions) (ScreenshotResult, error) {
	select {
	case <-ctx.Done():
		return ScreenshotResult{}, ctx.Err()
	default:
		return ScreenshotResult{}, ErrGoBrowserNotReady
	}
}

// GetCookies 导出 Cookie。
func (c *GoController) GetCookies(ctx context.Context) ([]Cookie, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrGoBrowserNotReady
	}
}

// SetCookies 导入 Cookie。
func (c *GoController) SetCookies(ctx context.Context, cookies []Cookie) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrGoBrowserNotReady
	}
}

// SetDownloadDir 设置下载目录。
func (c *GoController) SetDownloadDir(ctx context.Context, dir string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrGoBrowserNotReady
	}
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
