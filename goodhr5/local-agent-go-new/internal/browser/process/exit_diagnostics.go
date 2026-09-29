// Package process 文件作用：保存 Worker 退出前的有限操作轨迹、原始错误和原生进程输出。
package process

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// processDiagnostics 保存单个进程的停止意图和退出证据，避免新旧 Worker 互相影响。
type processDiagnostics struct {
	startedAt    time.Time
	stopReason   string
	exitReported bool
	recent       []map[string]any
	lastError    map[string]any
	stderr       []string
	fatalOutput  string
}

// capture 保存有限的操作字段和原始 stderr，不把简历正文放进退出快照。
func (d *processDiagnostics) capture(line []byte) {
	var item map[string]any
	if json.Unmarshal(line, &item) != nil {
		text := string(line)
		lower := strings.ToLower(text)
		if d.fatalOutput == "" && (strings.Contains(lower, "fatal error") || strings.Contains(lower, "out of memory") || strings.Contains(lower, "assertion failed") || strings.HasPrefix(lower, "panic:")) {
			d.fatalOutput = limitedText(text, 2200)
		} else if d.fatalOutput != "" && len(d.fatalOutput) < 2200 {
			d.fatalOutput = limitedText(d.fatalOutput+"\n"+text, 2200)
		}
		d.stderr = append(d.stderr, limitedText(string(line), 400))
		if len(d.stderr) > 8 {
			d.stderr = d.stderr[len(d.stderr)-8:]
		}
		return
	}
	action, _ := item["action"].(string)
	if action == "worker.exit" && item["step"] == "exit" {
		d.exitReported = true
	}
	entry := map[string]any{}
	for _, key := range []string{"timestamp", "action", "step", "status", "trace_id", "request_id", "duration_ms", "error_code", "error_message", "original_error"} {
		if value, ok := item[key]; ok {
			if text, ok := value.(string); ok {
				value = limitedText(text, 240)
			}
			entry[key] = value
		}
	}
	if item["is_error"] == true || item["level"] == "error" {
		d.lastError = map[string]any{}
		for key, value := range entry {
			d.lastError[key] = value
		}
		if stack, ok := item["stack"].(string); ok {
			d.lastError["stack"] = limitedText(stack, 2200)
		}
	}
	if item["step"] != "http" && !strings.HasPrefix(action, "http.") && !strings.HasPrefix(action, "worker.exit") && !strings.HasPrefix(action, "worker.crash") && action != "health" && action != "runtime.status" && action != "browser.status" && action != "downloads.status" && action != "downloads.list" {
		d.recent = append(d.recent, entry)
		if len(d.recent) > 12 {
			d.recent = d.recent[len(d.recent)-12:]
		}
	}
}

// MarkStopping 在结束子进程前保存本次停止的原因。
func (w *lineSinkWriter) MarkStopping(reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.diagnostics.stopReason == "" {
		w.diagnostics.stopReason = reason
	}
}

// ExitEvent 返回尚未被 Node 记录的进程退出事件，包含真实退出码和有限诊断。
func (w *lineSinkWriter) ExitEvent(pid, exitCode int, waitErr error, state string) []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	d := &w.diagnostics
	if d.exitReported {
		return nil
	}
	expected := d.stopReason != ""
	abnormal := !expected
	category, status, level := "运行", "finished", "info"
	reason := d.stopReason
	if abnormal {
		category, status, level = "异常退出", "failed", "error"
		reason = fmt.Sprintf("Browser Worker 意外退出，退出码 %d", exitCode)
	}
	waitError := ""
	if waitErr != nil {
		waitError = waitErr.Error()
	}
	if abnormal && waitError != "" {
		reason += "：" + waitError
	}
	errorMessage := ""
	if abnormal {
		errorMessage = reason
	}
	data, _ := json.Marshal(map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "category": category,
		"action": "worker.exit", "step": "process_exit", "status": status, "level": level,
		"is_error": abnormal, "error_message": errorMessage, "exit_reason": reason, "exit_code": exitCode,
		"expected_stop": expected, "input": map[string]any{"pid": pid},
		"output": map[string]any{"exit_code": exitCode, "exit_code_hex": fmt.Sprintf("0x%08X", uint32(exitCode)), "wait_error": waitError, "process_state": state},
		"diagnostics": map[string]any{"pid": pid, "platform": runtime.GOOS, "arch": runtime.GOARCH,
			"started_at": d.startedAt.UTC().Format(time.RFC3339Nano), "uptime_ms": time.Since(d.startedAt).Milliseconds(),
			"stop_reason": d.stopReason, "last_error": d.lastError, "raw_fatal_error": d.fatalOutput, "recent_operations": d.recent, "stderr_tail": d.stderr},
	})
	return data
}

// limitedText 按字节限制诊断文本，并避免在 UTF-8 字符中间截断。
func limitedText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	return strings.ToValidUTF8(value, "") + "…"
}
