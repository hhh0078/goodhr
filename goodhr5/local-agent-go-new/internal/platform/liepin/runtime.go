// Package liepin 实现猎聘企业端平台的页面、候选人和消息能力。
package liepin

// Runtime 是猎聘企业端平台运行时。
type Runtime struct{}

// NewRuntime 创建猎聘企业端平台运行时。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// PlatformID 返回猎聘企业端平台编号。
func (r *Runtime) PlatformID() string {
	return "liepin"
}
