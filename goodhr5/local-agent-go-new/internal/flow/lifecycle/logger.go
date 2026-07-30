// Package lifecycle 把流程步骤日志同步到标准日志和 SQLite 当前任务状态。
package lifecycle

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TaskLogger 同时输出日志并更新任务当前步骤。
type TaskLogger struct {
	store *storage.Store
	next  shared.Logger
}

// NewTaskLogger 创建任务步骤日志器。
func NewTaskLogger(store *storage.Store, next shared.Logger) *TaskLogger {
	return &TaskLogger{store: store, next: next}
}

// Step 输出步骤日志，并在步骤开始时更新 SQLite 当前状态。
func (l *TaskLogger) Step(taskID string, flow string, step string, status string, startedAt time.Time, err error) {
	if l.next != nil {
		l.next.Step(taskID, flow, step, status, startedAt, err)
	}
	if l.store != nil && status == "start" {
		l.updateCurrentStep(taskID, userStepLabel(step))
	}
	if l.store != nil {
		message := taskLogMessage(step, status, err)
		if _, saveErr := l.store.SaveTaskLog(context.Background(), storage.TaskLog{
			TaskID: taskID, Flow: flow, Step: step, Status: status,
			Message: message, DurationMS: time.Since(startedAt).Milliseconds(),
		}); saveErr != nil {
			log.Printf("task_id=%s flow=lifecycle step=save_task_log status=warning error=%q", taskID, saveErr)
		}
	}
}

// Progress 把具体中文进度同时写入当前任务状态和用户可见日志。
func (l *TaskLogger) Progress(taskID string, message string) {
	message = strings.TrimSpace(message)
	if l == nil || l.store == nil || message == "" {
		return
	}
	l.updateCurrentStep(taskID, message)
	if _, err := l.store.SaveTaskLog(context.Background(), storage.TaskLog{
		TaskID: taskID, Flow: "progress", Step: "progress", Status: "running",
		Message: message,
	}); err != nil {
		log.Printf("task_id=%s flow=lifecycle step=save_progress status=warning error=%q", taskID, err)
	}
}

// updateCurrentStep 更新悬浮窗读取的当前任务步骤。
func (l *TaskLogger) updateCurrentStep(taskID string, message string) {
	if l == nil || l.store == nil || strings.TrimSpace(message) == "" {
		return
	}
	if err := l.store.UpdateTaskStep(context.Background(), taskID, message); err != nil {
		log.Printf("task_id=%s flow=lifecycle step=update_task_step status=warning error=%q", taskID, err)
	}
}

// workerLogLine 表示 Worker 输出到 stdout 的统一 JSON 行字段。
type workerLogLine struct {
	Timestamp         string `json:"timestamp"`
	Level             string `json:"level"`
	TraceID           string `json:"trace_id"`
	Action            string `json:"action"`
	Step              string `json:"step"`
	Status            string `json:"status"`
	DurationMS        int64  `json:"duration_ms"`
	PageURL           string `json:"page_url"`
	TargetDescription string `json:"target_description"`
	ErrorCode         string `json:"error_code"`
	ErrorMessage      string `json:"error_message"`
	OriginalError     string `json:"original_error"`
	PollAttempts      int    `json:"poll_attempts"`
	TimeoutMS         int    `json:"timeout_ms"`
	Attempt           int    `json:"attempt"`
	RemainingDistance int    `json:"remaining_distance"`
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
		return
	}
	exists, err := l.store.TaskExists(context.Background(), item.TraceID)
	if err != nil {
		log.Printf("task_id=%s flow=browser_worker step=check_task status=warning error=%q", item.TraceID, err)
		return
	}
	if !exists {
		return
	}
	if !keepWorkerLog(item) {
		return
	}
	message := workerLogMessage(item)
	if strings.EqualFold(item.Status, "start") {
		l.updateCurrentStep(item.TraceID, workerCurrentStep(item))
	}
	if _, err := l.store.SaveTaskLog(context.Background(), storage.TaskLog{
		TaskID: item.TraceID, Flow: "browser_worker", Step: item.Step,
		Status: item.Status, Level: item.Level, Message: message,
		DurationMS: item.DurationMS, CreatedAt: item.Timestamp,
	}); err != nil {
		log.Printf("task_id=%s flow=browser_worker step=save_log status=warning error=%q", item.TraceID, err)
	}
}

// workerCurrentStep 把 Worker 封装动作转换成悬浮窗使用的简短中文状态。
func workerCurrentStep(item workerLogLine) string {
	label := workerActionLabel(item.Action)
	target := strings.TrimSpace(item.TargetDescription)
	if target != "" && !strings.Contains(label, target) {
		label += "“" + target + "”"
	}
	return "正在" + label
}

