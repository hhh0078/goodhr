// Package job51 实现前程无忧平台的页面、候选人和消息能力。
package job51

import "goodhr5/local-agent-go-new/internal/platform/model"

// Runtime 是前程无忧平台运行时。
type Runtime struct {
	positionName   string
	manualSearch   bool // 搜索页保留原名单顺序核对，推荐页允许按公开编号找回换序后的锚点。
	resumeTracking bool
	resumePoints   map[string]model.CandidateResumePoint
}

// NewRuntime 创建前程无忧平台运行时。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// PlatformID 返回前程无忧平台编号。
func (r *Runtime) PlatformID() string {
	return "51job"
}
