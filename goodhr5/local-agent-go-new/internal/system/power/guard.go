// Package power 使用 macOS caffeinate 为运行中的招聘任务提供防睡眠保护。
package power

import (
	"fmt"
	"os/exec"
	"sync"
)

// Guard 管理当前防睡眠子进程。
type Guard struct {
	mu      sync.Mutex
	command *exec.Cmd
}

// Available 检查 macOS caffeinate 是否可用。
func (g *Guard) Available() error {
	if _, err := exec.LookPath("caffeinate"); err != nil {
		return fmt.Errorf("系统防睡眠工具不可用：%w", err)
	}
	return nil
}

// Start 启动防睡眠保护，已启动时直接复用。
func (g *Guard) Start() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.command != nil && g.command.Process != nil {
		return nil
	}
	command := exec.Command("caffeinate", "-dimsu")
	if err := command.Start(); err != nil {
		return fmt.Errorf("启动防睡眠失败：%w", err)
	}
	g.command = command
	return nil
}

// Stop 停止防睡眠保护。
func (g *Guard) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.command != nil && g.command.Process != nil {
		_ = g.command.Process.Kill()
		_, _ = g.command.Process.Wait()
	}
	g.command = nil
}
