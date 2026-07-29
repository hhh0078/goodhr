// Package contract 定义 Go 与 TypeScript Browser Worker 之间唯一的强类型协议。
package contract

// SelectorCandidate 表示一个按顺序尝试的候选选择器。
type SelectorCandidate struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	Name  string `json:"name,omitempty"`
}

// SelectorGroup 表示一个层级的候选选择器和匹配约束。
type SelectorGroup struct {
	Selectors  []SelectorCandidate `json:"selectors"`
	Index      *int                `json:"index,omitempty"`
	Text       string              `json:"text,omitempty"`
	ExactText  *bool               `json:"exact_text,omitempty"`
	Attributes map[string]string   `json:"attributes,omitempty"`
}

// SelectorSpec 表示 iframe、父级和目标组成的统一定位方案。
type SelectorSpec struct {
	Frames        []SelectorGroup `json:"frames,omitempty"`
	Parents       []SelectorGroup `json:"parents,omitempty"`
	Target        SelectorGroup   `json:"target"`
	State         string          `json:"state,omitempty"`
	TimeoutMS     int             `json:"timeout_ms,omitempty"`
	ReadProperty  string          `json:"read_property,omitempty"`
	ReadAttribute string          `json:"read_attribute,omitempty"`
	Description   string          `json:"description"`
}

// ElementBox 表示元素在视口内的位置。
type ElementBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ViewportSize 表示页面视口大小。
type ViewportSize struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ElementView 表示元素的可见、可用和视口状态。
type ElementView struct {
	Box             ElementBox   `json:"box"`
	Viewport        ViewportSize `json:"viewport"`
	Visible         bool         `json:"visible"`
	Enabled         bool         `json:"enabled"`
	InViewport      bool         `json:"in_viewport"`
	FullyInViewport bool         `json:"fully_in_viewport"`
}

// SelectorAttempt 表示一次选择器尝试的诊断结果。
type SelectorAttempt struct {
	Level         string `json:"level"`
	SelectorType  string `json:"selector_type"`
	SelectorValue string `json:"selector_value"`
	Matches       int    `json:"matches"`
	SelectedIndex int    `json:"selected_index"`
}

// FindResult 表示查找结果和短生命周期元素引用。
type FindResult struct {
	ElementRef      string            `json:"element_ref"`
	Description     string            `json:"description"`
	MatchedSelector SelectorCandidate `json:"matched_selector"`
	Attempts        []SelectorAttempt `json:"attempts"`
	View            ElementView       `json:"view"`
	PageID          string            `json:"page_id"`
	PageURL         string            `json:"page_url"`
}

// ProxyConfig 表示浏览器代理配置。
type ProxyConfig struct {
	Server   string `json:"server"`
	Bypass   string `json:"bypass,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// BrowserStartRequest 表示 CloakBrowser 启动参数。
type BrowserStartRequest struct {
	UserDataDir    string       `json:"user_data_dir,omitempty"`
	DownloadsPath  string       `json:"downloads_path,omitempty"`
	Headless       *bool        `json:"headless,omitempty"`
	Humanize       *bool        `json:"humanize,omitempty"`
	GeoIP          *bool        `json:"geoip,omitempty"`
	URL            string       `json:"url,omitempty"`
	WaitUntil      string       `json:"wait_until,omitempty"`
	TimeoutMS      int          `json:"timeout_ms,omitempty"`
	NewTab         *bool        `json:"new_tab,omitempty"`
	Locale         string       `json:"locale,omitempty"`
	Timezone       string       `json:"timezone,omitempty"`
	UserAgent      string       `json:"user_agent,omitempty"`
	ViewportWidth  int          `json:"viewport_width,omitempty"`
	ViewportHeight int          `json:"viewport_height,omitempty"`
	Proxy          *ProxyConfig `json:"proxy,omitempty"`
	Args           []string     `json:"args,omitempty"`
}

// BrowserStatus 表示 Browser Worker 会话状态。
type BrowserStatus struct {
	Running       bool   `json:"running"`
	Persistent    bool   `json:"persistent"`
	Reused        bool   `json:"reused"`
	UserDataDir   string `json:"user_data_dir"`
	DownloadsPath string `json:"downloads_path"`
	CurrentURL    string `json:"current_url"`
}

// WorkerRuntimeStatus 表示 CloakBrowser 增强二进制安装状态。
type WorkerRuntimeStatus struct {
	CloakBrowserVersion string `json:"cloakbrowser_version"`
	Platform            string `json:"platform"`
	BinaryPath          string `json:"binary_path"`
	Installed           bool   `json:"installed"`
}

// PageOpenRequest 表示打开页面请求。
type PageOpenRequest struct {
	URL       string `json:"url"`
	WaitUntil string `json:"wait_until,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	NewTab    *bool  `json:"new_tab,omitempty"`
}

