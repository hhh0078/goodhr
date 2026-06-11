// 本文件负责提供云端任务创建和查询的 HTTP API。
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	stdlog "log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TaskService 处理任务创建、列表、详情和执行请求。
type TaskService struct {
	auth           *AuthService
	store          TaskStore
	systemConfigs  SystemConfigStore
	positionStore  PositionStore
	taskLogs       TaskLogService
	aiConfigStore  AIConfigStore
	userPrefsStore UserPreferencesStore
	tenantStore    TenantStore
	cookieStore    CookieStore
	candidateStore CandidateStore
	agentWS        *AgentWSHub
	subscriptions  SubscriptionStore
	mailer         Mailer
	runningMu      sync.Mutex
	runningCancels map[string]context.CancelFunc
}

type createTaskRequest struct {
	Name              string `json:"name"`
	PlatformID        string `json:"platform_id"`
	PlatformAccountID string `json:"platform_account_id"`
	PositionID        string `json:"position_id"`
	Mode              string `json:"mode"`
	MatchLimit        int    `json:"match_limit"`
	EnableSound       bool   `json:"enable_sound"`
}

// NewTaskService 创建任务 API 服务，注入认证、存储和执行所需依赖。
func NewTaskService(auth *AuthService, store TaskStore, systemConfigs SystemConfigStore, positionStore PositionStore, taskLogs TaskLogService, aiConfigStore AIConfigStore, userPrefsStore UserPreferencesStore, tenantStore TenantStore, cookieStore CookieStore, candidateStore CandidateStore, agentWS *AgentWSHub, subscriptions SubscriptionStore, mailer Mailer) *TaskService {
	return &TaskService{
		auth:           auth,
		store:          store,
		systemConfigs:  systemConfigs,
		positionStore:  positionStore,
		taskLogs:       taskLogs,
		aiConfigStore:  aiConfigStore,
		userPrefsStore: userPrefsStore,
		tenantStore:    tenantStore,
		cookieStore:    cookieStore,
		candidateStore: candidateStore,
		agentWS:        agentWS,
		subscriptions:  subscriptions,
		mailer:         mailer,
		runningCancels: map[string]context.CancelFunc{},
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
		"task": s.publicTaskRunWithAccount(tenantID, saved, TaskCountSummary{}),
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
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}
	todayStart := startOfToday()
	todaySummaries, err := s.taskLogs.logStore.SummarizeTaskCounts(tenantID, session.Email, isAdmin, &todayStart)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to summarize task stats")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"tasks": s.publicTaskRunsWithAccount(tenantID, tasks, todaySummaries),
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
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"task": s.publicTaskRunWithAccount(tenantID, task, TaskCountSummary{}),
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
		"task": s.publicTaskRunWithAccount(tenantID, updated, TaskCountSummary{}),
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
	}

	if task.PlatformID == "" {
		writeError(w, http.StatusBadRequest, "platform_id is required")
		return TaskRun{}, false
	}
	if task.PlatformAccountID == "" {
		writeError(w, http.StatusBadRequest, "platform_account_id is required")
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
		"status":              item.Status,
		"scanned_count":       item.ScannedCount,
		"greeted_count":       item.GreetedCount,
		"skipped_count":       item.SkippedCount,
		"failed_count":        item.FailedCount,
		"local_task_id":       item.LocalTaskID,
		"created_at":          item.CreatedAt,
		"started_at":          item.StartedAt,
		"finished_at":         item.FinishedAt,
	}
}

func (s *TaskService) publicTaskRunsWithAccount(tenantID string, items []TaskRun, todaySummaries map[string]TaskCountSummary) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, s.publicTaskRunWithAccount(tenantID, item, todaySummaries[item.ID]))
	}
	return result
}

