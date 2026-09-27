// Package diagnostics 文件作用：用磁盘标记识别上次未正常退出，并提取 Go 致命崩溃原因。
package diagnostics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PreviousRun 保存上一次未正常退出的进程及可读崩溃原因。
type PreviousRun struct {
	PID         int    `json:"pid"`
	StartedAt   string `json:"started_at"`
	CrashOffset int64  `json:"crash_offset"`
	Reason      string `json:"-"`
}

// RunMarker 是程序正常收尾时需移除的运行标记。
type RunMarker struct{ path string }

// MarkRun 标记本次运行，并在标记仍存在时读取上次异常退出原因。
func MarkRun(logsDir, crashPath string, crashOffset int64) (*RunMarker, *PreviousRun, error) {
	path := filepath.Join(logsDir, "agent-running.json")
	var previous *PreviousRun
	if content, err := os.ReadFile(path); err == nil {
		previous = &PreviousRun{Reason: "上次异常退出，具体原因未知（可能是强制结束、断电或系统故障）"}
		_ = json.Unmarshal(content, previous)
		if previous.CrashOffset >= 0 {
			if crash, readErr := os.Open(crashPath); readErr == nil {
				if info, statErr := crash.Stat(); statErr == nil && info.Size() > previous.CrashOffset {
					start := previous.CrashOffset
					if info.Size()-start > 16<<10 {
						start = info.Size() - (16 << 10)
					}
					buffer := make([]byte, info.Size()-start)
					if n, readErr := crash.ReadAt(buffer, start); readErr == nil || n > 0 {
						previous.Reason = "Go 致命崩溃：" + crashReason(string(buffer[:n]))
					}
				}
				_ = crash.Close()
			}
		}
	}
	current := PreviousRun{PID: os.Getpid(), StartedAt: time.Now().UTC().Format(time.RFC3339Nano), CrashOffset: crashOffset}
	content, err := json.Marshal(current)
	if err != nil {
		return nil, previous, err
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		return nil, previous, fmt.Errorf("创建运行标记失败：%w", err)
	}
	return &RunMarker{path: path}, previous, nil
}

// crashReason 只提取崩溃摘要，避免把整段调用栈和业务数据传到岗位分享链接。
func crashReason(content string) string {
	first := "致命退出，详细调用栈保留在本机应急文件"
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if first == "致命退出，详细调用栈保留在本机应急文件" {
			first = line
		}
		if strings.Contains(line, "fatal error:") || strings.HasPrefix(line, "panic:") {
			first = line
			break
		}
	}
	if len(first) > 500 {
		first = string([]rune(first)[:min(160, len([]rune(first)))]) + "…"
	}
	return first
}

// Clear 在程序正常返回并写完退出日志后清除运行标记。
func (m *RunMarker) Clear() error {
	if m == nil {
		return nil
	}
	err := os.Remove(m.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
