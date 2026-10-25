// Package lifecycle 把流程步骤日志同步到标准日志和 SQLite 当前任务状态。
package lifecycle

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
)

const analysisResultHoldDuration = 8 * time.Second

// TaskLogger 同时输出日志并更新任务当前步骤。
type TaskLogger struct {
	store              *storage.Store
	next               shared.Logger
	candidateMu        sync.RWMutex
	candidateTaskID    string
	candidateName      string
	analysisMu         sync.RWMutex
	analysisTaskID     string
	analysis           shared.AnalysisStatus
	analysisVisibleAt  time.Time
	pendingAnalysis    shared.AnalysisStatus
	aiDiagnosticMu     sync.Mutex
	aiDiagnosticErrors []aiDiagnosticLog
}

// aiDiagnosticLog 只保存诊断编号和已落库的记录编号，避免去重缓存保留大型原文。
type aiDiagnosticLog struct {
	DiagnosticID uint64
	LogID        int64
}

// ReportAnalysis 保存当前任务最近一次 AI 或关键词判断状态。
func (l *TaskLogger) ReportAnalysis(taskID string, status shared.AnalysisStatus) {
	if l == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	status.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	status.Keywords = append([]string(nil), status.Keywords...)
	status.MatchedKeywords = append([]string(nil), status.MatchedKeywords...)
	status.ExcludeKeywords = append([]string(nil), status.ExcludeKeywords...)
	status.MatchedExcludes = append([]string(nil), status.MatchedExcludes...)
	l.analysisMu.Lock()
	now := time.Now()
	if l.analysisTaskID == taskID &&
		l.analysis.Terminal &&
		!l.analysisVisibleAt.IsZero() &&
		now.Sub(l.analysisVisibleAt) < analysisResultHoldDuration &&
		analysisChangedCandidate(l.analysis, status) {
		l.pendingAnalysis = status
		l.analysisMu.Unlock()
		return
	}
	l.analysisTaskID = taskID
	l.analysis = status
	l.analysisVisibleAt = now
	l.pendingAnalysis = shared.AnalysisStatus{}
	l.analysisMu.Unlock()
}

// AnalysisStatus 返回指定任务最近一次结构化判断状态的副本。
func (l *TaskLogger) AnalysisStatus(taskID string) *shared.AnalysisStatus {
	if l == nil {
		return nil
	}
	l.analysisMu.Lock()
	defer l.analysisMu.Unlock()
	if l.analysisTaskID != taskID || strings.TrimSpace(l.analysis.Kind) == "" {
		return nil
	}
	if strings.TrimSpace(l.pendingAnalysis.Kind) != "" &&
		l.analysis.Terminal &&
		!l.analysisVisibleAt.IsZero() &&
		time.Since(l.analysisVisibleAt) >= analysisResultHoldDuration {
		l.analysis = l.pendingAnalysis
		l.analysis.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		l.analysisVisibleAt = time.Now()
		l.pendingAnalysis = shared.AnalysisStatus{}
	}
	status := l.analysis
	status.Keywords = append([]string(nil), status.Keywords...)
	status.MatchedKeywords = append([]string(nil), status.MatchedKeywords...)
	status.ExcludeKeywords = append([]string(nil), status.ExcludeKeywords...)
	status.MatchedExcludes = append([]string(nil), status.MatchedExcludes...)
	return &status
}

// analysisChangedCandidate 判断新状态是否已经切换到另一位候选人。
func analysisChangedCandidate(current shared.AnalysisStatus, incoming shared.AnalysisStatus) bool {
	currentName := strings.TrimSpace(current.CandidateName)
	incomingName := strings.TrimSpace(incoming.CandidateName)
	return currentName != "" && incomingName != "" && currentName != incomingName
}

// ResetAnalysis 清空旧分析内容并把内存状态归属到当前任务。
func (l *TaskLogger) ResetAnalysis(taskID string) {
	if l == nil {
		return
	}
	l.analysisMu.Lock()
	l.analysisTaskID = taskID
	l.analysis = shared.AnalysisStatus{}
	l.analysisVisibleAt = time.Time{}
	l.pendingAnalysis = shared.AnalysisStatus{}
	l.analysisMu.Unlock()
}

// NewTaskLogger 创建任务步骤日志器。
func NewTaskLogger(store *storage.Store, next shared.Logger) *TaskLogger {
	return &TaskLogger{store: store, next: next}
}

