// Package platformcore 定义本地任务平台运行时的统一接口。
package platformcore

import (
	"context"

	"goodhr5/local-agent-go/internal/cloudapi"
)

// Executor 定义平台实现调用浏览器 Worker 的统一执行器。
type Executor interface {
	// Post 调用浏览器 Worker 接口并返回响应。
	Post(ctx context.Context, path string, payload any) (map[string]any, error)
	// Log 写入任务日志。
	Log(level string, message string)
	// Delay 按业务动作等待指定秒数。
	Delay(ctx context.Context, label string, seconds float64) error
}

// Candidate 表示平台抽取到的候选人。
type Candidate map[string]any

// DetailRequest 表示读取候选人详情的请求。
type DetailRequest struct {
	TaskID         string
	Mode           string
	ScreenshotsDir string
	Filename       string
}

// DetailResult 表示平台读取候选人详情后的统一结果。
type DetailResult struct {
	Text       string
	Screenshot map[string]any
	Source     string
}

// Runtime 定义主流程调用的平台能力。
type Runtime interface {
	// OpenEntryPage 打开平台入口页面。
	OpenEntryPage(ctx context.Context, exec Executor, cfg cloudapi.PlatformConfig, entryURL string) error
	// IsTaskEntryPage 判断当前页面是否仍是任务入口页面。
	IsTaskEntryPage(ctx context.Context, exec Executor, cfg cloudapi.PlatformConfig) (bool, error)
	// CurrentPositionName 读取当前页面岗位名称。
	CurrentPositionName(ctx context.Context, exec Executor, cfg cloudapi.PlatformConfig) (string, error)
	// SelectPosition 切换当前页面岗位。
	SelectPosition(ctx context.Context, exec Executor, cfg cloudapi.PlatformConfig, positionName string) error
	// ListVisibleCandidates 提取当前可见候选人。
	ListVisibleCandidates(ctx context.Context, exec Executor, cfg cloudapi.PlatformConfig, maxItems int) ([]Candidate, error)
	// ScrollCandidateList 滚动候选人列表。
	ScrollCandidateList(ctx context.Context, exec Executor, cfg cloudapi.PlatformConfig, distance int) error
	// FetchCandidateDetail 读取候选人详情。
	FetchCandidateDetail(ctx context.Context, exec Executor, cfg cloudapi.PlatformConfig, candidate Candidate, request DetailRequest) (DetailResult, error)
	// CloseCandidateDetail 关闭候选人详情。
	CloseCandidateDetail(ctx context.Context, exec Executor, cfg cloudapi.PlatformConfig, candidate Candidate) error
	// GreetCandidate 执行候选人打招呼。
	GreetCandidate(ctx context.Context, exec Executor, cfg cloudapi.PlatformConfig, candidate Candidate) error
	// CandidateFilterText 返回候选人筛选文本。
	CandidateFilterText(candidate Candidate) string
	// CandidateFingerprint 返回候选人去重指纹。
	CandidateFingerprint(candidate Candidate) string
}
