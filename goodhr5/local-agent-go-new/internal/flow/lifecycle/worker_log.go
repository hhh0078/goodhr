// Package lifecycle 文件作用：解析浏览器 Worker 日志，向岗位时间线写入有候选人上下文的错误详情。
package lifecycle

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"

	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
)

// workerLogLine 表示 Worker 输出到 stdout 的统一 JSON 行字段。
type workerLogLine struct {
	Timestamp         string          `json:"timestamp"`
	Level             string          `json:"level"`
	Category          string          `json:"category"`
	TraceID           string          `json:"trace_id"`
	RequestID         string          `json:"request_id"`
	Action            string          `json:"action"`
	Step              string          `json:"step"`
	Status            string          `json:"status"`
	DurationMS        int64           `json:"duration_ms"`
	PageURL           string          `json:"page_url"`
	TargetDescription string          `json:"target_description"`
	ErrorCode         string          `json:"error_code"`
	ErrorMessage      string          `json:"error_message"`
	OriginalError     string          `json:"original_error"`
	PollAttempts      int             `json:"poll_attempts"`
	TimeoutMS         int             `json:"timeout_ms"`
	Attempt           int             `json:"attempt"`
	RemainingDistance int             `json:"remaining_distance"`
	ExitCode          int             `json:"exit_code"`
	ExitReason        string          `json:"exit_reason"`
	Message           string          `json:"message"`
	Input             json.RawMessage `json:"input"`
	Output            json.RawMessage `json:"output"`
	Diagnostic        bool            `json:"diagnostic"`
}

// WorkerLine 解析 Worker JSON 行，并按 trace_id 汇入对应任务的岗位日志。
func (l *TaskLogger) WorkerLine(line []byte) {
	if l == nil || l.store == nil {
		return
	}
	var item workerLogLine
	if err := json.Unmarshal(line, &item); err != nil {
		log.Printf("flow=browser_worker step=parse_log status=warning error=%q", err)
		return
	}
	item.TraceID = strings.TrimSpace(item.TraceID)
	if item.TraceID == "" {
		if strings.HasPrefix(item.Action, "worker.") || strings.HasPrefix(item.Action, "browser.") {
			item.TraceID, _ = l.store.RunningTaskID(context.Background())
		}
		if item.TraceID == "" {
			return
		}
	}
	if item.Action == "worker.exit" && item.ExitCode != 0 && item.ErrorMessage == "" {
		item.ErrorMessage = item.ExitReason
	}
	if item.ErrorMessage == "" && (strings.EqualFold(item.Status, "failed") || strings.EqualFold(item.Level, "error")) {
		item.ErrorMessage = strings.TrimSpace(item.OriginalError)
		if item.ErrorMessage == "" {
			item.ErrorMessage = strings.TrimSpace(item.Message)
		}
	}
	exists, err := l.store.TaskExists(context.Background(), item.TraceID)
	if err != nil {
		log.Printf("task_id=%s flow=browser_worker step=check_task status=warning error=%q", item.TraceID, err)
		return
	}
	if !exists {
		return
	}
	message := workerLogMessage(item)
	if strings.EqualFold(item.Status, "start") && keepWorkerLog(item) {
		l.updateCurrentStep(item.TraceID, l.withCandidate(item.TraceID, workerCurrentStep(item)))
	}
	isError := strings.EqualFold(item.Level, "error") || strings.EqualFold(item.Status, "failed") || item.Action == "worker.crash" || (item.Action == "worker.exit" && item.ExitCode != 0)
	if !isError && !item.Diagnostic {
		return
	}
	message = strings.TrimSpace(string(diagnostics.Redact([]byte(l.withCandidate(item.TraceID, message)))))
	input, output := workerLogObjects(item)
	if _, err := l.store.SaveTaskLog(context.Background(), storage.TaskLog{
		TaskID: item.TraceID, Flow: "browser_worker", Step: item.Step,
		Status: item.Status, Level: item.Level, Message: message,
		DurationMS: item.DurationMS, CreatedAt: item.Timestamp, Category: item.Category,
		IsError:      isError,
		ErrorMessage: strings.TrimSpace(string(diagnostics.Redact([]byte(item.ErrorMessage)))), Input: input, Output: output,
	}); err != nil {
		log.Printf("task_id=%s flow=browser_worker step=save_log status=warning error=%q", item.TraceID, err)
	}
}

// workerLogObjects 保留 Worker 已脱敏的定位参数和结果，同时补上退出与重试信息。
func workerLogObjects(item workerLogLine) (json.RawMessage, json.RawMessage) {
	input := map[string]any{}
	output := map[string]any{}
	_ = json.Unmarshal(item.Input, &input)
	_ = json.Unmarshal(item.Output, &output)
	if input == nil {
		input = map[string]any{}
	}
	if output == nil {
		output = map[string]any{}
	}
	input["action"] = item.Action
	input["step"] = item.Step
	if item.TargetDescription != "" {
		input["target_description"] = item.TargetDescription
	}
	if item.TimeoutMS > 0 {
		input["timeout_ms"] = item.TimeoutMS
	}
	output["status"] = item.Status
	output["duration_ms"] = item.DurationMS
	if item.RequestID != "" {
		output["request_id"] = item.RequestID
	}
	if item.PageURL != "" {
		output["page_url"] = item.PageURL
	}
	if item.ErrorCode != "" {
		output["error_code"] = item.ErrorCode
	}
	if item.OriginalError != "" {
		output["original_error"] = item.OriginalError
	}
	if item.PollAttempts > 0 {
		output["poll_attempts"] = item.PollAttempts
	}
	if item.Attempt > 0 {
		output["attempt"] = item.Attempt
	}
	if item.Action == "worker.exit" {
		output["exit_code"] = item.ExitCode
		output["exit_reason"] = item.ExitReason
	}
	inputJSON, _ := json.Marshal(input)
	outputJSON, _ := json.Marshal(output)
	return diagnostics.Redact(inputJSON), diagnostics.Redact(outputJSON)
}

