// 本文件负责提供云端任务创建和查询的 HTTP API。
package httpapi

import (
	"encoding/json"
	"errors"
	stdlog "log"
	"net/http"
	"strings"
	"time"
)

// TaskService 处理任务创建、列表、详情和执行请求。
type TaskService struct {
	auth           *AuthService
	store          TaskStore
	positionStore  PositionStore
	taskLogs       TaskLogService
	tenantStore    TenantStore
	accounts       PlatformAccountStore
	candidateStore CandidateStore
	subscriptions  SubscriptionStore
	mailer         Mailer
	dailyStats     SystemDailyStatsStore
}

type createTaskRequest struct {
	Name              string `json:"name"`
	PlatformID        string `json:"platform_id"`
	PlatformAccountID string `json:"platform_account_id"`
	PositionID        string `json:"position_id"`
	Mode              string `json:"mode"`
	MatchLimit        int    `json:"match_limit"`
	EnableSound       bool   `json:"enable_sound"`
	EnableThinking    bool   `json:"enable_thinking"`
}

// NewTaskService 创建任务 API 服务，注入任务元数据和候选人入库所需依赖。
func NewTaskService(auth *AuthService, store TaskStore, positionStore PositionStore, taskLogs TaskLogService, tenantStore TenantStore, accounts PlatformAccountStore, candidateStore CandidateStore, subscriptions SubscriptionStore, mailer Mailer, dailyStats SystemDailyStatsStore) *TaskService {
	return &TaskService{
		auth:           auth,
		store:          store,
		positionStore:  positionStore,
		taskLogs:       taskLogs,
		tenantStore:    tenantStore,
		accounts:       accounts,
		candidateStore: candidateStore,
		subscriptions:  subscriptions,
		mailer:         mailer,
		dailyStats:     dailyStats,
	}
}

// Collection 按请求方法处理任务集合资源。
func (s *TaskService) Collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.List(w, r)
	case http.MethodPost:
		s.Create(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// Create 创建云端任务运行记录。
func (s *TaskService) Create(w http.ResponseWriter, r *http.Request) {
	// 调用认证服务读取当前用户，用于把任务归属到该账号下。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	task, ok := req.toTask(w, session.Email)
	if !ok {
		return
	}
	if !s.applyPositionDefaults(w, session.Email, &task) {
		return
	}

	// 调用任务存储创建任务，后续会替换为 PostgreSQL task_runs 表。
	saved, err := s.store.CreateTask(task)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusBadRequest, "platform account not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create task")
		return
	}

	tenantID, _ := s.getTenantInfo(session.Email)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"task": s.publicTaskRunWithAccount(tenantID, saved),
	})
}

// ensureSubscriptionActive 校验当前用户订阅是否可启动任务。
func (s *TaskService) ensureSubscriptionActive(w http.ResponseWriter, email string) bool {
	if s.subscriptions == nil {
		return true
	}
	subscription, err := s.subscriptions.UserSubscription(email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load subscription")
		return false
	}
	if subscriptionActive(subscription) {
		return true
	}
	writeJSON(w, http.StatusPaymentRequired, map[string]any{
		"ok":           false,
		"error":        "subscription_expired",
		"message":      "会员已到期，请先订阅后再开始任务",
		"subscription": publicSubscription(subscription),
	})
	return false
}

// List 返回当前登录用户的任务列表。
func (s *TaskService) List(w http.ResponseWriter, r *http.Request) {
	// 调用认证服务读取当前用户，用于只返回自己的任务。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	// 调用任务存储读取任务列表，用于任务控制台展示统计摘要。
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	tasks, err := s.store.ListTasks(tenantID, session.Email, isAdmin)
	if err != nil {
		stdlog.Printf("[任务列表] 读取任务失败 user=%s tenant=%s admin=%v err=%v", session.Email, tenantID, isAdmin, err)
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"tasks": s.publicTaskRunsWithAccount(tenantID, tasks),
	})
}

// Detail 返回当前登录用户的单个任务详情。
func (s *TaskService) Detail(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.Delete(w, r)
		return
	}
	if r.Method == http.MethodPut {
		s.Update(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，用于限制只能查看自己的任务。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	taskID := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	if taskID == "" || taskID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	// 调用任务存储读取任务详情，用于后续展开日志和候选人数据。
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	task, err := s.store.TaskByID(tenantID, session.Email, taskID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		stdlog.Printf("[任务详情] 读取任务失败 task=%s user=%s tenant=%s admin=%v err=%v", taskID, session.Email, tenantID, isAdmin, err)
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"task": s.publicTaskRunWithAccount(tenantID, task),
	})
}

