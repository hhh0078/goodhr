// 本文件负责提供岗位配置的 HTTP API。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// PositionService 处理岗位配置的创建、查询和删除请求。
type PositionService struct {
	auth  *AuthService
	store PositionStore
}

type positionRequest struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Keywords        []string `json:"keywords"`
	ExcludeKeywords []string `json:"exclude_keywords"`
	Description     string   `json:"description"`
	GreetMessage    string   `json:"greet_message"`
	IsAndMode       bool     `json:"is_and_mode"`
}

// NewPositionService 创建岗位配置 API 服务，并注入认证服务和岗位存储。
func NewPositionService(auth *AuthService, store PositionStore) *PositionService {
	return &PositionService{
		auth:  auth,
		store: store,
	}
}

// Collection 按请求方法处理岗位配置集合资源。
func (s *PositionService) Collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.List(w, r)
	case http.MethodPost:
		s.Save(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// List 返回当前登录用户的岗位配置列表。
func (s *PositionService) List(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	// 调用岗位存储读取当前用户的岗位配置，供后续任务选择和复用。
	items, err := s.store.ListPositions("", session.Email, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list positions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"positions": publicPositions(items),
	})
}

// Save 创建或更新一个岗位配置。
func (s *PositionService) Save(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	var req positionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	position, ok := req.toPosition(w, session.Email)
	if !ok {
		return
	}

	// 调用岗位存储保存岗位配置，用于后续任务快速选择筛选条件。
	saved, err := s.store.SavePosition(position)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save position")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"position": publicPosition(saved),
	})
}

// Delete 删除当前登录用户的岗位配置。
func (s *PositionService) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	positionID := strings.TrimPrefix(r.URL.Path, "/api/positions/")
	if positionID == "" || positionID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "position id is required")
		return
	}

	// 调用岗位存储删除岗位配置，避免继续出现在任务配置候选项里。
	err := s.store.DeletePosition(session.Email, positionID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete position")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

// currentSession 从请求中解析登录会话。
func (s *PositionService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	// 调用认证服务解析请求会话，避免岗位配置 API 自己重复处理 token。
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

// toPosition 将请求结构转换为岗位配置模型。
func (r positionRequest) toPosition(w http.ResponseWriter, userEmail string) (Position, bool) {
	position := Position{
		ID:              strings.TrimSpace(r.ID),
		UserEmail:       userEmail,
		Name:            strings.TrimSpace(r.Name),
		Keywords:        trimStringList(r.Keywords),
		ExcludeKeywords: trimStringList(r.ExcludeKeywords),
		Description:     strings.TrimSpace(r.Description),
		GreetMessage:    strings.TrimSpace(r.GreetMessage),
		IsAndMode:       r.IsAndMode,
	}

	if position.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return Position{}, false
	}
	return position, true
}

// publicPositions 将岗位配置列表转换为前端响应结构。
func publicPositions(items []Position) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicPosition(item))
	}
	return result
}

// publicPosition 将岗位配置转换为前端响应结构。
func publicPosition(item Position) map[string]any {
	return map[string]any{
		"id":               item.ID,
		"name":             item.Name,
		"keywords":         item.Keywords,
		"exclude_keywords": item.ExcludeKeywords,
		"description":      item.Description,
		"greet_message":    item.GreetMessage,
		"is_and_mode":      item.IsAndMode,
		"created_at":       item.CreatedAt,
		"updated_at":       item.UpdatedAt,
	}
}

// trimStringList 清理字符串数组里的空白项。
func trimStringList(items []string) []string {
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		cleaned = append(cleaned, value)
	}
	return cleaned
}
