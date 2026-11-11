// Package liepin 实现猎聘企业端平台的页面、候选人和消息能力。
package liepin

import (
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
)

// Runtime 是猎聘企业端平台运行时。
type Runtime struct {
	common.CandidateResumeTracker
	contactRows map[string]contract.FindAllItem // 当前任务上次完整核实过的联系人，按平台会话编号保存。
}

// NewRuntime 创建猎聘企业端平台运行时。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// PlatformID 返回猎聘企业端平台编号。
func (r *Runtime) PlatformID() string {
	return "liepin"
}
