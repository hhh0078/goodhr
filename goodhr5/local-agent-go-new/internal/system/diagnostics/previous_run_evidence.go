// Package diagnostics 文件作用：恢复上次未正常退出前的脱敏操作和错误，让下次启动能补传崩溃证据。
package diagnostics

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Diagnostics 返回上次运行的有限诊断，强制结束时明确保留未知原因。
func (p *PreviousRun) Diagnostics() map[string]any {
	if p == nil {
		return nil
	}
	return map[string]any{"previous_pid": p.PID, "previous_started_at": p.StartedAt,
		"platform": runtime.GOOS, "arch": runtime.GOARCH, "crash_stack": p.CrashStack,
		"recent_operations": p.RecentOperations, "last_error": p.LastError}
}

// readRecentEvidence 只读取旧日志末尾，提取最后错误与主程序 recover 保存的调用栈。
func (p *PreviousRun) readRecentEvidence(logsDir string) {
	file, err := os.Open(filepath.Join(logsDir, "goodhr.jsonl"))
	if err != nil {
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return
	}
	start := max(int64(0), info.Size()-(64<<10))
	buffer := make([]byte, info.Size()-start)
	n, _ := file.ReadAt(buffer, start)
	lines := bytes.Split(buffer[:n], []byte("\n"))
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	startedAt, _ := time.Parse(time.RFC3339Nano, p.StartedAt)
	for _, line := range lines {
		var item map[string]any
		if json.Unmarshal(Redact(line), &item) != nil {
			continue
		}
		if timestamp, ok := item["timestamp"].(string); ok && !startedAt.IsZero() {
			at, err := time.Parse(time.RFC3339Nano, timestamp)
			if err == nil && at.Before(startedAt) {
				continue
			}
		}
		entry := map[string]any{}
		for _, key := range []string{"timestamp", "action", "step", "status", "trace_id", "request_id", "error_code", "error_message"} {
			if value, ok := item[key]; ok {
				if text, ok := value.(string); ok {
					value = diagnosticText(text, 200)
				}
				entry[key] = value
			}
		}
		output, _ := item["output"].(map[string]any)
		if event, ok := output["event"].(string); ok {
			entry["event"] = event
			if item["is_error"] == true && (event == "agent_panic" || event == "agent_exit") && p.CrashStack == "" {
				if reason, ok := output["exit_reason"].(string); ok {
					p.Reason = diagnosticText(reason, 700)
				}
				if stack, ok := output["stack"].(string); ok {
					p.CrashStack = diagnosticText(stack, 6000)
				}
			}
		}
		if item["is_error"] == true {
			p.LastError = entry
		}
		action, _ := entry["action"].(string)
		if action == "" && entry["event"] == nil {
			continue
		}
		if strings.HasPrefix(action, "http.") || action == "health" || action == "downloads.status" {
			continue
		}
		p.RecentOperations = append(p.RecentOperations, entry)
		if len(p.RecentOperations) > 8 {
			p.RecentOperations = p.RecentOperations[1:]
		}
	}
}
