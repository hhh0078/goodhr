// 本文件提供租户管理的 HTTP API。
package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TenantService struct {
	auth   *AuthService
	store  TenantStore
	mailer Mailer
}

// NewTenantService 创建团队管理服务，并注入认证、存储和邮件能力。
func NewTenantService(auth *AuthService, store TenantStore, mailer Mailer) *TenantService {
	return &TenantService{auth: auth, store: store, mailer: mailer}
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
	if !ok {
		return
	}

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

	todayGreetedCount := 0
	for _, member := range members {
		todayGreetedCount += member.TodayGreetedCount
	}
	canManage, _ := s.store.IsTenantAdmin(tenant.ID, session.Email)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "members": members, "today_greeted_count": todayGreetedCount,
		"can_manage": canManage, "tenant": map[string]any{"id": tenant.ID, "name": tenant.Name, "owner_email": tenant.OwnerEmail},
	})
}

// Invite 邀请邮箱加入租户，仅管理员可操作。
func (s *TenantService) Invite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	tenant, err := s.store.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	if !s.requireTenantAdmin(w, tenant.ID, session.Email) {
		return
	}

	var req inviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	email, valid := normalizeEmail(req.Email)
	if !valid {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if strings.EqualFold(email, session.Email) {
		writeError(w, http.StatusBadRequest, "自己已经在团队里啦，这封邀请我就先不寄了")
		return
	}
	if req.Role != "admin" && req.Role != "user" {
		req.Role = "user"
	}

	invitation, resent, err := s.store.InviteMember(tenant.ID, email, req.Role, session.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := sendTeamInvitationNotice(s.mailer, invitation.InviteeEmail, TeamInvitationNotice{
		InviterEmail: invitation.InvitedByEmail,
		TeamName:     invitation.TenantName,
		TeamOwner:    invitation.TenantOwner,
		Role:         invitation.Role,
		LoginURL:     "https://goodhr5.58it.cn/admin",
	}); err != nil {
		log.Printf("[团队邀请] 邮件发送失败 invitation=%s invitee=%s err=%v", invitation.ID, invitation.InviteeEmail, err)
		writeError(w, http.StatusBadGateway, "邀请已经记下了，但邮件没发出去。问题不大，稍后点重发就行")
		return
	}
	if err := s.store.MarkInvitationEmailSent(invitation.ID, time.Now()); err != nil {
		log.Printf("[团队邀请] 邮件成功但发送时间记录失败 invitation=%s err=%v", invitation.ID, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "invitation": invitation, "resent": resent})
}

// PendingInvitations 返回当前登录账号等待确认的团队邀请。
func (s *TenantService) PendingInvitations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	items, err := s.store.PendingInvitations(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "团队邀请暂时没读出来，请刷新后再试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "invitations": items})
}

// InvitationAction 处理邀请的接受、拒绝、重发、修改角色和取消操作。
func (s *TenantService) InvitationAction(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tenants/invitations/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeError(w, http.StatusBadRequest, "invitation id is required")
		return
	}
	invitationID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if action == "accept" && r.Method == http.MethodPost {
		if err := s.store.AcceptInvitation(invitationID, session.Email); err != nil {
			s.writeInvitationActionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if action == "reject" && r.Method == http.MethodPost {
		if err := s.store.RejectInvitation(invitationID, session.Email); err != nil {
			s.writeInvitationActionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	tenant, err := s.store.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	if !s.requireTenantAdmin(w, tenant.ID, session.Email) {
		return
	}
	if action == "resend" && r.Method == http.MethodPost {
		member, found, err := s.pendingMemberByID(tenant.ID, invitationID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "邀请暂时没读出来")
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "这条邀请已经处理过了")
			return
		}
		invitation, _, err := s.store.InviteMember(tenant.ID, member.Email, member.Role, session.Email)
		if err != nil {
			s.writeInvitationActionError(w, err)
			return
		}
		if err = sendTeamInvitationNotice(s.mailer, invitation.InviteeEmail, TeamInvitationNotice{InviterEmail: session.Email, TeamName: invitation.TenantName, TeamOwner: invitation.TenantOwner, Role: invitation.Role, LoginURL: "https://goodhr5.58it.cn/admin"}); err != nil {
			log.Printf("[团队邀请] 重发邮件失败 invitation=%s invitee=%s err=%v", invitation.ID, invitation.InviteeEmail, err)
			writeError(w, http.StatusBadGateway, "邮件这次还是没发出去，我已经记下了，可以稍后再试")
			return
		}
		_ = s.store.MarkInvitationEmailSent(invitation.ID, time.Now())
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if action == "" && r.Method == http.MethodPut {
		var req updateRoleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if err := s.store.UpdateInvitationRole(tenant.ID, invitationID, req.Role); err != nil {
			s.writeInvitationActionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if action == "" && r.Method == http.MethodDelete {
		if err := s.store.CancelInvitation(tenant.ID, invitationID); err != nil {
			s.writeInvitationActionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// UpdateMember 修改成员角色，仅管理员可操作。
func (s *TenantService) UpdateMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	tenant, err := s.store.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	if !s.requireTenantAdmin(w, tenant.ID, session.Email) {
		return
	}

	email, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/tenants/members/"))
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
		s.writeInvitationActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ToggleCookieSharing 切换 cookie 共享开关。
func (s *TenantService) ToggleCookieSharing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, 500, "failed to get tenant")
		return
	}
	if !s.requireTenantAdmin(w, tenant.ID, session.Email) {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	s.store.SetCookieSharing(tenant.ID, req.Enabled)
	writeJSON(w, 200, map[string]any{"ok": true, "enabled": req.Enabled})
}

// DeleteMember 移除成员，仅管理员可操作。
func (s *TenantService) DeleteMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	tenant, err := s.store.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	if !s.requireTenantAdmin(w, tenant.ID, session.Email) {
		return
	}

	email, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/tenants/members/"))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	if err := s.store.RemoveMember(tenant.ID, email); err != nil {
		s.writeInvitationActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// pendingMemberByID 从团队列表中找到指定待确认邀请。
func (s *TenantService) pendingMemberByID(tenantID, invitationID string) (TenantMember, bool, error) {
	members, err := s.store.ListMembers(tenantID)
	if err != nil {
		return TenantMember{}, false, err
	}
	for _, member := range members {
		if member.InvitationID == invitationID && member.Status == "pending" {
			return member, true, nil
		}
	}
	return TenantMember{}, false, nil
}

// writeInvitationActionError 把团队邀请和成员操作错误转换为明确的 HTTP 提示。
func (s *TenantService) writeInvitationActionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "这条邀请或成员记录已经找不到了")
	case errors.Is(err, ErrCannotMoveSharedTenant), errors.Is(err, ErrPositionRunning), errors.Is(err, ErrTenantOwner):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *TenantService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return Session{}, false
	}
	return session, true
}

// requireTenantAdmin 校验当前邮箱是否为团队管理员。
// tenantID 为团队 ID，email 为当前登录邮箱；不通过时直接写入 403 响应。
func (s *TenantService) requireTenantAdmin(w http.ResponseWriter, tenantID string, email string) bool {
	isAdmin, err := s.store.IsTenantAdmin(tenantID, email)
	if err != nil || !isAdmin {
		writeError(w, http.StatusForbidden, "只有团队管理员才能操作")
		return false
	}
	return true
}