func (s *TaskService) Delete(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	if taskID == "" || taskID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	current, err := s.store.TaskByID(tenantID, session.Email, taskID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}
	if current.Status == "running" {
		writeError(w, http.StatusConflict, "running task cannot be deleted")
		return
	}
	if err := s.store.DeleteTask(tenantID, session.Email, taskID, isAdmin); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete task")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

// Update 更新任务创建参数（平台、账号、岗位模板、模式、上限）。
func (s *TaskService) Update(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	if taskID == "" || taskID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	current, err := s.store.TaskByID(tenantID, session.Email, taskID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}
	if current.Status == "running" {
		writeError(w, http.StatusConflict, "running task cannot be edited")
		return
	}

	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	next, ok := req.toTask(w, session.Email)
	if !ok {
		return
	}
	if !s.applyPositionDefaults(w, session.Email, &next) {
		return
	}
	updated, err := s.store.UpdateTask(taskID, next)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusBadRequest, "platform account not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update task")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"task": s.publicTaskRunWithAccount(tenantID, updated),
	})
}

// currentSession 从请求中解析登录会话。
func (s *TaskService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	// 调用认证服务解析请求会话，避免任务 API 自己重复处理 token。
	session, err := s.auth.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return Session{}, false
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return Session{}, false
	}
	return session, true
}

// toTask 将任务创建请求转换为任务模型。
func (r createTaskRequest) toTask(w http.ResponseWriter, userEmail string) (TaskRun, bool) {
	task := TaskRun{
		Name:              strings.TrimSpace(r.Name),
		UserEmail:         userEmail,
		PlatformID:        strings.TrimSpace(r.PlatformID),
		PlatformAccountID: strings.TrimSpace(r.PlatformAccountID),
		PositionID:        strings.TrimSpace(r.PositionID),
		Mode:              strings.TrimSpace(r.Mode),
		MatchLimit:        r.MatchLimit,
		EnableSound:       r.EnableSound,
		EnableThinking:    r.EnableThinking,
	}

	if task.PlatformID == "" {
		writeError(w, http.StatusBadRequest, "platform_id is required")
		return TaskRun{}, false
	}
	if task.Mode == "" {
		task.Mode = "keyword"
	}
	if task.MatchLimit < 0 {
		writeError(w, http.StatusBadRequest, "match_limit must be greater than or equal to 0")
		return TaskRun{}, false
	}
	return task, true
}

// publicTaskRuns 将任务列表转换为前端响应结构。
func publicTaskRuns(items []TaskRun) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicTaskRun(item))
	}
	return result
}

// publicTaskRun 将任务模型转换为前端响应结构。
func publicTaskRun(item TaskRun) map[string]any {
	return map[string]any{
		"id":                  item.ID,
		"platform_id":         item.PlatformID,
		"name":                item.Name,
		"platform_account_id": item.PlatformAccountID,
		"position_id":         item.PositionID,
		"mode":                item.Mode,
		"match_limit":         item.MatchLimit,
		"enable_sound":        item.EnableSound,
		"enable_thinking":     item.EnableThinking,
		"status":              item.Status,
		"scanned_count":       item.ScannedCount,
		"greeted_count":       item.GreetedCount,
		"daily_greeted_count": item.DailyGreetedCount,
		"daily_greeted_date":  item.DailyGreetedDate,
		"today_greeted_count": taskTodayGreetedCount(item),
		"skipped_count":       item.SkippedCount,
		"failed_count":        item.FailedCount,
		"local_task_id":       item.LocalTaskID,
		"created_at":          item.CreatedAt,
		"started_at":          item.StartedAt,
		"finished_at":         item.FinishedAt,
	}
}

// taskTodayGreetedCount 返回任务当天打招呼数，日期不是今天时返回 0。
// item 为任务记录，返回值用于任务列表展示今日统计。
func taskTodayGreetedCount(item TaskRun) int {
	if item.DailyGreetedDate != time.Now().In(time.Local).Format(time.DateOnly) {
		return 0
	}
	if item.DailyGreetedCount < 0 {
		return 0
	}
	return item.DailyGreetedCount
}

func (s *TaskService) publicTaskRunsWithAccount(tenantID string, items []TaskRun) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, s.publicTaskRunWithAccount(tenantID, item))
	}
	return result
}

