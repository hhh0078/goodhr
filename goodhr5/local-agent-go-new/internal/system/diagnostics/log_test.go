// 文件作用说明：验证日志持久化、凭据脱敏与轮转，确保异常退出证据可供排查。
package diagnostics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLogRedactionAndRotation 验证敏感字段不落盘且轮转后仍能保存新日志。
func TestLogRedactionAndRotation(t *testing.T) {
	directory := t.TempDir()
	writer, err := Open(directory, "worker.log")
	if err != nil {
		t.Fatal(err)
	}
	writer.limit = 300
	line := []byte(`{"token":"private-value","api_key":"api-secret-value","nested":{"password":"secret-value","candidateName":"张三"},"url":"https://example.test/a?access=hidden-value","error":"Bearer raw-value 13800138000 candidate@example.com"}`)
	if _, err = writer.Write(line); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(directory, "worker.log"))
	for _, secret := range []string{"private-value", "secret-value", "hidden-value", "raw-value", "api-secret-value", "13800138000", "candidate@example.com", "张三"} {
		if strings.Contains(string(first), secret) {
			t.Fatalf("敏感内容未脱敏：%s", secret)
		}
	}
	if _, err = writer.Write([]byte(strings.Repeat("x", 200))); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	if _, err = os.Stat(filepath.Join(directory, "worker.log.1")); err != nil {
		t.Fatal(err)
	}
}

// TestAIResponseLogExemptsLineLimitAndRotates 验证完整 AI 失败原文真实写入 agent.log，普通超长日志仍限制。
func TestAIResponseLogExemptsLineLimitAndRotates(t *testing.T) {
	writer, err := Open(t.TempDir(), "agent.log")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.limit = 1024
	response := strings.Repeat("完整 AI 返回\n", 10000) + `末尾证据 {"api_key":"private-key"}`
	line, _ := json.Marshal(map[string]any{"is_error": true, "output": map[string]any{"ai_response_diagnostic": map[string]any{"response": response, "model": "test-model"}}})
	if _, err := writer.Write(line); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("下一条日志")); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(writer.path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		Output struct {
			AI struct {
				Response string `json:"response"`
			} `json:"ai_response_diagnostic"`
		} `json:"output"`
	}
	if err := json.Unmarshal(saved, &event); err != nil {
		t.Fatal(err)
	}
	expected := strings.Replace(response, "private-key", "[已隐藏]", 1)
	if event.Output.AI.Response != expected {
		t.Fatal("agent.log 的完整 AI 返回被单行限额、轮转或脱敏截断")
	}
	if _, err := writer.Write([]byte(strings.Repeat("x", 100000))); err != nil {
		t.Fatal(err)
	}
	saved, err = os.ReadFile(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "日志过长已截断") {
		t.Fatal("普通超长日志没有维持限制")
	}
}

// TestLogAlwaysWritesJSONLines 验证普通 Go 输出与 Worker 输出都满足一行一个完整 JSON 对象。
func TestLogAlwaysWritesJSONLines(t *testing.T) {
	writer, err := Open(t.TempDir(), "goodhr.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("agent started")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(`{"action":"element.find","status":"success"}`)); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		var item map[string]any
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatalf("invalid JSON line: %v", err)
		}
		for _, key := range []string{"timestamp", "category", "is_error", "error_message", "input", "output"} {
			if _, ok := item[key]; !ok {
				t.Fatalf("missing %s: %s", key, line)
			}
		}
		if item["category"] != "运行" {
			t.Fatalf("普通记录类别应为运行：%s", line)
		}
	}
	_ = writer.Close()
}

