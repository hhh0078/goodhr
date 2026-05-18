// 本文件负责提供云端任务创建和查询的 HTTP API。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// TaskService 处理任务创建、列表、详情和执行请求。
type TaskService struct {
	auth           *AuthService
	store          TaskStore
	systemConfigs  SystemConfigStore
	positionStore  PositionStore
	taskLogs       TaskLogService
}

type createTaskRequest struct {
	PlatformID        string `json:"platform_id"`
	PlatformAccountID string `json:"platform_account_id"`
	PositionID        string `json:"position_id"`
	Mode              string `json:"mode"`
	MatchLimit        int    `json:"match_limit"`
}

// NewTaskService 创建任务 API 服务，注入认证、存储和执行所需依赖。
func NewTaskService(auth *AuthService, store TaskStore, systemConfigs SystemConfigStore, positionStore PositionStore, taskLogs TaskLogService) *TaskService {
	return &TaskService{
		auth:          auth,
		store:         store,
		systemConfigs: systemConfigs,
		positionStore: positionStore,
		taskLogs:      taskLogs,
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

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"task": publicTaskRun(saved),
	})
}

// List 返回当前登录用户的任务列表。
func (s *TaskService) List(w http.ResponseWriter, r *http.Request) {
	// 调用认证服务读取当前用户，用于只返回自己的任务。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	// 调用任务存储读取任务列表，用于任务控制台展示统计摘要。
	tasks, err := s.store.ListTasks(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"tasks": publicTaskRuns(tasks),
	})
}

// Detail 返回当前登录用户的单个任务详情。
func (s *TaskService) Detail(w http.ResponseWriter, r *http.Request) {
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
	task, err := s.store.TaskByID(session.Email, taskID)
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
		"task": publicTaskRun(task),
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
		UserEmail:         userEmail,
		PlatformID:        strings.TrimSpace(r.PlatformID),
		PlatformAccountID: strings.TrimSpace(r.PlatformAccountID),
		PositionID:        strings.TrimSpace(r.PositionID),
		Mode:              strings.TrimSpace(r.Mode),
		MatchLimit:        r.MatchLimit,
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
		"platform_account_id": item.PlatformAccountID,
		"position_id":         item.PositionID,
		"mode":                item.Mode,
		"match_limit":         item.MatchLimit,
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

// runTaskRequest 定义任务执行请求。
type runTaskRequest struct {
	AgentBaseURL string `json:"agent_base_url"`
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

	taskID := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	taskID = strings.TrimSuffix(taskID, "/run")

	task, err := s.store.TaskByID(session.Email, taskID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}

	var req runTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.AgentBaseURL == "" {
		writeError(w, http.StatusBadRequest, "agent_base_url is required")
		return
	}

	// 异步执行任务，不阻塞 HTTP 响应
	go s.executeTask(task, req.AgentBaseURL)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": "running",
	})
}

// executeTask 在 goroutine 中执行任务编排流程。
func (s *TaskService) executeTask(task TaskRun, agentBaseURL string) {
	ctx := context.Background()

	log := func(level, message string) {
		// 调用任务日志存储写入日志，供前端展示运行摘要
		_ = s.taskLogs.WriteLog(task.ID, level, message)
	}

	log("info", fmt.Sprintf("任务 %s 开始执行", task.ID))

	// 读取平台配置
	cfg, err := s.systemConfigs.Get("platform." + task.PlatformID)
	if err != nil {
		log("error", fmt.Sprintf("读取平台配置失败: %v", err))
		return
	}

	platformCfg, err := ParsePlatformConfig(cfg.ConfigValue)
	if err != nil {
		log("error", fmt.Sprintf("解析平台配置失败: %v", err))
		return
	}

	// 读取岗位信息
	position := map[string]any{}
	if task.PositionID != "" {
		pos, err := s.positionStore.PositionByID(sessionEmail(task.UserEmail), task.PositionID)
		if err == nil {
			// 确保位置不为 nil
			position = map[string]any{
				"name":     pos.Name,
				"keywords": pos.Keywords,
				"exclude":  pos.ExcludeKeywords,
			}
		}
	}

	executor := NewTaskExecutor(task, platformCfg, position, agentBaseURL, log)
	if err := executor.Run(ctx); err != nil {
		log("error", fmt.Sprintf("任务执行失败: %v", err))
	} else {
		log("info", "任务执行完成")
	}
}

// sessionEmail 模拟从 session 获取 email（用于内部调用）。
func sessionEmail(email string) string { return email }