// taskLogMessage 把流程步骤和状态转换成短中文用户文案。
func taskLogMessage(step string, status string, err error) string {
	label := userStepLabel(step)
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "start", "running":
		return label + "：开始"
	case "success", "completed":
		return label + "：完成"
	case "skipped":
		if err != nil {
			return label + "：跳过，" + err.Error()
		}
		return label + "：跳过"
	case "warning":
		if err != nil {
			return label + "：我小声提醒一下，" + err.Error()
		}
		return label + "：我小声提醒一下"
	case "failed":
		if err != nil {
			return label + "：没处理成功，" + err.Error()
		}
		return label + "：没处理成功，可以直接重试"
	default:
		return label + "：进行中"
	}
}

// userStepLabel 返回流程内部步骤对应的中文名称。
func userStepLabel(step string) string {
	step = strings.TrimSpace(step)
	if strings.HasPrefix(step, "wait_") {
		return "拟人等待"
	}
	labels := map[string]string{
		"validate_request":           "检查启动参数",
		"check_local_program":        "检查本地程序",
		"check_cloud_session":        "检查登录状态",
		"load_position_snapshot":     "读取岗位配置",
		"check_subscription":         "检查会员状态",
		"load_user_preferences":      "读取个人设置",
		"load_local_platform_config": "读取平台配置",
		"check_profile":              "检查浏览器账号",
		"check_task_conflict":        "检查运行中的任务",
		"check_node_runtime":         "检查浏览器运行组件",
		"check_worker_build":         "检查浏览器操作程序",
		"check_worker":               "启动浏览器操作程序",
		"check_cloakbrowser":         "检查增强浏览器",
		"check_local_storage":        "检查本地数据",
		"check_required_ai":          "检查 AI 配置",
		"check_required_ocr":         "检查文字识别组件",
		"check_power_guard":          "检查防休眠能力",
		"start_browser":              "启动增强浏览器",
		"open_greeting_page":         "打开打招呼页面",
		"initialize_greeting_page":   "整理打招呼页面",
		"select_position":            "选择岗位",
		"apply_basic_filters":        "应用基础筛选",
		"scan_decide_and_greet":      "处理候选人并打招呼",
		"candidate_fingerprint":      "确认候选人身份",
		"sync_processed_resumes":     "同步候选人数量",
		"candidate_operation":        "处理当前候选人",
		"scroll_to_candidate":        "滚动到当前候选人",
		"open_detail":                "打开候选人详情",
		"read_detail_text":           "读取候选人详情",
		"browse_candidate_detail":    "浏览候选人详情",
		"close_detail":               "关闭候选人详情",
		"ai_decision":                "AI 正在判断候选人匹配度",
		"request_candidate_info":     "索要候选人资料",
		"greet":                      "打招呼",
		"save_candidate":             "保存处理结果",
		"simulated_rest":             "拟人休息",
		"open_message_page":          "打开消息页面",
		"initialize_message_page":    "整理消息页面",
		"scan_generate_and_reply":    "读取消息并自动回复",
		"read_conversation":          "读取候选人消息",
		"generate_reply":             "生成回复",
		"reply_safety_check":         "检查回复内容",
		"reply_duplicate_check":      "检查重复回复",
		"send_reply":                 "发送回复",
		"save_conversation":          "保存回复结果",
		"request_cloud_start":        "向云端申请开始任务",
		"start_power_guard":          "启动防休眠",
		"dispatch_task_flow":         "运行岗位任务",
		"detect_sleep_resume":        "检查电脑休眠恢复",
		"sync_completed_status":      "同步完成状态",
		"sync_failed_counts":         "同步失败统计",
		"sync_stopped_status":        "同步停止状态",
		"send_failure_notice":        "发送失败提醒",
		"play_failure_sound":         "播放失败提示音",
		"detail_precheck":            "判断是否值得查看详情",
		"candidate_preview":          "AI 正在批量判断候选人基础信息",
		"detail_probability":         "判断本次是否查看详情",
		"decision":                   "判断是否适合打招呼",
		"filter":                     "筛选候选人",
		"detail":                     "读取候选人详情",
		"ocr":                        "OCR 正在识别候选人详情",
		"cleanup_detail_screenshots": "清理临时截图",
		"play_success_sound":         "播放成功提示音",
		"save_running_sync_failure":  "保存运行状态",
		"save_final_task":            "保存任务结果",
		"conversation_operation":     "处理当前会话",
		"list_view":                  "浏览候选人列表",
		"after_scroll":               "等待列表加载",
		"before_detail_open":         "准备查看候选人详情",
		"detail_view":                "浏览候选人详情",
		"before_detail_close":        "准备关闭候选人详情",
		"before_greet":               "准备打招呼",
	}
	if label := labels[step]; label != "" {
		return label
	}
	return "执行任务步骤"
}

// keepWorkerLog 只保留封装操作的首尾日志和所有失败，隐藏查找、移动等重复内部过程。
func keepWorkerLog(item workerLogLine) bool {
	if strings.EqualFold(item.Status, "failed") || strings.EqualFold(item.Level, "error") {
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
	label := workerActionLabel(item.Action)
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

// workerActionLabel 返回 Worker 封装操作对应的中文名称。
func workerActionLabel(action string) string {
	labels := map[string]string{
		"browser.start":           "启动增强浏览器",
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
