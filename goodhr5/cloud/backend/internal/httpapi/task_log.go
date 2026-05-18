// 本文件负责提供云端任务日志摘要的 HTTP API。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// TaskLogService 处理任务日志写入和读取请求。
type TaskLogService struct {
	auth     *AuthService
	tasks    TaskStore
	logStore TaskLogStore
}

type addTaskLogRequest struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// NewTaskLogService 创建任务日志 API 服务，并注入认证、任务存储和日志存储。
func NewTaskLogService(auth *AuthService, tasks TaskStore, logStore TaskLogStore) *TaskLogService {
	return &TaskLogService{
		auth:     auth,
		tasks:    tasks,
		logStore: logStore,
	}
}

// WriteLog 写入任务日志摘要（内部调用，不验证 session）。
func (s *TaskLogService) WriteLog(taskID, level, message string) error {
	_, err := s.logStore.AddTaskLog(TaskLog{
		TaskID:  taskID,
		Level:   level,
		Message: message,
	})
	return err
}

// Collection 按请求方法处理任务日志集合资源。
func (s *TaskLogService) Collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.List(w, r)
	case http.MethodPost:
		s.Add(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// Add 写入一条任务日志摘要。
func (s *TaskLogService) Add(w http.ResponseWriter, r *http.Request) {
	// 调用认证服务读取当前用户，用于将日志归属到该账号下。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	taskID, ok := taskIDFromLogsPath(w, r.URL.Path)
	if !ok {
		return
	}

	// 调用任务存储确认任务归属，避免写入其他用户任务日志。
	if _, err := s.tasks.TaskByID("", session.Email, taskID, true); errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}

	var req addTaskLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	// 调用任务日志存储写入摘要，候选人详情仍保存在本地 Agent。
	log, err := s.logStore.AddTaskLog(TaskLog{
		TaskID:    taskID,
		UserEmail: session.Email,
		Level:     strings.TrimSpace(req.Level),
		Message:   message,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add task log")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":  true,
		"log": publicTaskLog(log),
	})
}

// List 返回某个任务的日志摘要列表。
func (s *TaskLogService) List(w http.ResponseWriter, r *http.Request) {
	// 调用认证服务读取当前用户，用于只返回自己的任务日志。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	taskID, ok := taskIDFromLogsPath(w, r.URL.Path)
	if !ok {
		return
	}

	// 调用任务存储确认任务归属，避免读取其他用户任务日志。
	if _, err := s.tasks.TaskByID("", session.Email, taskID, true); errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}

	// 调用任务日志存储读取摘要，用于前端展开任务卡片。
	logs, err := s.logStore.ListTaskLogs(session.Email, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list task logs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"logs": publicTaskLogs(logs),
	})
}

// currentSession 从请求中解析登录会话。
func (s *TaskLogService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	// 调用认证服务解析请求会话，避免日志 API 自己重复处理 token。
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

// taskIDFromLogsPath 从日志接口路径中解析任务 ID。
func taskIDFromLogsPath(w http.ResponseWriter, path string) (string, bool) {
	trimmed := strings.TrimPrefix(path, "/api/tasks/")
	if trimmed == path {
		writeError(w, http.StatusBadRequest, "task id is required")
		return "", false
	}
	taskID := strings.TrimSuffix(trimmed, "/logs")
	if taskID == "" || taskID == trimmed {
		writeError(w, http.StatusBadRequest, "task log path is invalid")
		return "", false
	}
	return taskID, true
}

// publicTaskLogs 将任务日志列表转换为前端响应结构。
func publicTaskLogs(items []TaskLog) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicTaskLog(item))
	}
	return result
}

// publicTaskLog 将任务日志模型转换为前端响应结构。
func publicTaskLog(item TaskLog) map[string]any {
	return map[string]any{
		"id":         item.ID,
		"task_id":    item.TaskID,
		"level":      item.Level,
		"message":    item.Message,
		"created_at": item.CreatedAt,
	}
}
