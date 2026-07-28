// Package boss 实现 Boss 直聘平台的页面、候选人和消息能力。
package boss

// Runtime 是 Boss 直聘平台运行时。
type Runtime struct{}

// NewRuntime 创建 Boss 直聘平台运行时。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// PlatformID 返回 Boss 平台编号。
func (r *Runtime) PlatformID() string {
	return "boss"
}
