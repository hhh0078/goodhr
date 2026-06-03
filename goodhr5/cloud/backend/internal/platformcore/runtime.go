// Package platformcore 定义平台运行时的统一接口与共享数据结构。
package platformcore

import "fmt"

// RuntimeExecutor 定义平台运行时调用本地 Agent 的统一执行器。
type RuntimeExecutor interface {
	// Post 向本地 Agent 发送请求并解析响应。
	Post(path string, body any, result any) error
	// Delay 在云端后端等待指定业务动作延时。
	Delay(label string, minSeconds float64, maxSeconds float64) error
	// Log 输出运行日志。
	Log(level, message string)
}

// RuntimeConfig 定义平台运行时所需的配置快照。
type RuntimeConfig struct {
	PlatformID   string
	EntryPages   []RuntimePage
	Card         RuntimeCardConfig
	Actions      RuntimeActionsConfig
	Detail       RuntimeDetailConfig
	Position     RuntimePositionConfig
	Behavior     RuntimeBehaviorConfig
	PlatformName string
}

// RuntimePage 定义平台页面入口配置。
type RuntimePage struct {
	URL   string
	Title string
	Match string
	Entry bool
}

// RuntimeCardConfig 定义候选人卡片相关定位配置。
type RuntimeCardConfig struct {
	CardElement   map[string]any
	ScrollElement map[string]any
	FieldRequests []map[string]any
}

// RuntimeActionsConfig 定义候选人动作按钮配置。
type RuntimeActionsConfig struct {
	GreetBtn    map[string]any
	ContinueBtn map[string]any
	ConfirmBtn  map[string]any
}

// RuntimeDetailConfig 定义候选人详情配置。
type RuntimeDetailConfig struct {
	OpenTarget map[string]any
	CloseBtn   map[string]any
	Content    map[string]any
}

// RuntimePositionConfig 定义页面当前岗位和岗位切换所需定位配置。
type RuntimePositionConfig struct {
	Current      map[string]any
	SwitchButton map[string]any
	List         map[string]any
	Item         map[string]any
}

// RuntimeBehaviorConfig 定义平台行为配置。
type RuntimeBehaviorConfig struct {
	NeedsDetailPage bool
}

// RuntimePreferences 定义运行时需要的偏好设置。
type RuntimePreferences struct {
	DetailOpenDelayMin  float64
	DetailOpenDelayMax  float64
	DetailCloseDelayMin float64
	DetailCloseDelayMax float64
	GreetBeforeDelayMin float64
	GreetBeforeDelayMax float64
	GreetMessage        string
	VisionAIBaseURL     string
	VisionAIAPIKey      string
	VisionAIModel       string
	VisionAIPrompt      string
}

// RuntimeScrollOptions 定义候选人列表滚动参数。
type RuntimeScrollOptions struct {
	DistanceMin int
	DistanceMax int
}

// PlatformRuntime 定义主流程调用的平台运行时能力。
type PlatformRuntime interface {
	// OpenEntryPage 打开平台任务入口页面。
	OpenEntryPage(exec RuntimeExecutor, cfg RuntimeConfig, cookies []map[string]any) error
	// IsEntryPage 判断当前默认页面是否仍是平台任务入口页。
	IsEntryPage(exec RuntimeExecutor, cfg RuntimeConfig) (bool, error)
	// CurrentPositionName 读取当前页面选中的岗位名称。
	CurrentPositionName(exec RuntimeExecutor, cfg RuntimeConfig) (string, error)
	// SelectPosition 在平台页面切换到指定岗位名称。
	SelectPosition(exec RuntimeExecutor, cfg RuntimeConfig, positionName string) error
	// ListVisibleCandidates 提取当前可见候选人。
	ListVisibleCandidates(exec RuntimeExecutor, cfg RuntimeConfig) ([]Candidate, error)
	// ScrollCandidateList 滚动到下一屏候选人。
	ScrollCandidateList(exec RuntimeExecutor, cfg RuntimeConfig, prefs RuntimePreferences, options RuntimeScrollOptions) error
	// GreetCandidate 执行候选人打招呼动作。
	GreetCandidate(exec RuntimeExecutor, cfg RuntimeConfig, prefs RuntimePreferences, candidate Candidate) error
	// OpenCandidateDetail 打开候选人详情面板。
	OpenCandidateDetail(exec RuntimeExecutor, cfg RuntimeConfig, prefs RuntimePreferences, candidate Candidate) error
	// CloseCandidateDetail 关闭候选人详情面板。
	CloseCandidateDetail(exec RuntimeExecutor, cfg RuntimeConfig, prefs RuntimePreferences) error
	// FetchCandidateDetailText 读取候选人详情文本。
	FetchCandidateDetailText(exec RuntimeExecutor, cfg RuntimeConfig, prefs RuntimePreferences, candidate Candidate, detailMode string) (string, error)
	// CandidateFilterText 返回用于筛选的候选人文本。
	CandidateFilterText(candidate Candidate) string
	// CandidateFingerprint 返回候选人去重指纹。
	CandidateFingerprint(candidate Candidate) string
	// MapFieldsToCandidate 将原始字段映射为候选人对象。
	MapFieldsToCandidate(platformID string, fields map[string]any) Candidate
}

// ErrRuntimeNotImplemented 表示平台运行时未实现。
func ErrRuntimeNotImplemented(platformID string) error {
	return fmt.Errorf("平台 %s 未实现 runtime", platformID)
}
