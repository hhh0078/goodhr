//go:build darwin

// Package power 提供 macOS 防睡眠实现。
package power

import (
	"os/exec"
	"sync"
)

type caffeinateInhibitor struct {
	cmd  *exec.Cmd
	once sync.Once
}

// PreventSleep 在 macOS 上启动 caffeinate，阻止任务运行时系统自动睡眠。
// reason 为调用方说明，当前实现不传给系统，仅用于保持接口语义。
func PreventSleep(reason string) (Inhibitor, error) {
	cmd := exec.Command("caffeinate", "-dimsu")
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &caffeinateInhibitor{cmd: cmd}, nil
}

// Stop 结束 caffeinate 进程，释放防睡眠状态。
func (i *caffeinateInhibitor) Stop() error {
	var err error
	i.once.Do(func() {
		if i.cmd == nil || i.cmd.Process == nil {
			return
		}
		err = i.cmd.Process.Kill()
		_ = i.cmd.Wait()
	})
	return err
}