func (s *TaskService) publicTaskRunWithAccount(tenantID string, item TaskRun, todaySummary TaskCountSummary) map[string]any {
	result := publicTaskRun(item)
	result["today_scanned_count"] = todaySummary.ScannedCount
	result["today_greeted_count"] = todaySummary.GreetedCount
	result["today_skipped_count"] = todaySummary.SkippedCount
	result["today_failed_count"] = todaySummary.FailedCount
	if item.PlatformAccountID != "" && tenantID != "" {
		account, err := s.cookieStore.GetByID(tenantID, item.PlatformAccountID)
		if err == nil {
			result["platform_account_name"] = account.DisplayName
			result["platform_account"] = map[string]any{
				"id":           account.ID,
				"platform_id":  account.PlatformID,
				"display_name": account.DisplayName,
				"status":       account.Status,
				"updated_at":   account.UpdatedAt,
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

// Run 启动任务异步执行。
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

	if s.agentWS == nil || !s.agentWS.IsOnline(session.Email) {
		stdlog.Printf("[任务开始] 拒绝执行 task=%s user=%s 原因=本地WS未连接", task.ID, session.Email)
		writeError(w, http.StatusConflict, "local agent websocket is not connected")
		return
	}

	if task.Status == "running" {
		stdlog.Printf("[任务开始] 拒绝执行 task=%s user=%s 原因=任务状态已是%s", task.ID, session.Email, task.Status)
		writeError(w, http.StatusBadRequest, "task is already "+task.Status)
		return
	}

	// 异步执行任务，不阻塞 HTTP 响应
	stdlog.Printf("[任务开始] 已接受执行 task=%s user=%s platform=%s account=%s position=%s mode=%s", task.ID, session.Email, task.PlatformID, task.PlatformAccountID, task.PositionID, task.Mode)
	go s.executeTask(task)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": "running",
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
	s.cancelTask(task.ID)
	stdlog.Printf("[任务停止] 收到停止请求 task=%s user=%s", task.ID, session.Email)
	if task.Status != "stopped" {
		_ = s.store.UpdateTaskStatus(task.ID, "stopped")
		_ = s.taskLogs.WriteLog(task.ID, task.UserEmail, "warn", "任务已停止")
		s.sendTaskStatusNotice(task, "stopped", "")
	}
	s.releaseTaskCookieIfOwned(tenantID, task, "停止任务时释放占用的 cookie", func(level, message string) {
		_ = s.taskLogs.WriteLog(task.ID, task.UserEmail, level, message)
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": "stopped",
	})
}

// executeTask 在 goroutine 中执行任务编排流程。
func (s *TaskService) executeTask(task TaskRun) {
	ctx, cancel := context.WithCancel(context.Background())
	if !s.registerTaskCancel(task.ID, cancel) {
		cancel()
		stdlog.Printf("[任务流程] task=%s 注册取消器失败：任务已在运行", task.ID)
		_ = s.taskLogs.WriteLog(task.ID, task.UserEmail, "warn", "任务已在运行中")
		return
	}
	defer s.unregisterTaskCancel(task.ID)

	log := func(level, message string) {
		stdlog.Printf("[任务流程] task=%s level=%s message=%s", task.ID, level, message)
		// 调用任务日志存储写入日志，供前端展示运行摘要
		_ = s.taskLogs.WriteLog(task.ID, task.UserEmail, level, message)
	}

	log("info", fmt.Sprintf("任务 %s 开始执行", task.ID))
	disconnectReason := "task_finished"
	var releaseClaim func()
	defer func() {
		if releaseClaim != nil {
			releaseClaim()
		}
		if err := s.taskLogs.FlushLogs(task.ID, task.UserEmail); err != nil {
			stdlog.Printf("[任务流程] task=%s 刷新缓存日志失败: %v", task.ID, err)
		}
	}()
	defer func() {
		s.notifyLocalAgentDisconnect(task, disconnectReason, log)
	}()

	tenantID := ""
	if s.tenantStore != nil {
		tenantID, _ = s.getTenantInfo(task.UserEmail)
	}
	claimedCookie, release, err := s.claimTaskCookie(tenantID, task, log)
	if err != nil {
		disconnectReason = "task_failed"
		log("error", fmt.Sprintf("准备任务 cookie 失败: %v", err))
		_ = s.store.UpdateTaskStatus(task.ID, "failed")
		return
	}
	releaseClaim = release

	// 更新任务状态为 running
	_ = s.store.UpdateTaskStatus(task.ID, "running")

	// 读取平台配置
	cfg, err := s.systemConfigs.Get("platform." + task.PlatformID)
	if err != nil {
		disconnectReason = "task_failed"
		log("error", fmt.Sprintf("读取平台配置失败: %v", err))
		_ = s.store.UpdateTaskStatus(task.ID, "failed")
		return
	}

	platformCfg, err := ParsePlatformConfig(cfg.ConfigValue)
	if err != nil {
		disconnectReason = "task_failed"
		log("error", fmt.Sprintf("解析平台配置失败: %v", err))
		_ = s.store.UpdateTaskStatus(task.ID, "failed")
		return
	}

	// 读取岗位信息
	position := map[string]any{}
	if task.PositionID != "" {
		pos, err := s.positionStore.PositionByID("", sessionEmail(task.UserEmail), task.PositionID, true)
		if err == nil {
			// 确保位置不为 nil
			position = map[string]any{
				"name":           pos.Name,
				"keywords":       pos.Keywords,
				"exclude":        pos.ExcludeKeywords,
				"is_and_mode":    pos.IsAndMode,
				"common_config":  pos.CommonConfig,
				"ai_config":      pos.AIConfig,
				"keyword_config": pos.KeywordConfig,
			}
		}
	}

	// 读取 AI 配置（供 AI 筛选模式和 Boss 图片详情识别使用）
	var aiConfig AIConfig
	if taskRequiresAIConfig(task) && s.aiConfigStore != nil {
		cfg, err := s.aiConfigStore.UserConfig(task.UserEmail)
		if err != nil {
			disconnectReason = "task_failed"
			log("error", "当前用户未配置 AI，请先在个人配置中填写 AI 服务参数")
			_ = s.store.UpdateTaskStatus(task.ID, "failed")
			return
		}
		if !cfg.Enabled {
			disconnectReason = "task_failed"
			log("error", "当前用户 AI 配置未启用，请先在个人配置中启用 AI")
			_ = s.store.UpdateTaskStatus(task.ID, "failed")
			return
		}
		aiConfig = cfg
	}
	defaultPrompts := loadDefaultPrompts(s.systemConfigs)
	userPrefs := DefaultUserPreferences()
	if s.userPrefsStore != nil {
		if prefs, err := s.userPrefsStore.UserPreferences(task.UserEmail); err == nil {
			userPrefs = prefs
		}
	}

	executor := NewTaskExecutor(task, platformCfg, position, s.agentWS, aiConfig, defaultPrompts, userPrefs, claimedCookie, s.candidateStore, log, func(scanned, greeted, skipped, failed int) {
		if err := s.store.IncrementTaskCounts(task.ID, scanned, greeted, skipped, failed); err != nil {
			log("warn", fmt.Sprintf("更新任务统计失败: %v", err))
		}
	})
	if err := executor.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			disconnectReason = "task_stopped"
			log("warn", "任务已取消")
			_ = s.store.UpdateTaskStatus(task.ID, "stopped")
			return
		}
		disconnectReason = "task_failed"
		errMessage := fmt.Sprintf("任务执行失败: %v", err)
		log("error", errMessage)
		_ = s.store.UpdateTaskStatus(task.ID, "failed")
		s.sendTaskStatusNotice(task, "failed", errMessage)
	} else {
		disconnectReason = "task_finished"
		log("info", "本轮任务执行完成，可再次开始")
		_ = s.store.UpdateTaskStatus(task.ID, "stopped")
		s.sendTaskStatusNotice(task, "stopped", "")
	}
}

// taskRequiresAIConfig 判断任务是否必须配置 AI。
// Boss 平台详情需要图片 AI 识别，AI 模式也必须配置 AI。
func taskRequiresAIConfig(task TaskRun) bool {
	return strings.EqualFold(strings.TrimSpace(task.PlatformID), "boss") ||
		strings.EqualFold(strings.TrimSpace(task.Mode), "ai")
}

// notifyLocalAgentDisconnect 通知当前用户的本地程序断开任务 WebSocket。
// reason 用于记录断开原因，logFn 用于把通知结果写入任务日志。
func (s *TaskService) notifyLocalAgentDisconnect(task TaskRun, reason string, logFn func(string, string)) {
	if s.agentWS == nil || !s.agentWS.IsOnline(task.UserEmail) {
		return
	}
	payload := map[string]any{
		"path": "/api/v1/ws/disconnect",
		"body": map[string]any{
			"reason": reason,
		},
	}
	_, err := s.agentWS.SendCommand(task.UserEmail, AgentWSMessage{
		Type:    "local.http.post",
		TaskID:  task.ID,
		Payload: payload,
	}, 1)
	if err != nil {
		message := fmt.Sprintf("通知本地程序断开 WS 失败: %v", err)
		if logFn != nil {
			logFn("warn", message)
		} else {
			stdlog.Printf("[任务流程] task=%s %s", task.ID, message)
		}
		return
	}
	if logFn != nil {
		logFn("info", "已通知本地程序断开 WS")
	}
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
	if tenantID != "" && task.PlatformAccountID != "" && s.cookieStore != nil {
		if account, err := s.cookieStore.GetByID(tenantID, task.PlatformAccountID); err == nil {
			notice.PlatformAccount = account.DisplayName
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
	if status == "failed" {
		return "任务失败"
	}
	return "任务结束"
}

func (s *TaskService) registerTaskCancel(taskID string, cancel context.CancelFunc) bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	if _, exists := s.runningCancels[taskID]; exists {
		return false
	}
	s.runningCancels[taskID] = cancel
	return true
}

func (s *TaskService) cancelTask(taskID string) {
	s.runningMu.Lock()
	cancel := s.runningCancels[taskID]
	s.runningMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *TaskService) unregisterTaskCancel(taskID string) {
	s.runningMu.Lock()
	delete(s.runningCancels, taskID)
	s.runningMu.Unlock()
}

func (s *TaskService) getTenantInfo(email string) (string, bool) {
	t, err := s.tenantStore.GetOrCreateTenant(email)
	if err != nil {
		return "", false
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(t.ID, email)
	return t.ID, isAdmin
}

func startOfToday() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// sessionEmail 模拟从 session 获取 email（用于内部调用）。
func sessionEmail(email string) string { return email }

func (s *TaskService) claimTaskCookie(tenantID string, task TaskRun, log func(level, message string)) (*claimedTaskCookie, func(), error) {
	if tenantID == "" || task.PlatformAccountID == "" || s.cookieStore == nil {
		return nil, nil, nil
	}

	rec, err := s.cookieStore.GetByID(tenantID, task.PlatformAccountID)
	if err != nil {
		return nil, nil, err
	}
	if rec.Status == "in_use" && (!rec.UsedByTaskID.Valid || rec.UsedByTaskID.String != task.ID) {
		return nil, nil, fmt.Errorf("该平台账号正在被其他任务占用")
	}
	if rec.Status == "expired" {
		return nil, nil, fmt.Errorf("该平台账号登录已过期，请重新登录后再开始任务")
	}
	if rec.Status != "in_use" || !rec.UsedByTaskID.Valid || rec.UsedByTaskID.String != task.ID {
		if err := s.cookieStore.UpdateStatus(tenantID, rec.ID, "in_use", task.ID); err != nil {
			return nil, nil, err
		}
	}
	log("info", fmt.Sprintf("已锁定任务 cookie：账号=%s cookie=%s", rec.DisplayName, rec.ID))

	released := false
	release := func() {
		if released {
			return
		}
		released = true
		s.releaseTaskCookieIfOwned(tenantID, task, "任务结束释放 cookie", log)
	}

	return &claimedTaskCookie{
		CookieID:      rec.ID,
		DisplayName:   rec.DisplayName,
		EncryptedData: base64.StdEncoding.EncodeToString(rec.EncryptedData),
		EncryptedKeys: rec.EncryptedKeys,
	}, release, nil
}

func (s *TaskService) releaseTaskCookieIfOwned(tenantID string, task TaskRun, reason string, log func(level, message string)) {
	if tenantID == "" || task.PlatformAccountID == "" || s.cookieStore == nil {
		return
	}
	current, err := s.cookieStore.GetByID(tenantID, task.PlatformAccountID)
	if err != nil {
		if log != nil {
			log("warn", fmt.Sprintf("%s失败：读取 cookie 失败 cookie=%s err=%v", reason, task.PlatformAccountID, err))
		}
		return
	}
	if current.Status == "expired" {
		if log != nil {
			log("info", fmt.Sprintf("任务 cookie 已标记过期，跳过恢复可用状态：账号=%s cookie=%s", current.DisplayName, current.ID))
		}
		return
	}
	if current.Status != "in_use" {
		return
	}
	if !current.UsedByTaskID.Valid || current.UsedByTaskID.String != task.ID {
		if log != nil {
			log("warn", fmt.Sprintf("跳过释放非当前任务占用的 cookie：账号=%s cookie=%s used_by_task=%s", current.DisplayName, current.ID, current.UsedByTaskID.String))
		}
		return
	}
	if err := s.cookieStore.UpdateStatus(tenantID, current.ID, "available", ""); err != nil {
		if log != nil {
			log("error", fmt.Sprintf("释放任务 cookie 失败：cookie=%s err=%v", current.ID, err))
		}
		return
	}
	if log != nil {
		log("info", fmt.Sprintf("%s：账号=%s cookie=%s", reason, current.DisplayName, current.ID))
	}
}

// FailNotice 接收本地代理发送的任务失败通知，发送邮件提醒。
// 请求体中包含 task_id（云端任务 ID）和 error_message（失败原因）。
// 此接口由本地代理调用，不需要用户认证。
func (s *TaskService) FailNotice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	task, err := s.store.TaskByID("", "", taskID, true)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}
	_ = s.store.UpdateTaskStatus(task.ID, "failed")
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
