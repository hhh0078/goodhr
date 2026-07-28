// Package hliepin 实现猎聘猎头端平台的页面、候选人和消息能力。
package hliepin

// Runtime 是猎聘猎头端平台运行时，保存当前单任务的岗位和开聊方式。
type Runtime struct {
	positionName          string
	selectJobWhenGreeting bool
}

// NewRuntime 创建猎聘猎头端平台运行时。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// PlatformID 返回猎聘猎头端平台编号。
func (r *Runtime) PlatformID() string {
	return "hliepin"
}
