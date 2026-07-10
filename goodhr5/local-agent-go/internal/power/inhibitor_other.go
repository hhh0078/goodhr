//go:build !darwin && !windows

// Package power 提供其他系统的防睡眠空实现。
package power

// PreventSleep 在暂未适配的平台返回空句柄。
// reason 为调用方说明，空实现不会使用。
func PreventSleep(reason string) (Inhibitor, error) {
	return noopInhibitor{}, nil
}