// Step 输出步骤日志，并在步骤开始时更新 SQLite 当前状态。
func (l *TaskLogger) Step(taskID string, flow string, step string, status string, startedAt time.Time, err error) {
	if l == nil {
		return
	}
	if l.next != nil {
		l.next.Step(taskID, flow, step, status, startedAt, err)
	}
	if l.store != nil && status == "start" {
		l.updateCurrentStep(taskID, l.withCandidate(taskID, userStepLabel(step)))
	}
	if l.store != nil && visibleTaskStep(flow, step, status, err) {
		message := taskLogMessage(step, status, err)
		message = l.withCandidate(taskID, message)
		errorMessage := ""
		if err != nil && (status == "failed" || status == "panic" || status == "warning") {
			errorMessage = strings.TrimSpace(string(diagnostics.Redact([]byte(err.Error()))))
		}
		message = strings.TrimSpace(string(diagnostics.Redact([]byte(message))))
		input, _ := json.Marshal(struct {
			Flow string `json:"flow"`
			Step string `json:"step"`
		}{flow, step})
		output := l.taskLogOutput(status, time.Since(startedAt).Milliseconds(), err)
		isError := status == "failed" || status == "panic" || (status == "warning" && err != nil)
		category := "运行"
		if isError {
			category = "错误"
		}
		var diagnostic struct {
			AIResponse json.RawMessage `json:"ai_response_diagnostic"`
		}
		if json.Unmarshal(output, &diagnostic) == nil && len(diagnostic.AIResponse) > 0 {
			if line, marshalErr := json.Marshal(map[string]any{
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "task_id": taskID,
				"action": flow, "step": step, "status": status, "category": category,
				"is_error": isError, "message": message, "error_message": errorMessage,
				"input": input, "output": output,
			}); marshalErr == nil {
				log.Print(string(line))
			}
		}
		saved, saveErr := l.store.SaveTaskLog(context.Background(), storage.TaskLog{
			TaskID: taskID, Flow: flow, Step: step, Status: status,
			Message: message, DurationMS: time.Since(startedAt).Milliseconds(), Category: category,
			IsError: isError, ErrorMessage: errorMessage,
			Input: input, Output: output,
		})
		if saveErr != nil {
			log.Printf("task_id=%s flow=lifecycle step=save_task_log status=warning error=%q", taskID, saveErr)
		} else {
			l.recordAIResponseDiagnostic(err, output, saved.ID)
		}
	}
}

// visibleTaskStep 只选取岗位时间线需要的流程节点和真实故障。
func visibleTaskStep(flow string, step string, status string, err error) bool {
	if flow == "greeting" && (step == "screening_first" || step == "screening_second") {
		return status != "start" && status != "running"
	}
	if status == "failed" || status == "panic" || (status == "warning" && err != nil) {
		return true
	}
	if flow == "lifecycle" && step == "finished" {
		return true
	}
	if flow == "lifecycle" && step == "agent_start" && status == "start" {
		return true
	}
	if flow == "lifecycle" && step == "agent_shutdown" && status == "stopped" {
		return true
	}
	if flow == "greeting" && step == "browse_candidate_detail" {
		return status == "start" || status == "success"
	}
	if status != "start" {
		return false
	}
	visible := map[string]bool{
		"greeting/open_greeting_page":        true,
		"greeting/select_position":           true,
		"greeting/apply_basic_filters":       true,
		"greeting/scan_decide_and_greet":     true,
		"auto_reply/open_message_page":       true,
		"auto_reply/scan_generate_and_reply": true,
	}
	return visible[flow+"/"+step]
}

// Progress 只更新当前任务状态；岗位时间线由精选流程节点和候选人动作组成。
func (l *TaskLogger) Progress(taskID string, message string) {
	message = strings.TrimSpace(message)
	if l == nil || l.store == nil || message == "" {
		return
	}
	l.updateCurrentStep(taskID, message)
}

