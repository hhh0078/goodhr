// Package zhaopin 实现智联招聘平台的页面、候选人和消息能力。
package zhaopin

import "goodhr5/local-agent-go-new/internal/platform/common"

// Runtime 是智联招聘平台运行时。
type Runtime struct {
	startFromViewport bool // 页面初始化仅对推荐页启用当前完整卡片起跑。
	viewportStarted   bool // 本轮只设置一次起点，后续批次不重置。
	common.CandidateResumeTracker
	positionName string
	manualSearch bool // 仅本次初始化确认的搜索页启用详情路径筛选，推荐页保持原流程。
}

// NewRuntime 创建智联招聘平台运行时。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// PlatformID 返回智联招聘平台编号。
func (r *Runtime) PlatformID() string {
	return "zhaopin"
}