// TestNormalizeDistinguishesEmptyError 验证正常步骤的空 error 字段不会被误判为错误。
func TestNormalizeDistinguishesEmptyError(t *testing.T) {
	for _, sample := range []struct {
		line    string
		isError bool
	}{
		{`task_id=task flow=greeting step=open_detail status=success duration_ms=35 error=""`, false},
		{`task_id=task flow=greeting step=open_detail status=failed duration_ms=35 error="按钮没找到"`, true},
	} {
		var item map[string]any
		if err := json.Unmarshal(Normalize([]byte(sample.line)), &item); err != nil {
			t.Fatal(err)
		}
		if item["is_error"] != sample.isError {
			t.Fatalf("日志错误分类不正确：%v", item)
		}
	}
}

// TestNormalizeKeepsAbnormalExit 验证异常退出不会被通用错误分类覆盖。
func TestNormalizeKeepsAbnormalExit(t *testing.T) {
	var item map[string]any
	if err := json.Unmarshal(Normalize([]byte(`{"action":"browser.exit","category":"异常退出","is_error":true,"error_message":"浏览器连接断开"}`)), &item); err != nil {
		t.Fatal(err)
	}
	if item["category"] != "异常退出" || item["is_error"] != true {
		t.Fatalf("异常退出分类丢失：%v", item)
	}
}

