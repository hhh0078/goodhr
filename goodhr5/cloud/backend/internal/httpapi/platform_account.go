// 本文件负责提供招聘平台账号映射的 HTTP API。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// PlatformAccountService 处理平台账号映射的创建、查询和删除。
type PlatformAccountService struct {
	auth  *AuthService
	store PlatformAccountStore
}

type createPlatformAccountRequest struct {
	PlatformID     string `json:"platform_id"`
	DisplayName    string `json:"display_name"`
	LocalProfileID string `json:"local_profile_id"`
}

// NewPlatformAccountService 创建平台账号 API 服务，并注入认证服务和账号存储。
func NewPlatformAccountService(auth *AuthService, store PlatformAccountStore) *PlatformAccountService {
	return &PlatformAccountService{
		auth:  auth,
		store: store,
	}
}

// List 返回当前登录用户的平台账号映射列表。
func (s *PlatformAccountService) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，用于只返回自己的平台账号映射。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	platformID := strings.TrimSpace(r.URL.Query().Get("platform_id"))
	// 调用平台账号存储读取映射列表，用于任务创建时选择账号/profile。
	items, err := s.store.ListPlatformAccounts(session.Email, platformID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list platform accounts")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"accounts": publicPlatformAccounts(items),
	})
}

// Create 创建一个平台账号映射。
func (s *PlatformAccountService) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，用于将账号映射写入该用户名下。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	var req createPlatformAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	account, ok := req.toAccount(w, session.Email)
	if !ok {
		return
	}

	// 调用平台账号存储保存映射；cookie/profile 原文仍保留在本地 Agent。
	saved, err := s.store.SavePlatformAccount(account)
	if errors.Is(err, ErrConflict) {
		writeError(w, http.StatusConflict, "platform account already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save platform account")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"account": publicPlatformAccount(saved),
	})
}

// Delete 删除当前登录用户的平台账号映射。
func (s *PlatformAccountService) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，用于避免删除其他用户的账号映射。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	accountID := strings.TrimPrefix(r.URL.Path, "/api/platform-accounts/")
	if accountID == "" || accountID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "account id is required")
		return
	}

	// 调用平台账号存储删除映射；本地 profile 是否删除由 Local Agent 另行处理。
	err := s.store.DeletePlatformAccount(session.Email, accountID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "platform account not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete platform account")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

// currentSession 从请求中解析登录会话。
func (s *PlatformAccountService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	// 调用认证服务解析请求会话，避免平台账号 API 自己重复处理 token。
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

// toAccount 将创建请求转换为平台账号映射模型。
func (r createPlatformAccountRequest) toAccount(w http.ResponseWriter, userEmail string) (PlatformAccount, bool) {
	account := PlatformAccount{
		UserEmail:      userEmail,
		PlatformID:     strings.TrimSpace(r.PlatformID),
		DisplayName:    strings.TrimSpace(r.DisplayName),
		LocalProfileID: strings.TrimSpace(r.LocalProfileID),
	}

	if account.PlatformID == "" {
		writeError(w, http.StatusBadRequest, "platform_id is required")
		return PlatformAccount{}, false
	}
	if account.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return PlatformAccount{}, false
	}
	if account.LocalProfileID == "" {
		writeError(w, http.StatusBadRequest, "local_profile_id is required")
		return PlatformAccount{}, false
	}
	return account, true
}

// publicPlatformAccounts 将平台账号映射列表转换为前端响应结构。
func publicPlatformAccounts(items []PlatformAccount) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicPlatformAccount(item))
	}
	return result
}

// publicPlatformAccount 将平台账号映射转换为前端响应结构。
func publicPlatformAccount(item PlatformAccount) map[string]any {
	return map[string]any{
		"id":               item.ID,
		"platform_id":      item.PlatformID,
		"display_name":     item.DisplayName,
		"local_profile_id": item.LocalProfileID,
		"created_at":       item.CreatedAt,
	}
}
