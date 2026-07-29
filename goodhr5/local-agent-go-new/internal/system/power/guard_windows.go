//go:build windows

// Package power 文件作用：使用 Windows 执行状态 API 为任务提供防睡眠保护。
package power

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
)

const (
	executionSystemRequired  = 0x00000001
	executionDisplayRequired = 0x00000002
	executionContinuous      = 0x80000000
)

// Guard 管理当前 Windows 防睡眠执行状态。
type Guard struct {
	mu   sync.Mutex
	done chan struct{}
}

// Available 检查 Windows 防睡眠 API 是否可用。
func (g *Guard) Available() error {
	procedure := syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")
	if err := procedure.Find(); err != nil {
		return fmt.Errorf("系统防睡眠能力不可用：%w", err)
	}
	return nil
}

// Start 在固定系统线程上启用 Windows 防睡眠状态，已经启用时直接复用。
func (g *Guard) Start() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done != nil {
		return nil
	}
	done := make(chan struct{})
	ready := make(chan error, 1)
	go runWindowsPowerGuard(done, ready)
	if err := <-ready; err != nil {
		close(done)
		return err
	}
	g.done = done
	return nil
}

// Stop 释放 Windows 防睡眠状态。
func (g *Guard) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done != nil {
		close(g.done)
		g.done = nil
	}
}

// runWindowsPowerGuard 在同一个系统线程上设置并释放 Windows 执行状态。
func runWindowsPowerGuard(done <-chan struct{}, ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procedure := syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")
	flags := uintptr(executionContinuous | executionSystemRequired | executionDisplayRequired)
	result, _, callErr := procedure.Call(flags)
	if result == 0 {
		ready <- fmt.Errorf("启用 Windows 防睡眠失败：%w", callErr)
		return
	}
	ready <- nil
	<-done
	_, _, _ = procedure.Call(uintptr(executionContinuous))
}