// TestRunMarkerDetectsAbnormalExit 验证上次意外退出可提取致命崩溃原因。
func TestRunMarkerDetectsAbnormalExit(t *testing.T) {
	directory := t.TempDir()
	crashPath := filepath.Join(directory, "crash.log")
	if err := os.WriteFile(crashPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := MarkRun(directory, crashPath, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crashPath, []byte("fatal error: test crash\n"), 0600); err != nil {
		t.Fatal(err)
	}
	marker, previous, err := MarkRun(directory, crashPath, 24)
	if err != nil || previous == nil || !strings.Contains(previous.Reason, "fatal error: test crash") {
		t.Fatalf("previous=%+v err=%v", previous, err)
	}
	_ = marker.Clear()
}

// TestResetLegacyLogsPreservesPreviousCrashReason 验证首次清空旧文件前仍能识别上次闪退，且新日志不会反复删除。
func TestResetLegacyLogsPreservesPreviousCrashReason(t *testing.T) {
	directory := t.TempDir()
	crashPath := filepath.Join(directory, "crash.log")
	if _, _, err := MarkRun(directory, crashPath, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crashPath, []byte("fatal error: old crash\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"goodhr.jsonl", "goodhr.jsonl.1"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("旧日志"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	previous := ReadPreviousRun(directory, crashPath)
	if previous == nil || !strings.Contains(previous.Reason, "fatal error: old crash") {
		t.Fatalf("清理前未能保留闪退原因：%+v", previous)
	}
	reset, err := ResetLegacyLogs(directory)
	if err != nil || !reset {
		t.Fatalf("首次清理旧日志失败：reset=%v err=%v", reset, err)
	}
	for _, name := range []string{"goodhr.jsonl", "goodhr.jsonl.1", "crash.log", "agent-running.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("旧文件仍存在：%s err=%v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "goodhr.jsonl"), []byte("新日志"), 0600); err != nil {
		t.Fatal(err)
	}
	reset, err = ResetLegacyLogs(directory)
	if err != nil || reset {
		t.Fatalf("升级标记未防止重复清理：reset=%v err=%v", reset, err)
	}
	content, err := os.ReadFile(filepath.Join(directory, "goodhr.jsonl"))
	if err != nil || string(content) != "新日志" {
		t.Fatalf("新日志被误删：%q err=%v", content, err)
	}
}

// TestWorkerStderrWithoutTrace 保留没有任务编号的启动日志和未换行的崩溃信息。
func TestWorkerStderrWithoutTrace(t *testing.T) {
	writer, err := Open(t.TempDir(), "worker.log")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	var forwarded []byte
	sink := WorkerLine(writer, func(line []byte) { forwarded = append([]byte(nil), line...) })
	sink([]byte("Error: startup failed password=super-secret"))
	data, err := os.ReadFile(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(forwarded), `"action":"worker.stderr"`) || !strings.Contains(string(data), "startup failed") || strings.Contains(string(data), "super-secret") || strings.Contains(string(forwarded), "super-secret") {
		t.Fatal("崩溃诊断未保存或未脱敏")
	}
}

// TestWorkerStderrClassifiesBrowserObjectFailure 验证已知浏览器对象失效仅记警告，未知错误和致命输出仍保留失败。
func TestWorkerStderrClassifiesBrowserObjectFailure(t *testing.T) {
	const reported = "pw:browser [pid=123][err] ERROR: error in channel content::2/3/2 exception callFunction namespace page Cannot find object with id = id-1448 evaluateFunction/args<@chrome://juggler/content/content/Runtime.js:747:17"
	for _, sample := range []struct {
		name    string
		line    string
		warning bool
	}{
		{"reported_browser_stack", reported, true},
		{"single_content_stack", "pw:browser [pid=123][err] ERROR: Cannot find object with id = id-1448 @chrome://juggler/content/Runtime.js:747:17", true},
		{"explicit_content_channel", "pw:browser [pid=123][err] ERROR: error in channel content::2/3/2 exception callFunction namespace page Cannot find object with id = id-1448", true},
		{"redacted_browser_stack", reported + " password=private-stderr-value", true},
		{"missing_browser_source", strings.Replace(reported, "pw:browser", "worker", 1), false},
		{"unknown_object_failure", "pw:browser [pid=123][err] ERROR: Cannot find object with id = id-1448", false},
		{"other_runtime_path", "pw:browser [pid=123][err] ERROR: Cannot find object with id = id-1448 @chrome://other/content/Runtime.js:747:17", false},
		{"lookalike_runtime_path", "pw:browser [pid=123][err] ERROR: Cannot find object with id = id-1448 @chrome://juggler/content/Runtime.js.backup:747:17", false},
		{"other_channel_namespace", "pw:browser [pid=123][err] ERROR: error in channel content::2/3/2 exception callFunction namespace browser Cannot find object with id = id-1448", false},
		{"lookalike_channel_namespace", "pw:browser [pid=123][err] ERROR: error in channel content::2/3/2 exception callFunction namespace page_aux Cannot find object with id = id-1448", false},
		{"missing_channel_function", "pw:browser [pid=123][err] ERROR: error in channel content::2/3/2 namespace page Cannot find object with id = id-1448", false},
		{"different_browser_error", "pw:browser [pid=123][err] ERROR: channel failed @chrome://juggler/content/Runtime.js:747:17", false},
		{"worker_startup_error", "Error: startup failed: Cannot find object with id = id-1448", false},
		{"worker_panic", "panic: Worker initialization failed", false},
		{"browser_fatal_output", "fatal error: " + reported, false},
		{"browser_panic_output", "panic: " + reported, false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var local bytes.Buffer
			var forwarded []byte
			sink := WorkerLine(&local, func(line []byte) { forwarded = append([]byte(nil), line...) })
			sink([]byte(sample.line))
			var item struct {
				Action       string         `json:"action"`
				Step         string         `json:"step"`
				Status       string         `json:"status"`
				Level        string         `json:"level"`
				IsError      bool           `json:"is_error"`
				ErrorMessage string         `json:"error_message"`
				Message      string         `json:"message"`
				Diagnostic   bool           `json:"diagnostic"`
				Output       map[string]any `json:"output"`
			}
			if err := json.Unmarshal(forwarded, &item); err != nil {
				t.Fatal(err)
			}
			if item.Action != "worker.stderr" || item.Step != "process_output" || item.Diagnostic {
				t.Fatalf("原始输出类型不正确：%s", forwarded)
			}
			expectedMessage := strings.TrimSpace(string(Redact([]byte(sample.line))))
			if item.Message != expectedMessage || !bytes.Equal(bytes.TrimSpace(local.Bytes()), forwarded) {
				t.Fatalf("本地原文或分类字段没有完整保留：%s", local.Bytes())
			}
			if strings.Contains(string(local.Bytes()), "private-stderr-value") {
				t.Fatal("原始输出中的凭据没有脱敏")
			}
			if sample.warning {
				if item.Status != "warning" || item.Level != "warn" || item.IsError || item.ErrorMessage != "" || item.Output["stderr_kind"] != "browser_internal_stale_object" || item.Output["operation_outcome"] != "unknown" {
					t.Fatalf("已知内部对象失效被误报成操作失败或恢复成功：%s", forwarded)
				}
			} else if item.Status != "failed" || item.Level != "error" || !item.IsError || item.ErrorMessage != expectedMessage || len(item.Output) != 0 {
				t.Fatalf("未知或致命错误被错误降级：%s", forwarded)
			}
		})
	}
}

// TestWorkerStructuredObjectFailureStaysError 验证真实操作的结构化失败不会因同样的浏览器原文被降级。
func TestWorkerStructuredObjectFailureStaysError(t *testing.T) {
	message := "pw:browser ERROR: Cannot find object with id = id-1448 @chrome://juggler/content/content/Runtime.js:747:17"
	line, err := json.Marshal(map[string]any{
		"trace_id": "failed-task", "request_id": "failed-request", "action": "element.find", "step": "find",
		"status": "failed", "level": "error", "is_error": true, "error_code": "ELEMENT_NOT_FOUND", "error_message": message,
		"output": map[string]any{"query_error_type": "StaleObjectReference"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var local bytes.Buffer
	sink := WorkerLine(&local, nil)
	sink(line)
	var item map[string]any
	if err := json.Unmarshal(local.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item["action"] != "element.find" || item["trace_id"] != "failed-task" || item["request_id"] != "failed-request" || item["status"] != "failed" || item["level"] != "error" || item["is_error"] != true || item["error_code"] != "ELEMENT_NOT_FOUND" || item["error_message"] != message {
		t.Fatalf("真实操作失败被原始 stderr 分类覆盖：%s", local.Bytes())
	}
	output, ok := item["output"].(map[string]any)
	if !ok || output["query_error_type"] != "StaleObjectReference" || len(output) != 1 {
		t.Fatalf("真实操作错误详情被改写：%s", local.Bytes())
	}
}

// TestWorkerBrowserStartupOutputs 验证真实十五条 Firefox 内部启动输出只记诊断警告，同文案的结构化动作失败仍保留。
func TestWorkerBrowserStartupOutputs(t *testing.T) {
	const rustSource = "moz-src:///toolkit/components/uniffi-bindgen-gecko-js/components/generated/RustSearch.sys.mjs"
	const recordFailure = "reason: JSON error: missing field `recordType` at line 1 column 114"
	rustError := "JavaScript error: " + rustSource + ", line 2681: Error: " + recordFailure
	for index, sample := range []struct {
		timestamp string
		stream    string
		payload   string
		family    string
	}{
		{"2026-10-08T17:22:37.206Z", "out", `console.error: "Warning: unrecognized command line flag" "-foreground"`, "foreground_flag"},
		{"2026-10-08T17:22:37.667Z", "out", "console.error: CustomizableUI:", "customizable_ui_header"},
		{"2026-10-08T17:22:37.924Z", "out", `console.error: SearchService: "#init: failure initializing search:" (new Error("` + recordFailure + `", "` + rustSource + `", 2681))`, "search_init_record_type"},
		{"2026-10-08T17:22:37.924Z", "err", rustError, "rust_search_record_type"},
		{"2026-10-08T17:22:37.925Z", "err", rustError, "rust_search_record_type"},
		{"2026-10-08T17:22:37.925Z", "err", rustError, "rust_search_record_type"},
		{"2026-10-08T17:22:37.925Z", "err", rustError, "rust_search_record_type"},
		{"2026-10-08T17:22:38.194Z", "out", `console.error: "Could not parse ASRouter preference. Try resetting browser.newtabpage.activity-stream.asrouter.providers.snippets in about:config."`, "asrouter_snippets_preference"},
		{"2026-10-08T17:22:38.951Z", "out", `console.error: SearchSettings: "_write: Could not write to settings file:" (new Error("cannot write without any engine.", "moz-src:///toolkit/components/search/SearchSettings.sys.mjs", 346))`, "search_settings_no_engines"},
		{"2026-10-08T17:22:39.210Z", "err", "JavaScript error: chrome://juggler/content/Helper.js, line 82: NS_ERROR_FAILURE: Component returned failure code: 0x80004005 (NS_ERROR_FAILURE) [nsIWebProgress.removeProgressListener]", "juggler_progress_listener"},
		{"2026-10-08T17:22:39.359Z", "out", "console.error: WebExtensions:", "webextensions_header"},
		{"2026-10-08T17:22:39.360Z", "out", "    - message = " + recordFailure, "search_record_type_continuation"},
		{"2026-10-08T17:22:39.360Z", "out", "    - reason = JSON error: missing field `recordType` at line 1 column 114", "search_record_type_continuation"},
		{"2026-10-08T17:22:40.584Z", "out", `console.error: "Search service failed to init"`, "search_service_init"},
		{"2026-10-08T17:22:58.044Z", "err", rustError, "rust_search_record_type"},
	} {
		t.Run(fmt.Sprintf("sample_%02d_%s", index+1, sample.family), func(t *testing.T) {
			line := fmt.Sprintf("%s pw:browser [pid=36176][%s] %s", sample.timestamp, sample.stream, sample.payload)
			var local bytes.Buffer
			var forwarded []byte
			sink := WorkerLine(&local, func(line []byte) { forwarded = append([]byte(nil), line...) })
			sink([]byte(line))
			var item struct {
				Action       string            `json:"action"`
				Status       string            `json:"status"`
				Level        string            `json:"level"`
				IsError      bool              `json:"is_error"`
				ErrorMessage string            `json:"error_message"`
				Message      string            `json:"message"`
				Diagnostic   bool              `json:"diagnostic"`
				Output       map[string]string `json:"output"`
			}
			if err := json.Unmarshal(forwarded, &item); err != nil {
				t.Fatal(err)
			}
			if item.Action != "worker.stderr" || item.Status != "warning" || item.Level != "warn" || item.IsError || item.ErrorMessage != "" || item.Diagnostic || item.Message != line || item.Output["stderr_kind"] != "browser_internal_startup_output" || item.Output["startup_family"] != sample.family || item.Output["operation_outcome"] != "unknown" {
				t.Fatalf("已知内部输出被当成动作失败或原文丢失：%s", forwarded)
			}
			if !bytes.Equal(bytes.TrimSpace(local.Bytes()), forwarded) {
				t.Fatal("本地诊断与转发的分类字段不一致")
			}
			local.Reset()
			structured, err := json.Marshal(map[string]any{
				"action": "element.read", "step": "read", "trace_id": "actual-task", "request_id": "actual-request",
				"status": "failed", "level": "error", "is_error": true, "error_code": "BROWSER_OPERATION_FAILED", "error_message": line,
			})
			if err != nil {
				t.Fatal(err)
			}
			sink(structured)
			var failure map[string]any
			if err := json.Unmarshal(local.Bytes(), &failure); err != nil {
				t.Fatal(err)
			}
			if failure["action"] != "element.read" || failure["status"] != "failed" || failure["level"] != "error" || failure["is_error"] != true || failure["error_code"] != "BROWSER_OPERATION_FAILED" || failure["error_message"] != line || failure["trace_id"] != "actual-task" || failure["request_id"] != "actual-request" {
				t.Fatalf("真实结构化失败被内部输出分类覆盖：%s", local.Bytes())
			}
		})
	}
}

// TestWorkerUnknownBrowserStartupErrorsStayFailed 验证近似签名、未知浏览器输出和致命错误都不会被内部启动输出规则降级。
func TestWorkerUnknownBrowserStartupErrorsStayFailed(t *testing.T) {
	const out = "2026-10-08T17:22:37.206Z pw:browser [pid=36176][out] "
	const errPrefix = "2026-10-08T17:22:37.206Z pw:browser [pid=36176][err] "
	const rustError = "JavaScript error: moz-src:///toolkit/components/uniffi-bindgen-gecko-js/components/generated/RustSearch.sys.mjs, line 2681: Error: reason: JSON error: missing field `recordType` at line 1 column 114"
	const listener = "JavaScript error: chrome://juggler/content/Helper.js, line 82: NS_ERROR_FAILURE: Component returned failure code: 0x80004005 (NS_ERROR_FAILURE) [nsIWebProgress.removeProgressListener]"
	for index, line := range []string{
		"Error: Worker startup failed",
		"console.error: WebExtensions:",
		out + `console.error: "Warning: unrecognized command line flag" "-unsafe-launch"`,
		out + "console.error: CustomizableUI: Error: widget initialization failed",
		out + "console.error: WebExtensions: Error: permissions rejected",
		errPrefix + strings.Replace(rustError, "recordType", "engineId", 1),
		errPrefix + strings.Replace(rustError, "RustSearch.sys.mjs", "OtherSearch.sys.mjs", 1),
		out + rustError,
		out + `console.error: SearchService: "#init: failure initializing search:" (new Error("permission denied", "moz-src:///toolkit/components/uniffi-bindgen-gecko-js/components/generated/RustSearch.sys.mjs", 2681))`,
		out + `console.error: "Could not parse ASRouter preference. Try resetting browser.newtabpage.activity-stream.asrouter.providers.custom in about:config."`,
		out + `console.error: SearchSettings: "_write: Could not write to settings file:" (new Error("permission denied", "moz-src:///toolkit/components/search/SearchSettings.sys.mjs", 346))`,
		errPrefix + strings.Replace(listener, "removeProgressListener", "addProgressListener", 1),
		errPrefix + strings.ReplaceAll(listener, "NS_ERROR_FAILURE", "NS_ERROR_UNEXPECTED"),
		errPrefix + strings.Replace(listener, "Helper.js", "Helper.js.backup", 1),
		out + "    - message = reason: JSON error: missing field `engineId` at line 1 column 114",
		out + "    - reason = JSON error: missing field `recordType` at line 1 column 114; Error: channel closed",
		out + `console.error: "Search service failed to init: BROWSER_NOT_FOUND"`,
		out + "console.error: (void 0)",
		errPrefix + "JavaScript error: resource://gre/modules/AsyncShutdown.sys.mjs, line 587: uncaught exception: undefined",
		"panic: " + out + "console.error: WebExtensions:",
		"fatal error: " + out + "console.error: WebExtensions:",
		"out of memory: " + out + "console.error: WebExtensions:",
		"assertion failed: " + out + "console.error: WebExtensions:",
		errPrefix + "out of memory",
		errPrefix + "assertion failed",
	} {
		t.Run(fmt.Sprintf("unknown_%02d", index+1), func(t *testing.T) {
			var local bytes.Buffer
			sink := WorkerLine(&local, nil)
			sink([]byte(line))
			var item map[string]any
			if err := json.Unmarshal(local.Bytes(), &item); err != nil {
				t.Fatal(err)
			}
			if item["status"] != "failed" || item["level"] != "error" || item["is_error"] != true || item["error_message"] != line || item["message"] != line {
				t.Fatalf("未知或致命错误被错误降级：%s", local.Bytes())
			}
			if output, ok := item["output"].(map[string]any); !ok || len(output) != 0 {
				t.Fatalf("未知错误被误归入已知内部输出：%s", local.Bytes())
			}
		})
	}
}
