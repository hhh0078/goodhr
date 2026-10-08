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

var sensitiveKey = regexp.MustCompile(`(?i)cookie|authorization|token|password|secret|api[_-]?key|proxy.*(user|pass)|message[_-]?text|input[_-]?text|resume[_-]?text|candidate[_-]?name|phone|wechat|email|id[_-]?card|address`)
var sensitiveText = regexp.MustCompile(`(?i)((?:token|password|secret|authorization|cookie|api[_-]?key)\s*[=:]\s*)([^\s&,;]+)`)
var urlQuery = regexp.MustCompile(`(https?://[^\s?"<>]+)\?[^\s"<>]*`)
var bearer = regexp.MustCompile(`(?i)Bearer\s+[^\s",;]+`)
var phoneText = regexp.MustCompile(`\b1[3-9][0-9]{9}\b`)
var emailText = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
var credentialKey = regexp.MustCompile(`(?i)cookie|authorization|token|password|secret|api[_-]?key|proxy.*(user|pass)`)
var credentialJSONText = regexp.MustCompile(`(?i)("(?:cookie|authorization|token|password|secret|api[_-]?key)"\s*:\s*")[^"\r\n]*(")`)
var browserRuntimeStack = regexp.MustCompile(`chrome://juggler/content/(?:content/)?Runtime\.js(?:[:\s]|$)`)

const rustSearchSource = "moz-src:///toolkit/components/uniffi-bindgen-gecko-js/components/generated/RustSearch.sys.mjs"
const searchRecordTypePattern = `JSON error: missing field \x60recordType\x60 at line [0-9]+ column [0-9]+`

var browserStartupOutput = regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z\s+)?pw:browser \[pid=[0-9]+\]\[(out|err)\]\s*(.*)$`)
var browserSearchInitOutput = regexp.MustCompile(`^console\.error: SearchService: "#init: failure initializing search:" \(new Error\("reason: ` + searchRecordTypePattern + `", "` + regexp.QuoteMeta(rustSearchSource) + `", [0-9]+\)\)$`)
var browserRustSearchOutput = regexp.MustCompile(`^JavaScript error: ` + regexp.QuoteMeta(rustSearchSource) + `, line [0-9]+: Error: reason: ` + searchRecordTypePattern + `$`)
var browserSearchSettingsOutput = regexp.MustCompile(`^console\.error: SearchSettings: "_write: Could not write to settings file:" \(new Error\("cannot write without any engine\.", "moz-src:///toolkit/components/search/SearchSettings\.sys\.mjs", [0-9]+\)\)$`)
var browserProgressListenerOutput = regexp.MustCompile(`^JavaScript error: chrome://juggler/content/Helper\.js, line [0-9]+: NS_ERROR_FAILURE: Component returned failure code: 0x80004005 \(NS_ERROR_FAILURE\) \[nsIWebProgress\.removeProgressListener\]$`)
var browserRecordTypeContinuation = regexp.MustCompile(`^- (?:message = reason: |reason = )` + searchRecordTypePattern + `$`)

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
	if len(safe) > 64<<10 && !hasAIResponseDiagnostic(safe) {
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

// hasAIResponseDiagnostic 只让明确的 AI 失败原文绕过普通单行日志限额，轮转规则继续生效。
func hasAIResponseDiagnostic(data []byte) bool {
	var event struct {
		Output struct {
			AIResponseDiagnostic *struct {
				Response string `json:"response"`
			} `json:"ai_response_diagnostic"`
		} `json:"output"`
	}
	return json.Unmarshal(data, &event) == nil && event.Output.AIResponseDiagnostic != nil
}

// Normalize 把 Go 与 Worker 的输出统一成一行一个完整 JSON 日志对象。
func Normalize(data []byte) []byte {
	var item map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &item); err != nil || item == nil {
		message := strings.TrimSpace(string(data))
		lower := strings.ReplaceAll(strings.ToLower(message), `error=""`, "")
		isError := strings.Contains(lower, "error") || strings.Contains(message, "失败") || strings.Contains(lower, "panic") || strings.Contains(lower, "status=failed")
		item = map[string]any{"message": message, "category": "agent", "is_error": isError}
		if isError {
			item["error_message"] = message
		}
	}
	if _, ok := item["timestamp"]; !ok {
		item["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if _, ok := item["is_error"]; !ok {
		level, _ := item["level"].(string)
		status, _ := item["status"].(string)
		item["is_error"] = strings.EqualFold(level, "error") || strings.EqualFold(status, "failed") || strings.EqualFold(status, "panic")
	}
	if item["category"] == "异常退出" {
		item["is_error"] = true
	} else if item["is_error"] == true {
		item["category"] = "错误"
	} else {
		item["category"] = "运行"
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
	safe = urlQuery.ReplaceAll(safe, []byte("${1}?[已隐藏]"))
	safe = phoneText.ReplaceAll(safe, []byte("[手机号已隐藏]"))
	return emailText.ReplaceAll(safe, []byte("[邮箱已隐藏]"))
}

// redactValue 只在未知 JSON 日志边界递归清理敏感字段。
func redactValue(value any) any {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if key == "ai_response_diagnostic" {
				item[key] = redactAIResponseDiagnostic(child)
			} else if sensitiveKey.MatchString(key) {
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
		safe = urlQuery.ReplaceAllString(safe, "${1}?[已隐藏]")
		safe = phoneText.ReplaceAllString(safe, "[手机号已隐藏]")
		return emailText.ReplaceAllString(safe, "[邮箱已隐藏]")
	}
	return value
}

// redactAIResponseDiagnostic 完整保留经用户授权的 AI 失败原文，只隐藏访问凭据和 URL 私密参数。
func redactAIResponseDiagnostic(value any) any {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if credentialKey.MatchString(key) {
				item[key] = "[已隐藏]"
			} else {
				item[key] = redactAIResponseDiagnostic(child)
			}
		}
	case []any:
		for index, child := range item {
			item[index] = redactAIResponseDiagnostic(child)
		}
	case string:
		safe := credentialJSONText.ReplaceAllString(item, "${1}[已隐藏]${2}")
		safe = sensitiveText.ReplaceAllString(safe, "${1}[已隐藏]")
		safe = bearer.ReplaceAllString(safe, "Bearer [已隐藏]")
		return urlQuery.ReplaceAllString(safe, "${1}?[已隐藏]")
	}
	return value
}

