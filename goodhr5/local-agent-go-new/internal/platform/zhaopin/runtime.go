// Package zhaopin 实现智联招聘平台的页面、候选人和消息能力。
package zhaopin

// Runtime 是智联招聘平台运行时。
type Runtime struct{}

// NewRuntime 创建智联招聘平台运行时。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// PlatformID 返回智联招聘平台编号。
func (r *Runtime) PlatformID() string {
	return "zhaopin"
}
