// Package api 文件作用：提供控制台使用的全局本地日志接口。
package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"goodhr5/local-agent-go-new/internal/flow/lifecycle"
	"goodhr5/local-agent-go-new/internal/storage"
)

// handleLocalLogs 读取、追加或清空全局本地步骤日志。
func (s *Server) handleLocalLogs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		logs, err := s.store.ListTaskLogs(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOG_LIST_FAILED", err)
			return
		}
		task, err := s.latestTaskSnapshot(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TASK_STATUS_READ_FAILED", err)
			return
		}
		writeSuccess(w, http.StatusOK, struct {
			Logs []storage.TaskLog       `json:"logs"`
			Task *lifecycle.TaskSnapshot `json:"task"`
		}{Logs: logs, Task: task})
	case http.MethodPost:
		var request struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
			return
		}
		item, err := s.store.SaveTaskLog(r.Context(), storage.TaskLog{
			Level: request.Level, Message: request.Message,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "LOG_SAVE_FAILED", err)
			return
		}
		writeSuccess(w, http.StatusOK, struct {
			Log storage.TaskLog `json:"log"`
		}{Log: item})
	case http.MethodDelete:
		if err := s.store.ClearTaskLogs(r.Context()); err != nil {
			writeError(w, http.StatusBadRequest, "LOG_CLEAR_FAILED", err)
			return
		}
		writeSuccess(w, http.StatusOK, struct {
			Cleared bool `json:"cleared"`
		}{Cleared: true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", fmt.Errorf("这个日志接口不支持当前请求方式"))
	}
}

// latestTaskSnapshot 返回最近任务状态；没有历史任务时返回空状态。
func (s *Server) latestTaskSnapshot(ctx context.Context) (*lifecycle.TaskSnapshot, error) {
	task, err := s.store.LatestTask(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapshot := lifecycle.TaskSnapshot{TaskRun: task}
	if s.runner != nil {
		snapshot = s.runner.TaskSnapshot(task)
	}
	return &snapshot, nil
}