// CandidateActivity 保存一条精简的候选人动作，并设置原子操作故障的当前候选人上下文。
func (l *TaskLogger) CandidateActivity(taskID string, candidateName string, action string, status string, err error) {
	if l == nil || l.store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	candidateName = strings.Join(strings.Fields(candidateName), " ")
	if candidateName == "" {
		candidateName = "当前候选人"
	}
	if characters := []rune(candidateName); len(characters) > 80 {
		candidateName = string(characters[:80]) + "…"
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return
	}
	l.candidateMu.Lock()
	l.candidateTaskID, l.candidateName = taskID, candidateName
	l.candidateMu.Unlock()
	message := "当前“" + candidateName + "”，" + action
	if err != nil {
		message += "失败：" + err.Error()
	}
	isError := err != nil || status == "failed"
	category := "运行"
	if isError {
		category = "错误"
	}
	l.updateCurrentStep(taskID, message)
	input, _ := json.Marshal(map[string]string{"action": action})
	message = strings.TrimSpace(string(diagnostics.Redact([]byte(message))))
	errorMessage := candidateErrorText(err)
	output := l.taskLogOutput(status, 0, err)
	if line, marshalErr := json.Marshal(map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"category":  category, "is_error": isError, "error_message": errorMessage,
		"message": message, "task_id": taskID, "action": "candidate", "step": action,
		"status": status, "input": json.RawMessage(input), "output": output,
	}); marshalErr == nil {
		log.Print(string(line))
	}
	saved, saveErr := l.store.SaveTaskLog(context.Background(), storage.TaskLog{
		TaskID: taskID, Flow: "candidate", Step: action, Status: status,
		Message:  message,
		Category: category, IsError: isError,
		ErrorMessage: errorMessage, Input: input, Output: output,
	})
	if saveErr != nil {
		log.Printf("task_id=%s flow=lifecycle step=save_candidate_activity status=warning error=%q", taskID, saveErr)
	} else {
		l.recordAIResponseDiagnostic(err, output, saved.ID)
	}
}

// candidateErrorText 对候选人动作错误做脱敏，保留可读原因。
func candidateErrorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(string(diagnostics.Redact([]byte(err.Error()))))
}

// ClearCandidate 在一位候选人处理完后清除上下文。
func (l *TaskLogger) ClearCandidate(taskID string) {
	if l == nil {
		return
	}
	l.candidateMu.Lock()
	if l.candidateTaskID == taskID {
		l.candidateTaskID, l.candidateName = "", ""
	}
	l.candidateMu.Unlock()
}

