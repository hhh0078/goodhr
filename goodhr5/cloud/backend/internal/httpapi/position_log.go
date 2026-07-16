// 本文件负责提供云端岗位日志摘要的 HTTP API。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// PositionLogService 处理岗位日志写入和读取请求。
type PositionLogService struct {
	auth        *AuthService
	positions   PositionStore
	logStore    PositionLogStore
	tenantStore TenantStore
}

type addPositionLogRequest struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// NewPositionLogService 创建岗位日志 API 服务，并注入认证、岗位存储和日志存储。
func NewPositionLogService(auth *AuthService, positions PositionStore, logStore PositionLogStore, tenantStore TenantStore) *PositionLogService {
	return &PositionLogService{
		auth:        auth,
		positions:   positions,
		logStore:    logStore,
		tenantStore: tenantStore,
	}
}

// WriteLog 写入岗位日志摘要（内部调用，不验证 session）。
func (s *PositionLogService) WriteLog(positionID, userEmail, level, message string) error {
	_, err := s.logStore.AddPositionLog(PositionLog{
		PositionID: positionID,
		UserEmail:  userEmail,
		Level:      level,
		Message:    message,
	})
	return err
}

// FlushLogs 将指定岗位的缓存日志写入持久化存储。
func (s *PositionLogService) FlushLogs(positionID, userEmail string) error {
	flusher, ok := s.logStore.(PositionLogFlushStore)
	if !ok {
		return nil
	}
	return flusher.FlushPositionLogs(positionID, userEmail)
}

// Collection 按请求方法处理岗位日志集合资源。
func (s *PositionLogService) Collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.List(w, r)
	case http.MethodPost:
		s.Add(w, r)
	case http.MethodDelete:
		s.Clear(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// Add 写入一条岗位日志摘要。
func (s *PositionLogService) Add(w http.ResponseWriter, r *http.Request) {
	// 调用认证服务读取当前用户，用于将日志归属到该账号下。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	positionID, ok := positionIDFromLogsPath(w, r.URL.Path)
	if !ok {
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)

	// 调用岗位存储确认岗位归属，避免写入其他用户岗位日志。
	if _, err := s.positions.PositionByID(tenantID, session.Email, positionID, isAdmin); errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position")
		return
	}

	var req addPositionLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	// 调用岗位日志存储写入摘要，候选人详情仍保存在本地 Agent。
	log, err := s.logStore.AddPositionLog(PositionLog{
		PositionID: positionID,
		UserEmail:  session.Email,
		Level:      strings.TrimSpace(req.Level),
		Message:    message,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add position log")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":  true,
		"log": publicPositionLog(log),
	})
}

// List 返回某个岗位的日志摘要列表。
func (s *PositionLogService) List(w http.ResponseWriter, r *http.Request) {
	// 调用认证服务读取当前用户，用于只返回自己的岗位日志。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	positionID, ok := positionIDFromLogsPath(w, r.URL.Path)
	if !ok {
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)

	// 调用岗位存储确认岗位归属，避免读取其他用户岗位日志。
	if _, err := s.positions.PositionByID(tenantID, session.Email, positionID, isAdmin); errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position")
		return
	}

	// 调用岗位日志存储读取摘要，用于前端展开岗位卡片。
	logQuery, err := parsePositionLogQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	logs, hasMore, err := s.logStore.ListPositionLogs(tenantID, session.Email, positionID, isAdmin, logQuery)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list position logs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"logs":     publicPositionLogs(logs),
		"has_more": hasMore,
	})
}

// Clear 清空某个岗位的日志摘要。
func (s *PositionLogService) Clear(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	positionID, ok := positionIDFromLogsPath(w, r.URL.Path)
	if !ok {
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)

	if _, err := s.positions.PositionByID(tenantID, session.Email, positionID, isAdmin); errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position")
		return
	}

	if err := s.logStore.ClearPositionLogs(tenantID, session.Email, positionID, isAdmin); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear position logs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func parsePositionLogQuery(r *http.Request) (PositionLogQuery, error) {
	values := r.URL.Query()
	result := PositionLogQuery{Limit: normalizePositionLogLimit(0)}
	if raw := strings.TrimSpace(values.Get("since")); raw != "" {
		value, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return PositionLogQuery{}, errors.New("since must be RFC3339 time")
		}
		result.Since = &value
	}
	if raw := strings.TrimSpace(values.Get("before")); raw != "" {
		value, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return PositionLogQuery{}, errors.New("before must be RFC3339 time")
		}
		result.Before = &value
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		var limit int
		if _, err := fmt.Sscanf(raw, "%d", &limit); err != nil {
			return PositionLogQuery{}, errors.New("limit must be integer")
		}
		result.Limit = normalizePositionLogLimit(limit)
	}
	return result, nil
}

func normalizePositionLogLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 300 {
		return 300
	}
	return limit
}

// currentSession 从请求中解析登录会话。
func (s *PositionLogService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
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

// positionIDFromLogsPath 从日志接口路径中解析岗位 ID。
func positionIDFromLogsPath(w http.ResponseWriter, path string) (string, bool) {
	trimmed := strings.TrimPrefix(path, "/api/positions/")
	if trimmed == path {
		writeError(w, http.StatusBadRequest, "position id is required")
		return "", false
	}
	positionID := strings.TrimSuffix(trimmed, "/logs")
	if positionID == "" || positionID == trimmed {
		writeError(w, http.StatusBadRequest, "position log path is invalid")
		return "", false
	}
	return positionID, true
}

func (s *PositionLogService) getTenantInfo(email string) (string, bool) {
	if s.tenantStore == nil {
		return "", false
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(email)
	if err != nil {
		return "", false
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, email)
	return tenant.ID, isAdmin
}

// publicPositionLogs 将岗位日志列表转换为前端响应结构。
func publicPositionLogs(items []PositionLog) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicPositionLog(item))
	}
	return result
}

// publicPositionLog 将岗位日志模型转换为前端响应结构。
func publicPositionLog(item PositionLog) map[string]any {
	return map[string]any{
		"id":          item.ID,
		"position_id": item.PositionID,
		"level":       item.Level,
		"message":     item.Message,
		"created_at":  item.CreatedAt,
	}
}
