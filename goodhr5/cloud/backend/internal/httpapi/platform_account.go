// 本文件提供平台账号兼容 API，底层统一使用 cookie_data 作为账号数据源。
package httpapi

import (
	"errors"
	"net/http"
	"strings"
)

// PlatformAccountService 处理平台账号兼容接口。
// 账号列表、删除和任务选择都以 cookie 记录为准，不再写入 platform_accounts 表。
type PlatformAccountService struct {
	auth        *AuthService
	cookies     CookieStore
	tenantStore TenantStore
}

// NewPlatformAccountService 创建平台账号兼容服务。
// cookies 为 cookie 存储，tenantStore 用于限定当前用户可见的租户数据。
func NewPlatformAccountService(auth *AuthService, cookies CookieStore, tenantStore TenantStore) *PlatformAccountService {
	return &PlatformAccountService{
		auth:        auth,
		cookies:     cookies,
		tenantStore: tenantStore,
	}
}

// List 返回当前租户可见的 cookie 账号列表。
func (s *PlatformAccountService) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := s.currentTenant(w, r)
	if !ok {
		return
	}
	platformID := strings.TrimSpace(r.URL.Query().Get("platform_id"))
	items, err := s.cookies.List(tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list platform accounts")
		return
	}
	accounts := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if platformID != "" && item.PlatformID != platformID {
			continue
		}
		accounts = append(accounts, publicCookieAccount(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"accounts": accounts,
	})
}

// Create 禁止继续创建独立平台账号。
// 新流程应直接调用 /api/cookies/create 写入带名称的 cookie 记录。
func (s *PlatformAccountService) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.currentTenant(w, r); !ok {
		return
	}
	writeError(w, http.StatusBadRequest, "platform account table has been removed; create cookie instead")
}

// Delete 删除一个 cookie 账号记录。
func (s *PlatformAccountService) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := s.currentTenant(w, r)
	if !ok {
		return
	}
	accountID := strings.TrimPrefix(r.URL.Path, "/api/platform-accounts/")
	if accountID == "" || accountID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "account id is required")
		return
	}
	if err := s.cookies.Delete(tenantID, accountID); err != nil {
		if errors.Is(err, ErrCookieNotFound) {
			writeError(w, http.StatusNotFound, "platform account not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete platform account")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// currentTenant 读取当前会话对应的租户 ID。
func (s *PlatformAccountService) currentTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return "", false
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return "", false
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return "", false
	}
	return tenant.ID, true
}

// publicCookieAccount 将 cookie 记录转换为前端账号兼容结构。
func publicCookieAccount(item CookieRecord) map[string]any {
	return map[string]any{
		"id":               item.ID,
		"platform_id":      item.PlatformID,
		"display_name":     item.DisplayName,
		"local_profile_id": item.ID,
		"cookie_status":    item.Status,
		"status":           item.Status,
		"size_bytes":       item.SizeBytes,
		"created_at":       item.CreatedAt,
		"updated_at":       item.UpdatedAt,
	}
}
