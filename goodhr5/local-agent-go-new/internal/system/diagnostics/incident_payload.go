// Package diagnostics 文件作用：规范新旧退出事件并约束上传大小，防止单条日志阻断离线补传。
package diagnostics

import (
	"encoding/json"
	"strings"
)

// normalizeIncident 修正旧 Worker 的错误类别，并始终保留可上传的退出原因。
func normalizeIncident(line []byte) []byte {
	var item map[string]any
	if json.Unmarshal(Normalize(line), &item) != nil {
		return line
	}
	switch item["action"] {
	case "worker.crash", "browser.crash", "browser.exit", "agent.previous_exit":
		item["category"], item["is_error"] = "异常退出", true
	case "worker.exit":
		if item["is_error"] == true && item["expected_stop"] != true {
			item["category"] = "异常退出"
		}
	}
	data, _ := json.Marshal(item)
	if len(data) < 15_000 {
		return data
	}
	compact := map[string]any{"input": map[string]any{}, "output": map[string]any{}, "diagnostic_truncated": true}
	for _, key := range []string{"timestamp", "category", "action", "step", "status", "is_error", "error_message", "error", "original_error", "exit_reason", "message", "exit_code", "signal", "pid", "trace_id", "request_id", "expected_stop"} {
		if value, ok := item[key]; ok {
			if text, ok := value.(string); ok {
				value = diagnosticText(text, 700)
			}
			compact[key] = value
		}
	}
	stack, _ := item["stack"].(string)
	if diagnostics, ok := item["diagnostics"].(map[string]any); ok {
		if fatal, ok := diagnostics["raw_fatal_error"].(string); ok && stack == "" {
			stack = fatal
		}
		if previousStack, ok := diagnostics["crash_stack"].(string); ok && stack == "" {
			stack = previousStack
		}
		if lastError, ok := diagnostics["last_error"].(map[string]any); ok && stack == "" {
			stack, _ = lastError["stack"].(string)
		}
	}
	compact["stack"] = diagnosticText(stack, 3000)
	data, _ = json.Marshal(compact)
	return data
}

// diagnosticText 按 UTF-8 字节限制诊断字段，保留原始错误开头。
func diagnosticText(text string, maximum int) string {
	if len(text) <= maximum {
		return text
	}
	return strings.ToValidUTF8(text[:maximum], "") + "…"
}
