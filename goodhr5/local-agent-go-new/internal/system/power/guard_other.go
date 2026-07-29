//go:build !darwin && !windows

// Package power 文件作用：为暂未支持的系统提供明确的防睡眠错误。
package power

import "fmt"

// Guard 表示当前系统暂无可用防睡眠实现。
type Guard struct{}

// Available 返回当前系统不支持防睡眠的说明。
func (g *Guard) Available() error {
	return fmt.Errorf("当前系统暂不支持任务防睡眠")
}

// Start 返回当前系统不支持防睡眠的说明。
func (g *Guard) Start() error {
	return g.Available()
}

// Stop 在当前系统没有需要释放的防睡眠状态。
func (g *Guard) Stop() {}
