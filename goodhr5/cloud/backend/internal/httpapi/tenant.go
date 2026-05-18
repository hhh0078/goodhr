// 本文件提供租户管理的 HTTP API。
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

type TenantService struct {
	auth  *AuthService
	store TenantStore
}

func NewTenantService(auth *AuthService, store TenantStore) *TenantService {
	return &TenantService{auth: auth, store: store}
}

type inviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type updateRoleRequest struct {
	Role string `json:"role"`
}

// Members 返回当前用户所在租户的成员列表，仅管理员可访问。
func (s *TenantService) Members(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok { return }

	tenant, err := s.store.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}

	members, err := s.store.ListMembers(tenant.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "members": members})
}

// Invite 邀请邮箱加入租户，仅管理员可操作。
func (s *TenantService) Invite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok { return }

	tenant, err := s.store.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}

	var req inviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if req.Role != "admin" && req.Role != "user" {
		req.Role = "user"
	}

	if err := s.store.InviteMember(tenant.ID, req.Email, req.Role, session.Email); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// UpdateMember 修改成员角色，仅管理员可操作。
func (s *TenantService) UpdateMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok { return }

	tenant, err := s.store.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}

	email := strings.TrimPrefix(r.URL.Path, "/api/tenants/members/")
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := s.store.UpdateMemberRole(tenant.ID, email, req.Role); err != nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DeleteMember 移除成员，仅管理员可操作。
func (s *TenantService) DeleteMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok { return }

	tenant, err := s.store.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}

	email := strings.TrimPrefix(r.URL.Path, "/api/tenants/members/")
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	if err := s.store.RemoveMember(tenant.ID, email); err != nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *TenantService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return Session{}, false
	}
	return session, true
}
