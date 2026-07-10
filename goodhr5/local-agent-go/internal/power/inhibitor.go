// Package power 提供跨平台的运行期防睡眠能力。
package power

// Inhibitor 表示一个可释放的防睡眠句柄。
type Inhibitor interface {
	Stop() error
}

type noopInhibitor struct{}

// Stop 释放空防睡眠句柄。
func (noopInhibitor) Stop() error {
	return nil
}
