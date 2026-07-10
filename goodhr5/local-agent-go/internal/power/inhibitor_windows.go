//go:build windows

// Package power 提供 Windows 防睡眠实现。
package power

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
)

const (
	esSystemRequired   = 0x00000001
	esDisplayRequired  = 0x00000002
	esAwayModeRequired = 0x00000040
	esContinuous       = 0x80000000
)

type windowsInhibitor struct {
	done chan struct{}
	once sync.Once
}

// PreventSleep 在 Windows 上设置线程执行状态，阻止系统自动睡眠。
// reason 为调用方说明，当前 Windows API 不展示该说明。
func PreventSleep(reason string) (Inhibitor, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("SetThreadExecutionState")
	done := make(chan struct{})
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		flags := uintptr(esContinuous | esSystemRequired | esDisplayRequired | esAwayModeRequired)
		ret, _, err := proc.Call(flags)
		if ret == 0 {
			ready <- fmt.Errorf("SetThreadExecutionState 失败：%w", err)
			return
		}
		ready <- nil
		<-done
		proc.Call(uintptr(esContinuous))
	}()
	if err := <-ready; err != nil {
		close(done)
		return nil, err
	}
	return &windowsInhibitor{done: done}, nil
}

// Stop 释放 Windows 防睡眠状态。
func (i *windowsInhibitor) Stop() error {
	i.once.Do(func() {
		close(i.done)
	})
	return nil
}