// PageInfo 表示一个浏览器标签页。
type PageInfo struct {
	PageID  string `json:"page_id"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Current bool   `json:"current"`
}

// PageListResult 表示当前全部浏览器标签页。
type PageListResult struct {
	Pages []PageInfo `json:"pages"`
	Count int        `json:"count"`
}

// PageUseRequest 表示切换当前标签页请求。
type PageUseRequest struct {
	PageID string `json:"page_id"`
}

// ElementFindRequest 表示查找一个元素请求。
type ElementFindRequest struct {
	Selector SelectorSpec `json:"selector"`
}

// ElementFindAllRequest 表示查找元素列表和字段请求。
type ElementFindAllRequest struct {
	Selector SelectorSpec            `json:"selector"`
	MaxItems int                     `json:"max_items,omitempty"`
	Fields   map[string]SelectorSpec `json:"fields,omitempty"`
}

// FindAllItem 表示列表查找中的一个元素。
type FindAllItem struct {
	Index      int               `json:"index"`
	ElementRef string            `json:"element_ref"`
	Text       string            `json:"text"`
	Fields     map[string]string `json:"fields"`
}

// ElementReadRequest 表示元素读取请求。
type ElementReadRequest struct {
	Selector  SelectorSpec `json:"selector"`
	Property  string       `json:"property,omitempty"`
	Attribute string       `json:"attribute,omitempty"`
}

// ReadResult 表示元素读取结果。
type ReadResult struct {
	Value      string `json:"value"`
	Property   string `json:"property"`
	ElementRef string `json:"element_ref"`
}

// ClickVerification 表示点击后的结果验证。
type ClickVerification struct {
	URLContains   string        `json:"url_contains,omitempty"`
	TargetHidden  *SelectorSpec `json:"target_hidden,omitempty"`
	TargetVisible *SelectorSpec `json:"target_visible,omitempty"`
	TimeoutMS     int           `json:"timeout_ms,omitempty"`
}

// ElementClickRequest 表示完整封装点击请求。
type ElementClickRequest struct {
	Selector         SelectorSpec       `json:"selector"`
	Button           string             `json:"button,omitempty"`
	ClickCount       int                `json:"click_count,omitempty"`
	ViewportMargin   int                `json:"viewport_margin,omitempty"`
	WaitForNewPage   bool               `json:"wait_for_new_page,omitempty"`
	NewPageTimeoutMS int                `json:"new_page_timeout_ms,omitempty"`
	Verify           *ClickVerification `json:"verify,omitempty"`
}

// ClickResult 表示完整封装点击结果。
type ClickResult struct {
	Clicked       bool   `json:"clicked"`
	ElementRef    string `json:"element_ref"`
	HoldMS        int    `json:"hold_ms"`
	Verified      bool   `json:"verified"`
	NewPageOpened bool   `json:"new_page_opened"`
	NewPageURL    string `json:"new_page_url"`
}

// ElementInputRequest 表示完整封装输入请求。
type ElementInputRequest struct {
	Selector   SelectorSpec `json:"selector"`
	Text       string       `json:"text"`
	Clear      *bool        `json:"clear,omitempty"`
	Verify     *bool        `json:"verify,omitempty"`
	MinDelayMS int          `json:"min_delay_ms,omitempty"`
	MaxDelayMS int          `json:"max_delay_ms,omitempty"`
}

// InputResult 表示完整封装输入结果。
type InputResult struct {
	Typed      bool   `json:"typed"`
	Length     int    `json:"length"`
	Verified   bool   `json:"verified"`
	ElementRef string `json:"element_ref"`
}

// KeyboardPressRequest 表示通用按键请求。
type KeyboardPressRequest struct {
	Key     string `json:"key"`
	DelayMS int    `json:"delay_ms,omitempty"`
}

// KeyboardPressResult 表示通用按键结果。
type KeyboardPressResult struct {
	Pressed bool   `json:"pressed"`
	Key     string `json:"key"`
}

// ScrollRequest 表示真实鼠标滚轮请求。
type ScrollRequest struct {
	Target         *SelectorSpec `json:"target,omitempty"`
	WheelAnchor    *SelectorSpec `json:"wheel_anchor,omitempty"`
	Distance       int           `json:"distance"`
	MaxAttempts    int           `json:"max_attempts,omitempty"`
	WaitMS         int           `json:"wait_ms,omitempty"`
	RequireFull    *bool         `json:"require_full,omitempty"`
	ViewportMargin int           `json:"viewport_margin,omitempty"`
}

// ScrollResult 表示真实滚轮次数和前后状态。
type ScrollResult struct {
	Scrolled bool `json:"scrolled"`
	Attempts int  `json:"attempts"`
	Distance int  `json:"distance"`
}

// ScreenshotRequest 表示页面或元素截图请求。
type ScreenshotRequest struct {
	Target    *SelectorSpec `json:"target,omitempty"`
	Directory string        `json:"directory"`
	Filename  string        `json:"filename"`
	FullPage  *bool         `json:"full_page,omitempty"`
}

// ScreenshotResult 表示截图保存结果。
type ScreenshotResult struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

// LongScreenshotRequest 表示使用真实鼠标滚轮分段截取长元素的请求。
type LongScreenshotRequest struct {
	Target      SelectorSpec  `json:"target"`
	WheelAnchor *SelectorSpec `json:"wheel_anchor,omitempty"`
	Directory   string        `json:"directory"`
	Filename    string        `json:"filename"`
	Distance    int           `json:"distance,omitempty"`
	MaxParts    int           `json:"max_parts,omitempty"`
	WaitMS      int           `json:"wait_ms,omitempty"`
}

// ScreenshotPart 表示长截图中的一个本地 PNG 分段。
type ScreenshotPart struct {
	Path           string `json:"path"`
	Filename       string `json:"filename"`
	Size           int64  `json:"size"`
	Index          int    `json:"index"`
	ScrollPosition int    `json:"scroll_position"`
}

// LongScreenshotResult 表示真实滚轮长截图的全部分段。
type LongScreenshotResult struct {
	Parts    []ScreenshotPart `json:"parts"`
	Count    int              `json:"count"`
	Complete bool             `json:"complete"`
}

// Cookie 表示 Go 与 Worker 共享的浏览器 Cookie。
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

// CookieListResult 表示浏览器 Cookie 列表。
type CookieListResult struct {
	Cookies []Cookie `json:"cookies"`
	Count   int      `json:"count"`
}

// CookieSetRequest 表示导入浏览器 Cookie 请求。
type CookieSetRequest struct {
	Cookies []Cookie `json:"cookies"`
}

// DownloadRecord 表示一条浏览器下载记录。
type DownloadRecord struct {
	ID                string `json:"id"`
	Filename          string `json:"filename"`
	FileName          string `json:"file_name"`
	FilePath          string `json:"file_path"`
	Path              string `json:"path"`
	SuggestedFilename string `json:"suggested_filename"`
	URL               string `json:"url"`
	PageURL           string `json:"page_url"`
	Size              int64  `json:"size"`
	Status            string `json:"status"`
	Error             string `json:"error"`
	CreatedAt         string `json:"created_at"`
}

// DownloadListResult 表示浏览器下载记录列表。
type DownloadListResult struct {
	Downloads []DownloadRecord `json:"downloads"`
	Count     int              `json:"count"`
	Pending   int              `json:"pending"`
	Directory string           `json:"directory"`
}

// DownloadConfigureRequest 表示切换后续下载保存目录请求。
type DownloadConfigureRequest struct {
	Directory string `json:"directory"`
}

// OverlayShowRequest 表示通用页面浮层内容。
type OverlayShowRequest struct {
	OverlayID string `json:"overlay_id"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Message   string `json:"message"`
	Level     string `json:"level,omitempty"`
	MaxAgeMS  int    `json:"max_age_ms,omitempty"`
}

// OverlayCloseRequest 表示关闭通用页面浮层请求。
type OverlayCloseRequest struct {
	OverlayID string `json:"overlay_id"`
}

// OverlayResult 表示通用页面浮层操作结果。
type OverlayResult struct {
	Shown     bool   `json:"shown,omitempty"`
	Closed    bool   `json:"closed,omitempty"`
	OverlayID string `json:"overlay_id"`
}