// workerCurrentStep 把 Worker 封装动作转换成悬浮窗使用的简短中文状态。
func workerCurrentStep(item workerLogLine) string {
	label := workerStepLabel(item)
	target := strings.TrimSpace(item.TargetDescription)
	if target != "" && !strings.Contains(label, target) {
		label += "“" + target + "”"
	}
	return "正在" + label
}

// keepWorkerLog 只保留封装操作的首尾日志和所有失败，隐藏查找、移动等重复内部过程。
func keepWorkerLog(item workerLogLine) bool {
	if item.Diagnostic || strings.EqualFold(item.Status, "failed") || strings.EqualFold(item.Level, "error") {
		return true
	}
	expected := map[string]string{
		"browser.start":           "start_browser",
		"browser.stop":            "stop_browser",
		"page.open":               "open_page",
		"page.close":              "close_page",
		"page.use":                "use_page",
		"element.find":            "find",
		"element.find_all":        "find_all",
		"element.read":            "read",
		"element.click":           "click",
		"element.input":           "input",
		"element.scroll":          "scroll",
		"page.scroll":             "scroll",
		"keyboard.press":          "press_key",
		"page.screenshot":         "screenshot",
		"element.screenshot":      "screenshot",
		"element.screenshot_long": "screenshot_long",
	}
	return expected[item.Action] == item.Step
}

// workerLogMessage 把 Worker 稳定字段整理成中文用户日志。
func workerLogMessage(item workerLogLine) string {
	if item.Action == "browser.exit" {
		return "浏览器异常退出：" + strings.TrimSpace(item.ErrorMessage)
	}
	if item.Action == "worker.exit" {
		return "浏览器操作进程已退出，退出代码：" + strconv.Itoa(item.ExitCode) + "；原因：" + item.ExitReason
	}
	if item.Action == "worker.crash" {
		return "浏览器操作进程异常退出：" + item.ErrorMessage
	}
	label := workerStepLabel(item)
	target := strings.TrimSpace(item.TargetDescription)
	if target != "" && !strings.Contains(label, target) {
		label += "“" + target + "”"
	}
	if strings.EqualFold(item.Status, "failed") || strings.EqualFold(item.Level, "error") {
		reason := strings.TrimSpace(item.ErrorMessage)
		if reason == "" {
			reason = "浏览器操作没有完成"
		}
		parts := []string{label + "：没处理成功，" + reason}
		if item.PollAttempts > 0 {
			parts = append(parts, "已经每 300 毫秒找了 "+strconv.Itoa(item.PollAttempts)+" 次")
		}
		parts = append(parts, workerLogSuggestion(item.ErrorCode))
		return strings.Join(parts, "；")
	}
	if strings.EqualFold(item.Status, "start") {
		return label + "：开始"
	}
	return label + "：完成"
}

// workerStepLabel 为重要鼠标诊断显示真实原子步骤，其余日志复用原有动作名称。
func workerStepLabel(item workerLogLine) string {
	if item.Diagnostic {
		if label := map[string]string{"mouse_move": "移动鼠标", "mouse_down": "按下鼠标", "mouse_up": "松开鼠标", "mouse_wheel": "滚动鼠标"}[item.Step]; label != "" {
			return label
		}
	}
	return workerActionLabel(item.Action)
}

// workerActionLabel 返回 Worker 封装操作对应的中文名称。
func workerActionLabel(action string) string {
	labels := map[string]string{
		"browser.start":           "启动增强浏览器",
		"browser.exit":            "浏览器异常退出",
		"worker.exit":             "浏览器操作进程退出",
		"worker.crash":            "浏览器操作进程崩溃",
		"worker.stderr":           "浏览器操作程序输出异常",
		"browser.stop":            "关闭增强浏览器",
		"page.open":               "打开页面",
		"page.close":              "关闭页面",
		"page.use":                "切换页面",
		"element.find":            "查找页面元素",
		"element.find_all":        "读取候选人列表",
		"element.read":            "读取页面内容",
		"element.click":           "点击",
		"element.input":           "输入",
		"element.scroll":          "滚动到目标",
		"page.scroll":             "滚动页面",
		"keyboard.press":          "按下键盘按键",
		"page.screenshot":         "截取页面",
		"element.screenshot":      "截取页面区域",
		"element.screenshot_long": "分段读取详情",
	}
	if label := labels[strings.TrimSpace(action)]; label != "" {
		return label
	}
	return "执行浏览器操作"
}

// workerLogSuggestion 根据 Worker 稳定错误码返回下一步提示。
func workerLogSuggestion(code string) string {
	switch strings.TrimSpace(code) {
	case "VIEWPORT_TOO_SMALL", "MOUSE_TARGET_OUTSIDE_VIEWPORT":
		return "请把浏览器窗口放大后再试"
	case "ELEMENT_NOT_FOUND":
		return "页面可能还没加载好，或平台页面结构有变化"
	case "SCROLL_NO_PROGRESS":
		return "列表可能已经到底，或当前区域不能继续滚动"
	default:
		return "我已经记下具体步骤，可以直接重试"
	}
}