// WorkerLine 将 Worker 输出统一成脱敏 JSON 行，并把异常 stderr 归入运行中的岗位。
func WorkerLine(writer io.Writer, next func([]byte)) func([]byte) {
	nativeTrace := &browserProcessTrace{}
	return func(line []byte) {
		data := bytes.TrimSpace(line)
		if len(data) == 0 {
			return
		}
		if !json.Valid(data) {
			if native := nativeTrace.observe(string(data)); len(native) > 0 {
				data = native
			}
		}
		if !json.Valid(data) {
			message := strings.TrimSpace(string(Redact(data)))
			isError := hasFatalBrowserOutput(message) || strings.Contains(strings.ToLower(message), "error") || strings.Contains(message, "失败")
			status := "info"
			level := "info"
			if isError {
				status, level = "failed", "error"
			}
			output := map[string]any{}
			stderrKind := ""
			if isBrowserInternalStaleObject(message) {
				stderrKind = "browser_internal_stale_object"
			} else if family := browserStartupOutputFamily(message); family != "" {
				stderrKind = "browser_internal_startup_output"
				output["startup_family"] = family
			}
			if stderrKind != "" {
				status, level, isError = "warning", "warn", false
				output["stderr_kind"] = stderrKind
				output["operation_outcome"] = "unknown"
			}
			errorMessage := ""
			if isError {
				errorMessage = message
			}
			data, _ = json.Marshal(map[string]any{
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "category": "worker",
				"action": "worker.stderr", "step": "process_output", "status": status, "level": level,
				"is_error": isError, "error_message": errorMessage, "message": message,
				"input": map[string]any{}, "output": output,
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

// isBrowserInternalStaleObject 仅识别 Firefox 调试输出中的 Juggler 对象失效，不推断操作已经恢复。
func isBrowserInternalStaleObject(message string) bool {
	if !strings.Contains(message, "pw:browser") || !strings.Contains(message, "Cannot find object with id") {
		return false
	}
	if hasFatalBrowserOutput(message) {
		return false
	}
	contentChannel := strings.Contains(message, "error in channel content::") && strings.Contains(message, "exception callFunction namespace page Cannot find object with id")
	return browserRuntimeStack.MatchString(message) || contentChannel
}

// browserStartupOutputFamily 仅分类已经核实的 Firefox 内部输出签名，不把空模块标题或内置搜索失败当作岗位动作失败。
func browserStartupOutputFamily(message string) string {
	if hasFatalBrowserOutput(message) {
		return ""
	}
	match := browserStartupOutput.FindStringSubmatch(message)
	if match == nil {
		return ""
	}
	stream, payload := match[1], strings.TrimSpace(match[2])
	if stream == "out" {
		switch payload {
		case `console.error: "Warning: unrecognized command line flag" "-foreground"`:
			return "foreground_flag"
		case "console.error: CustomizableUI:":
			return "customizable_ui_header"
		case "console.error: WebExtensions:":
			return "webextensions_header"
		case `console.error: "Could not parse ASRouter preference. Try resetting browser.newtabpage.activity-stream.asrouter.providers.snippets in about:config."`:
			return "asrouter_snippets_preference"
		case `console.error: "Search service failed to init"`:
			return "search_service_init"
		}
		if browserSearchInitOutput.MatchString(payload) {
			return "search_init_record_type"
		}
		if browserSearchSettingsOutput.MatchString(payload) {
			return "search_settings_no_engines"
		}
		if browserRecordTypeContinuation.MatchString(payload) {
			return "search_record_type_continuation"
		}
	}
	if stream == "err" {
		if browserRustSearchOutput.MatchString(payload) {
			return "rust_search_record_type"
		}
		if browserProgressListenerOutput.MatchString(payload) {
			return "juggler_progress_listener"
		}
	}
	return ""
}

// hasFatalBrowserOutput 保留 panic、内存耗尽、断言和致命错误，已知内部输出不得覆盖这些故障证据。
func hasFatalBrowserOutput(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "panic") || strings.Contains(lower, "fatal error") || strings.Contains(lower, "out of memory") || strings.Contains(lower, "assertion failed")
}
