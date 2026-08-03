//go:build windows

// Package process 文件作用：提供 Windows 进程树多级结束和退出确认能力。
package process

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const windowsTerminateWait = 1500 * time.Millisecond

type windowsTerminateAttempt struct {
	name string
	run  func() error
}

type windowsProcessExitWaiter func(pid int, timeout time.Duration) error

// TerminateTree 在 Windows 下依次使用 taskkill、Go 原生终止和 PowerShell 强制结束进程。
// pid 为已确认可以安全结束的目标进程 ID。
func TerminateTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	return runWindowsTerminateAttempts(pid, windowsTerminateAttempts(pid), waitWindowsProcessExit)
}

// windowsTerminateAttempts 创建 Windows 进程结束的多级尝试列表。
// pid 为目标进程 ID，尝试顺序从优先方案到兜底方案。
func windowsTerminateAttempts(pid int) []windowsTerminateAttempt {
	return []windowsTerminateAttempt{
		{
			name: "taskkill",
			run: func() error {
				cmd := hiddenCommand("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
				output, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("%w：%s", err, strings.TrimSpace(string(output)))
				}
				return nil
			},
		},
		{
			name: "go-process-kill",
			run: func() error {
				target, err := os.FindProcess(pid)
				if err != nil {
					return err
				}
				return target.Kill()
			},
		},
		{
			name: "powershell-stop-process",
			run: func() error {
				script := "Stop-Process -Id " + strconv.Itoa(pid) + " -Force -ErrorAction Stop"
				cmd := hiddenCommand("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-Command", script)
				output, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("%w：%s", err, strings.TrimSpace(string(output)))
				}
				return nil
			},
		},
	}
}

// runWindowsTerminateAttempts 执行多级结束方案，并在每次执行后确认进程已经退出。
// pid 为目标进程 ID，attempts 为尝试列表，waitExit 为退出确认方法。
func runWindowsTerminateAttempts(pid int, attempts []windowsTerminateAttempt, waitExit windowsProcessExitWaiter) error {
	var failures []string
	for _, attempt := range attempts {
		runErr := attempt.run()
		waitErr := waitExit(pid, windowsTerminateWait)
		if waitErr == nil {
			log.Printf("[进程清理] Windows 进程已结束 pid=%d method=%s", pid, attempt.name)
			return nil
		}
		failure := attempt.name + " 未结束进程"
		if runErr != nil {
			failure += "，执行错误=" + runErr.Error()
		}
		failure += "，确认错误=" + waitErr.Error()
		failures = append(failures, failure)
		log.Printf("[进程清理] Windows 结束进程失败 pid=%d method=%s run_err=%v wait_err=%v", pid, attempt.name, runErr, waitErr)
	}
	return fmt.Errorf("多种方式均未能结束 Windows 进程 PID=%d：%s", pid, strings.Join(failures, "；"))
}

// waitWindowsProcessExit 等待 Windows 进程句柄进入已退出状态。
// pid 为目标进程 ID，timeout 为最长等待时间。
func waitWindowsProcessExit(pid int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return fmt.Errorf("打开进程句柄失败：%w", err)
	}
	defer windows.CloseHandle(handle)
	milliseconds := uint32(timeout / time.Millisecond)
	result, err := windows.WaitForSingleObject(handle, milliseconds)
	if err != nil {
		return fmt.Errorf("等待进程退出失败：%w", err)
	}
	if result == windows.WAIT_OBJECT_0 {
		return nil
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("等待 %s 后进程仍在运行", timeout)
	}
	return fmt.Errorf("等待进程退出返回未知状态：%d", result)
}