func (s *TaskService) publicTaskRunWithAccount(tenantID string, item TaskRun) map[string]any {
	result := publicTaskRun(item)
	if item.PlatformAccountID != "" && s.accounts != nil {
		accounts, err := s.accounts.ListPlatformAccounts(tenantID, item.UserEmail, item.PlatformID, false)
		if err == nil {
			for _, account := range accounts {
				if account.ID != item.PlatformAccountID {
					continue
				}
				result["platform_account_name"] = account.DisplayName
				result["platform_account"] = map[string]any{
					"id":               account.ID,
					"platform_id":      account.PlatformID,
					"display_name":     account.DisplayName,
					"local_profile_id": account.LocalProfileID,
					"status":           "available",
					"created_at":       account.CreatedAt,
				}
				break
			}
		}
	}
	if item.PositionID != "" {
		position, err := s.positionStore.PositionByID(tenantID, item.UserEmail, item.PositionID, false)
		if err == nil {
			result["position_name"] = position.Name
			result["position"] = map[string]any{
				"id":               position.ID,
				"name":             position.Name,
				"description":      position.Description,
				"greet_message":    position.GreetMessage,
				"keywords":         position.Keywords,
				"exclude_keywords": position.ExcludeKeywords,
				"is_and_mode":      position.IsAndMode,
				"common_config":    cloneMap(position.CommonConfig),
				"ai_config":        cloneMap(position.AIConfig),
				"keyword_config":   cloneMap(position.KeywordConfig),
				"updated_at":       position.UpdatedAt,
			}
		}
	}
	return result
}

// applyPositionDefaults 根据岗位模板补齐任务模式和默认任务名称。
// email 为当前用户邮箱，task 为待保存任务；岗位不存在时返回 false。
func (s *TaskService) applyPositionDefaults(w http.ResponseWriter, email string, task *TaskRun) bool {
	if task == nil {
		writeError(w, http.StatusBadRequest, "task is required")
		return false
	}
	if strings.TrimSpace(task.PositionID) == "" {
		if strings.TrimSpace(task.Name) == "" {
			task.Name = "未命名任务"
		}
		return true
	}
	position, err := s.positionStore.PositionByID("", email, task.PositionID, false)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusBadRequest, "position not found")
		return false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position")
		return false
	}
	task.Mode = positionDefaultMode(position)
	if strings.TrimSpace(task.Name) == "" {
		task.Name = defaultTaskName(position.Name, task.Mode)
	}
	return true
}

// positionDefaultMode 返回岗位模板配置的默认筛选模式。
// position 为岗位模板，返回 ai 或 keyword。
func positionDefaultMode(position Position) string {
	mode, _ := position.CommonConfig["mode_default"].(string)
	mode = strings.TrimSpace(mode)
	if mode == "keyword" {
		return "keyword"
	}
	return "ai"
}

// defaultTaskName 生成任务默认名称。
// positionName 为岗位模板名称，mode 为筛选模式。
func defaultTaskName(positionName string, mode string) string {
	name := strings.TrimSpace(positionName)
	if name == "" {
		name = "未命名岗位"
	}
	return name + " " + modeLabel(mode)
}

// modeLabel 返回筛选模式中文名称。
// mode 为内部模式值。
func modeLabel(mode string) string {
	if mode == "keyword" {
		return "关键词筛选"
	}
	return "AI筛选"
}

// Run 拒绝旧版云端任务主流程启动。
func (s *TaskService) Run(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	if !s.ensureSubscriptionActive(w, session.Email) {
		return
	}

	taskID := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	taskID = strings.TrimSuffix(taskID, "/run")

	tenantID, isAdmin := s.getTenantInfo(session.Email)
	task, err := s.store.TaskByID(tenantID, session.Email, taskID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}

	if task.Status == "running" {
		stdlog.Printf("[任务开始] 拒绝执行 task=%s user=%s 原因=任务状态已是%s", task.ID, session.Email, task.Status)
		writeError(w, http.StatusBadRequest, "task is already "+task.Status)
		return
	}

	stdlog.Printf("[任务开始] 拒绝旧云端主流程 task=%s user=%s", task.ID, session.Email)
	writeJSON(w, http.StatusConflict, map[string]any{
		"ok":      false,
		"code":    http.StatusConflict,
		"message": "任务主流程已迁移到本地程序，请从本地程序启动任务",
		"msg":     "任务主流程已迁移到本地程序，请从本地程序启动任务",
	})
}

// Stop 停止正在运行的云端任务。
// 停止请求会取消任务上下文，并把任务状态更新为 stopped。
func (s *TaskService) Stop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	taskID = strings.TrimSuffix(taskID, "/stop")
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	task, err := s.store.TaskByID(tenantID, session.Email, taskID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}
	stdlog.Printf("[任务停止] 收到停止请求 task=%s user=%s", task.ID, session.Email)
	if task.Status != "stopped" {
		_ = s.store.UpdateTaskStatus(task.ID, "stopped")
		_ = s.taskLogs.WriteLog(task.ID, task.UserEmail, "warn", "任务已停止")
		s.sendTaskStatusNotice(task, "stopped", "")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": "stopped",
	})
}

