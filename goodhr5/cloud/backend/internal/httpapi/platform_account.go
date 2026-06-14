// 本文件提供平台账号信息 API，只保存账号名称和本地 profile 标识。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// PlatformAccountService 处理平台账号信息接口。
// 云端只保存账号名称、本地 profile ID 和平台标识，不保存 cookie 明文。
type PlatformAccountService struct {
	auth        *AuthService
	store       PlatformAccountStore
	tenantStore TenantStore
}

// NewPlatformAccountService 创建平台账号信息服务。
// store 为平台账号信息存储，tenantStore 用于限定当前用户可见的租户数据。
func NewPlatformAccountService(auth *AuthService, store PlatformAccountStore, tenantStore TenantStore) *PlatformAccountService {
	return &PlatformAccountService{
		auth:        auth,
		store:       store,
		tenantStore: tenantStore,
	}
}

// List 返回当前租户可见的平台账号信息列表。
func (s *PlatformAccountService) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, tenantID, isAdmin, ok := s.currentAccountContext(w, r)
	if !ok {
		return
	}
	platformID := strings.TrimSpace(r.URL.Query().Get("platform_id"))
	items, err := s.store.ListPlatformAccounts(tenantID, session.Email, platformID, isAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list platform accounts")
		return
	}
	accounts := make([]map[string]any, 0, len(items))
	for _, item := range items {
		accounts = append(accounts, publicPlatformAccount(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"accounts": accounts,
	})
}

// Create 创建平台账号信息记录。
// 请求只保存展示名称和本地 profile ID，不接收 cookie 内容。
func (s *PlatformAccountService) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, _, _, ok := s.currentAccountContext(w, r)
	if !ok {
		return
	}
	var req struct {
		PlatformID      string `json:"platform_id"`
		DisplayName     string `json:"display_name"`
		LocalProfileID  string `json:"local_profile_id"`
		LocalProfileAlt string `json:"profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	platformID := strings.TrimSpace(req.PlatformID)
	displayName := strings.TrimSpace(req.DisplayName)
	localProfileID := strings.TrimSpace(req.LocalProfileID)
	if localProfileID == "" {
		localProfileID = strings.TrimSpace(req.LocalProfileAlt)
	}
	if platformID == "" {
		writeError(w, http.StatusBadRequest, "platform_id is required")
		return
	}
	if displayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}
	if localProfileID == "" {
		localProfileID = platformID + "_" + safePlatformProfileName(displayName)
	}
	account, err := s.store.SavePlatformAccount(PlatformAccount{
		UserEmail:      session.Email,
		PlatformID:     platformID,
		DisplayName:    displayName,
		LocalProfileID: localProfileID,
	})
	if errors.Is(err, ErrConflict) {
		writeError(w, http.StatusConflict, "platform account already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create platform account")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"account": publicPlatformAccount(account),
	})
}

// Delete 删除一个平台账号信息记录。
func (s *PlatformAccountService) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, _, _, ok := s.currentAccountContext(w, r)
	if !ok {
		return
	}
	accountID := strings.TrimPrefix(r.URL.Path, "/api/platform-accounts/")
	if accountID == "" || accountID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "account id is required")
		return
	}
	if err := s.store.DeletePlatformAccount(session.Email, accountID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "platform account not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete platform account")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// currentAccountContext 读取当前平台账号请求的会话和租户上下文。
// 返回登录会话、租户 ID 和管理员标记，读取失败时已经写入响应。
func (s *PlatformAccountService) currentAccountContext(w http.ResponseWriter, r *http.Request) (Session, string, bool, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return Session{}, "", false, false
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return Session{}, "", false, false
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return Session{}, "", false, false
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, session.Email)
	return session, tenant.ID, isAdmin, true
}

// publicPlatformAccount 将平台账号记录转换为前端展示结构。
// item 为平台账号存储记录，返回前端使用的 JSON 字段。
func publicPlatformAccount(item PlatformAccount) map[string]any {
	return map[string]any{
		"id":               item.ID,
		"platform_id":      item.PlatformID,
		"display_name":     item.DisplayName,
		"local_profile_id": item.LocalProfileID,
		"status":           "available",
		"created_at":       item.CreatedAt,
		"updated_at":       item.CreatedAt,
	}
}

// safePlatformProfileName 生成安全的本地账号目录名称。
// value 为用户输入的账号名称，返回只包含字母、数字、下划线和短横线的字符串。
func safePlatformProfileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	result := strings.Trim(builder.String(), "_-")
	if result == "" {
		return "default"
	}
	return result
}
