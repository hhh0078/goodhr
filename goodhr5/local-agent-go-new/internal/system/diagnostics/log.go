// Package diagnostics 文件作用：持久保存脱敏运行日志和 Worker 原始诊断，并按大小轮转限制磁盘占用。
package diagnostics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const maxLogBytes int64 = 10 << 20
const logBackups = 4

var sensitiveKey = regexp.MustCompile(`(?i)cookie|authorization|token|password|secret|api[_-]?key|proxy.*(user|pass)|message_text|input_text|resume_text|candidate_name|phone|wechat`)
var sensitiveText = regexp.MustCompile(`(?i)((?:token|password|secret|authorization|cookie|api[_-]?key)\s*[=:]\s*)([^\s&,;]+)`)
var urlQuery = regexp.MustCompile(`(https?://[^\s?"<>]+)\?[^\s"<>]*`)
var bearer = regexp.MustCompile(`(?i)Bearer\s+[^\s",;]+`)

// Writer 保存独立日志文件，所有写入先脱敏，超过上限自动轮转。
type Writer struct {
	mu    sync.Mutex
	path  string
	file  *os.File
	size  int64
	limit int64
}

// Open 创建日志目录并以追加方式打开日志，不覆盖先前故障证据。
func Open(directory, name string) (*Writer, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	w := &Writer{path: filepath.Join(directory, name), limit: maxLogBytes}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// open 打开当前日志并恢复已有文件大小；调用方负责串行访问。
func (w *Writer) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file, w.size = file, info.Size()
	return nil
}

// Write 脱敏后直接写入文件，使程序退出前的最后一个步骤也能保留下来。
func (w *Writer) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	safe := Normalize(data)
	if len(safe) > 64<<10 {
		safe = Normalize([]byte(fmt.Sprintf("日志过长已截断，原始字节数=%d", len(data))))
	}
	if w.size+int64(len(safe)) > w.limit {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	if w.file == nil {
		return 0, os.ErrClosed
	}
	n, err := w.file.Write(safe)
	w.size += int64(n)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// Normalize 把 Go 与 Worker 的输出统一成一行一个完整 JSON 日志对象。
func Normalize(data []byte) []byte {
	var item map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &item); err != nil || item == nil {
		message := strings.TrimSpace(string(data))
		isError := strings.Contains(strings.ToLower(message), "error") || strings.Contains(message, "失败") || strings.Contains(message, "panic")
		item = map[string]any{"message": message, "category": "agent", "is_error": isError}
		if isError {
			item["error_message"] = message
		}
	}
	if _, ok := item["timestamp"]; !ok {
		item["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if _, ok := item["category"]; !ok {
		category := "agent"
		if action, ok := item["action"].(string); ok && strings.TrimSpace(action) != "" {
			category = "browser"
		}
		item["category"] = category
	}
	if _, ok := item["is_error"]; !ok {
		level, _ := item["level"].(string)
		status, _ := item["status"].(string)
		item["is_error"] = strings.EqualFold(level, "error") || strings.EqualFold(status, "failed") || strings.EqualFold(status, "panic")
	}
	if _, ok := item["error_message"]; !ok {
		if message, ok := item["error"].(string); ok && item["is_error"] == true {
			item["error_message"] = message
		} else {
			item["error_message"] = ""
		}
	}
	if _, ok := item["input"]; !ok {
		item["input"] = map[string]any{}
	}
	if _, ok := item["output"]; !ok {
		item["output"] = map[string]any{}
	}
	safe, err := json.Marshal(redactValue(item))
	if err != nil {
		return []byte("{\"timestamp\":\"1970-01-01T00:00:00Z\",\"category\":\"agent\",\"is_error\":true,\"error_message\":\"日志编码失败\",\"input\":{},\"output\":{}}\n")
	}
	return append(safe, '\n')
}

// rotate 保留最近四份备份；重命名失败时重新打开当前文件，避免静默丢失后续日志。
func (w *Writer) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", w.path, logBackups))
	for index := logBackups - 1; index >= 0; index-- {
		source := w.path
		if index > 0 {
			source = fmt.Sprintf("%s.%d", w.path, index)
		}
		if err := os.Rename(source, fmt.Sprintf("%s.%d", w.path, index+1)); err != nil && !os.IsNotExist(err) {
			_ = w.open()
			return err
		}
	}
	return w.open()
}

// Close 关闭日志文件，允许重复清理。
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// Redact 对结构化字段和非结构化错误中的凭据及 URL 查询参数进行脱敏。
func Redact(data []byte) []byte {
	var value any
	if json.Unmarshal(data, &value) == nil {
		sanitized, err := json.Marshal(redactValue(value))
		if err == nil {
			return append(sanitized, '\n')
		}
	}
	safe := sensitiveText.ReplaceAll(data, []byte("${1}[已隐藏]"))
	safe = bearer.ReplaceAll(safe, []byte("Bearer [已隐藏]"))
	return urlQuery.ReplaceAll(safe, []byte("${1}?[已隐藏]"))
}

// redactValue 只在未知 JSON 日志边界递归清理敏感字段。
func redactValue(value any) any {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if sensitiveKey.MatchString(key) {
				item[key] = "[已隐藏]"
			} else {
				item[key] = redactValue(child)
			}
		}
	case []any:
		for index, child := range item {
			item[index] = redactValue(child)
		}
	case string:
		safe := sensitiveText.ReplaceAllString(item, "${1}[已隐藏]")
		safe = bearer.ReplaceAllString(safe, "Bearer [已隐藏]")
		return urlQuery.ReplaceAllString(safe, "${1}?[已隐藏]")
	}
	return value
}

// WorkerLine 将 Worker 输出统一成脱敏 JSON 行，并把异常 stderr 归入运行中的岗位。
func WorkerLine(writer io.Writer, next func([]byte)) func([]byte) {
	return func(line []byte) {
		data := bytes.TrimSpace(line)
		if len(data) == 0 {
			return
		}
		if !json.Valid(data) {
			message := strings.TrimSpace(string(Redact(data)))
			isError := strings.Contains(strings.ToLower(message), "error") || strings.Contains(strings.ToLower(message), "panic") || strings.Contains(message, "失败")
			status := "info"
			level := "info"
			if isError {
				status, level = "failed", "error"
			}
			errorMessage := ""
			if isError {
				errorMessage = message
			}
			data, _ = json.Marshal(map[string]any{
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "category": "worker",
				"action": "worker.stderr", "step": "process_output", "status": status, "level": level,
				"is_error": isError, "error_message": errorMessage, "message": message,
				"input": map[string]any{}, "output": map[string]any{},
			})
		}
		safe := Normalize(data)
		if _, err := writer.Write(safe); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "写入 Worker 日志失败：%v\n", err)
		}
		if next != nil {
			next(bytes.TrimSpace(safe))
		}
	}
}
