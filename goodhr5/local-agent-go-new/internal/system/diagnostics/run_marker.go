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
	PID              int              `json:"pid"`
	StartedAt        string           `json:"started_at"`
	CrashOffset      int64            `json:"crash_offset"`
	Reason           string           `json:"-"`
	CrashStack       string           `json:"-"`
	RecentOperations []map[string]any `json:"-"`
	LastError        map[string]any   `json:"-"`
}

// RunMarker 是程序正常收尾时需移除的运行标记。
type RunMarker struct{ path string }

// MarkRun 标记本次运行，并在标记仍存在时读取上次异常退出原因。
func MarkRun(logsDir, crashPath string, crashOffset int64) (*RunMarker, *PreviousRun, error) {
	path := filepath.Join(logsDir, "agent-running.json")
	previous := ReadPreviousRun(logsDir, crashPath)
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

// ReadPreviousRun 在不改动磁盘标记的前提下，读取上次闪退的可见原因。
func ReadPreviousRun(logsDir, crashPath string) *PreviousRun {
	path := filepath.Join(logsDir, "agent-running.json")
	var previous *PreviousRun
	if content, err := os.ReadFile(path); err == nil {
		previous = &PreviousRun{Reason: "上次异常退出，具体原因未知（可能是强制结束、断电或系统故障）"}
		_ = json.Unmarshal(content, previous)
		if previous.CrashOffset >= 0 {
			if crash, readErr := os.Open(crashPath); readErr == nil {
				if info, statErr := crash.Stat(); statErr == nil && info.Size() > previous.CrashOffset {
					start := previous.CrashOffset
					// 保留当前崩溃的开头；文件尾部可能只有其他 goroutine 的调用栈。
					buffer := make([]byte, min(info.Size()-start, 8<<10))
					if n, readErr := crash.ReadAt(buffer, start); readErr == nil || n > 0 {
						previous.Reason = "Go 致命崩溃：" + crashReason(string(buffer[:n]))
						previous.CrashStack = diagnosticText(string(Redact(buffer[:n])), 6000)
					}
				}
				_ = crash.Close()
			}
		}
		previous.readRecentEvidence(logsDir)
	}
	return previous
}

// ResetLegacyLogs 首次升级时清除旧 JSONL、轮转备份与崩溃日志，后续运行不再重复清理。
func ResetLegacyLogs(logsDir string) (bool, error) {
	marker := filepath.Join(logsDir, "readable-logs-v2.ready")
	if _, err := os.Stat(marker); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	files := []string{"goodhr.jsonl", "crash.log", "agent-running.json"}
	for index := 1; index <= logBackups; index++ {
		files = append(files, fmt.Sprintf("goodhr.jsonl.%d", index))
	}
	for _, name := range files {
		if err := os.Remove(filepath.Join(logsDir, name)); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("清理旧日志 %s 失败：%w", name, err)
		}
	}
	if err := os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0600); err != nil {
		return false, fmt.Errorf("记录日志格式升级失败：%w", err)
	}
	return true, nil
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