// SyncStatus 接收本地程序同步的任务状态。
// 请求体中的 status 只允许 completed、stopped、running，用于避免完成任务被误标记为停止。
func (s *TaskService) SyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	taskID = strings.TrimSuffix(taskID, "/status")
	taskID = strings.Trim(taskID, "/")
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	status := strings.ToLower(strings.TrimSpace(payload.Status))
	if status != "completed" && status != "stopped" && status != "running" {
		writeError(w, http.StatusBadRequest, "unsupported status")
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	task, err := s.store.TaskByID(tenantID, session.Email, taskID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed load task")
		return
	}
	if task.Status != status {
		_ = s.store.UpdateTaskStatus(task.ID, status)
		if status == "completed" {
			_ = s.taskLogs.WriteLog(task.ID, task.UserEmail, "info", "任务已完成")
			s.sendTaskStatusNotice(task, "completed", "")
		}
		if status == "stopped" {
			_ = s.taskLogs.WriteLog(task.ID, task.UserEmail, "warn", "任务已停止")
			s.sendTaskStatusNotice(task, "stopped", "")
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": status,
	})
}

// sendTaskStatusNotice 发送任务结束或失败邮件提醒。
func (s *TaskService) sendTaskStatusNotice(task TaskRun, status string, errorMessage string) {
	if s.mailer == nil || strings.TrimSpace(task.UserEmail) == "" {
		return
	}
	tenantID := ""
	if s.tenantStore != nil {
		tenantID, _ = s.getTenantInfo(task.UserEmail)
	}
	notice := TaskStatusNotice{
		TaskID:       task.ID,
		Status:       status,
		StatusLabel:  taskStatusNoticeLabel(status),
		PlatformID:   task.PlatformID,
		Mode:         task.Mode,
		MatchLimit:   task.MatchLimit,
		FinishedAt:   time.Now(),
		ErrorMessage: strings.TrimSpace(errorMessage),
	}
	if tenantID != "" && task.PlatformAccountID != "" && s.accounts != nil {
		if accounts, err := s.accounts.ListPlatformAccounts(tenantID, task.UserEmail, task.PlatformID, false); err == nil {
			for _, account := range accounts {
				if account.ID == task.PlatformAccountID {
					notice.PlatformAccount = account.DisplayName
					break
				}
			}
		}
	}
	if notice.PlatformAccount == "" {
		notice.PlatformAccount = task.PlatformAccountID
	}
	if current, err := s.store.TaskByID(tenantID, task.UserEmail, task.ID, true); err == nil {
		notice.ScannedCount = current.ScannedCount
		notice.GreetedCount = current.GreetedCount
		notice.SkippedCount = current.SkippedCount
		notice.FailedCount = current.FailedCount
	}
	if err := s.mailer.SendTaskStatus(task.UserEmail, notice); err != nil {
		stdlog.Printf("[任务邮件] 发送任务状态提醒失败 task=%s user=%s err=%v", task.ID, task.UserEmail, err)
	}
}

// taskStatusNoticeLabel 返回任务状态邮件里的中文状态。
func taskStatusNoticeLabel(status string) string {
	switch status {
	case "failed":
		return "任务失败"
	case "stopped":
		return "任务已停止"
	case "completed":
		return "任务完成"
	default:
		return "任务结束"
	}
}

func (s *TaskService) getTenantInfo(email string) (string, bool) {
	t, err := s.tenantStore.GetOrCreateTenant(email)
	if err != nil {
		return "", false
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(t.ID, email)
	return t.ID, isAdmin
}

// FailNotice 接收本地代理发送的任务失败通知，发送邮件提醒。
// 请求体中包含 task_id（云端任务 ID）和 error_message（失败原因）。
// 此接口由本地代理调用，必须携带当前登录用户 token。
func (s *TaskService) FailNotice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	var payload struct {
		TaskID       string `json:"task_id"`
		ErrorMessage string `json:"error_message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	taskID := strings.TrimSpace(payload.TaskID)
	errorMessage := strings.TrimSpace(payload.ErrorMessage)
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task_id required")
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	task, err := s.store.TaskByID(tenantID, session.Email, taskID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}
	_ = s.store.UpdateTaskStatus(task.ID, "failed")
	if task.UserEmail == "" {
		task.UserEmail = session.Email
	}
	if s.mailer == nil {
		writeError(w, http.StatusServiceUnavailable, "mailer not configured")
		return
	}
	notice := TaskStatusNotice{
		TaskID:       task.ID,
		Status:       "failed",
		StatusLabel:  "任务失败",
		PlatformID:   task.PlatformID,
		Mode:         task.Mode,
		MatchLimit:   task.MatchLimit,
		FinishedAt:   time.Now(),
		ErrorMessage: errorMessage,
	}
	if err := s.mailer.SendTaskStatus(task.UserEmail, notice); err != nil {
		stdlog.Printf("[任务邮件] 发送失败通知邮件失败 task=%s user=%s err=%v", task.ID, task.UserEmail, err)
		writeError(w, http.StatusInternalServerError, "failed to send email")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": "notified",
	})
}