// withCandidate 为流程或 Worker 错误补充当前候选人姓名。
func (l *TaskLogger) withCandidate(taskID string, message string) string {
	l.candidateMu.RLock()
	defer l.candidateMu.RUnlock()
	if l.candidateTaskID != taskID || l.candidateName == "" || strings.Contains(message, l.candidateName) {
		return message
	}
	return "当前“" + l.candidateName + "”，" + message
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

// taskLogMessage 把流程步骤和状态转换成短中文用户文案。
func taskLogMessage(step string, status string, err error) string {
	if step == "screening_first" || step == "screening_second" {
		label := "第一轮筛选"
		if step == "screening_second" {
			label = "第二轮筛选"
		}
		if err != nil {
			return label + "：" + err.Error()
		}
		return label + "：" + status
	}
	if step == "agent_start" && status == "start" {
		return "本地程序已启动，正在执行岗位任务"
	}
	if step == "agent_shutdown" && err != nil {
		return "本地程序退出，原因：" + err.Error()
	}
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
	case "panic":
		if err != nil {
			return label + "：发生未捕获异常，" + err.Error()
		}
		return label + "：发生未捕获异常"
	case "stopped":
		return "岗位任务已停止"
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
		"validate_request":              "检查启动参数",
		"check_local_program":           "检查本地程序",
		"check_cloud_session":           "检查登录状态",
		"load_position_snapshot":        "读取岗位配置",
		"check_subscription":            "检查会员状态",
		"load_user_preferences":         "读取个人设置",
		"load_local_platform_config":    "读取平台配置",
		"check_profile":                 "检查浏览器账号",
		"check_task_conflict":           "检查运行中的任务",
		"check_node_runtime":            "检查浏览器运行组件",
		"check_worker_build":            "检查浏览器操作程序",
		"check_worker":                  "启动浏览器操作程序",
		"check_camoufox":                "检查隐身浏览器",
		"check_local_storage":           "检查本地数据",
		"check_required_ai":             "检查 AI 配置",
		"check_required_ocr":            "检查文字识别组件",
		"check_power_guard":             "检查防休眠能力",
		"start_browser":                 "启动隐身浏览器",
		"prepare_screenshot_workspace":  "准备本次截图目录",
		"open_greeting_page":            "打开打招呼页面",
		"initialize_greeting_page":      "整理打招呼页面",
		"select_position":               "选择岗位",
		"apply_basic_filters":           "应用基础筛选",
		"scan_decide_and_greet":         "处理候选人并打招呼",
		"candidate_fingerprint":         "确认候选人身份",
		"sync_processed_resumes":        "同步候选人数量",
		"candidate_operation":           "处理当前候选人",
		"candidate_timeout":             "候选人处理超时",
		"scroll_to_candidate":           "滚动到当前候选人",
		"open_detail":                   "打开候选人详情",
		"read_detail_text":              "读取候选人详情",
		"browse_candidate_detail":       "滚动浏览详情",
		"close_detail":                  "关闭候选人详情",
		"ai_decision":                   "AI 正在判断候选人匹配度",
		"request_candidate_info":        "索要候选人资料",
		"ensure_candidate_conversation": "确认候选人对话框",
		"send_candidate_message":        "发送首次打招呼语",
		"close_candidate_conversation":  "关闭候选人对话框",
		"greet":                         "打招呼",
		"save_candidate":                "保存处理结果",
		"simulated_rest":                "拟人休息",
		"open_message_page":             "打开消息页面",
		"initialize_message_page":       "整理消息页面",
		"scan_generate_and_reply":       "读取消息并自动回复",
		"read_conversation":             "读取候选人消息",
		"generate_reply":                "生成回复",
		"reply_safety_check":            "检查回复内容",
		"reply_duplicate_check":         "检查重复回复",
		"send_reply":                    "发送回复",
		"save_conversation":             "保存回复结果",
		"request_cloud_start":           "向云端申请开始任务",
		"agent_shutdown":                "本地程序退出",
		"agent_start":                   "本地程序启动",
		"finished":                      "岗位任务",
		"platform_id":                   "确认招聘平台",
		"open_login_page":               "打开平台登录页",
		"initialize_login_page":         "整理平台登录页",
		"find_candidates":               "查找候选人",
		"open_candidate_detail":         "打开候选人详情",
		"extract_candidate_detail":      "读取候选人详情",
		"clean_candidate_detail_text":   "整理候选人详情文本",
		"close_candidate_detail":        "关闭候选人详情",
		"greet_candidate":               "向候选人打招呼",
		"favorite_candidate":            "收藏候选人",
		"reject_candidate":              "忽略候选人",
		"advance_candidate_list":        "进入下一批候选人",
		"open_messages_page":            "打开消息页面",
		"initialize_messages_page":      "整理消息页面",
		"scan_unread_conversations":     "扫描未读会话",
		"reply_conversation":            "回复候选人会话",
		"start_power_guard":             "启动防休眠",
		"dispatch_task_flow":            "运行岗位任务",
		"detect_sleep_resume":           "检查电脑休眠恢复",
		"sync_completed_status":         "同步完成状态",
		"sync_failed_counts":            "同步失败统计",
		"sync_stopped_status":           "同步停止状态",
		"send_failure_notice":           "发送失败提醒",
		"play_failure_sound":            "播放失败提示音",
		"detail_precheck":               "判断是否值得查看详情",
		"candidate_preview":             "AI 正在批量判断候选人基础信息",
		"detail_probability":            "判断本次是否查看详情",
		"decision":                      "判断是否适合打招呼",
		"filter":                        "筛选候选人",
		"detail":                        "读取候选人详情",
		"ocr":                           "OCR 正在识别候选人详情",
		"cleanup_detail_screenshots":    "清理临时截图",
		"play_success_sound":            "播放成功提示音",
		"save_running_sync_failure":     "保存运行状态",
		"save_final_task":               "保存任务结果",
		"conversation_operation":        "处理当前会话",
		"list_view":                     "浏览候选人列表",
		"after_scroll":                  "等待列表加载",
		"before_detail_open":            "准备查看候选人详情",
		"detail_view":                   "浏览候选人详情",
		"before_detail_close":           "准备关闭候选人详情",
		"before_greet":                  "准备打招呼",
	}
	if label := labels[step]; label != "" {
		return label
	}
	return "执行任务步骤"
}
