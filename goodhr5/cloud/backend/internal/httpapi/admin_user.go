// 本文件负责超级管理员查看用户列表，并手动调整用户会员天数。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// AdminUser 表示超级管理员页面可见的用户信息。
type AdminUser struct {
	ID           string        `json:"id"`
	Email        string        `json:"email"`
	Role         string        `json:"role"`
	Status       string        `json:"status"`
	InviterEmail string        `json:"inviter_email"`
	Agent        *AgentBinding `json:"agent,omitempty"`
	Subscription Subscription  `json:"subscription"`
	CreatedAt    time.Time     `json:"created_at"`
	LastLoginAt  *time.Time    `json:"last_login_at,omitempty"`
}

// AdminUserStore 定义用户管理读取接口。
type AdminUserStore interface {
	// ListUsers 读取用户列表。
	ListUsers() ([]AdminUser, error)
}

type adjustUserSubscriptionRequest struct {
	Email  string `json:"email"`
	Days   int    `json:"days"`
	Reason string `json:"reason"`
}

type unbindUserAgentRequest struct {
	Email string `json:"email"`
}

// AdminUserService 处理超级管理员用户管理接口。
type AdminUserService struct {
	auth          *AuthService
	users         AdminUserStore
	subscriptions SubscriptionStore
	mailer        Mailer
	agents        AgentStore
}

// NewAdminUserService 创建超级管理员用户管理服务。
func NewAdminUserService(auth *AuthService, users AdminUserStore, subscriptions SubscriptionStore, mailer Mailer, agents AgentStore) *AdminUserService {
	return &AdminUserService{auth: auth, users: users, subscriptions: subscriptions, mailer: mailer, agents: agents}
}

// Collection 根据请求方法分发用户列表读取和会员天数调整。
func (s *AdminUserService) Collection(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "super admin access required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.list(w, r)
	case http.MethodPost:
		s.adjustSubscription(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// list 返回超级管理员可见的用户列表。
func (s *AdminUserService) list(w http.ResponseWriter, _ *http.Request) {
	users, err := s.users.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load users")
		return
	}
	result := make([]map[string]any, 0, len(users))
	for _, user := range users {
		if s.auth.IsSuperAdmin(user.Email) {
			user.Role = "super_admin"
		}
		if s.agents != nil {
			if binding, err := s.agents.CurrentBinding(user.Email); err == nil {
				user.Agent = &binding
			}
		}
		result = append(result, publicAdminUser(user))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "users": result})
}

// adjustSubscription 按正负天数调整用户会员到期时间。
func (s *AdminUserService) adjustSubscription(w http.ResponseWriter, r *http.Request) {
	var req adjustUserSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	email, ok := normalizeEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid email")
		return
	}
	if req.Days == 0 {
		writeError(w, http.StatusBadRequest, "days must not be zero")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "超级管理员调整会员天数"
	}

	subscription, err := s.subscriptions.AdjustSubscriptionDays(email, defaultMemberType, req.Days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to adjust subscription")
		return
	}
	if err := sendSubscriptionRewardNotice(s.mailer, email, SubscriptionRewardNotice{
		Reason:     reason,
		Days:       req.Days,
		MemberType: subscription.MemberType,
		ExpiresAt:  subscription.ExpiresAt,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send subscription notice")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"subscription": publicSubscription(subscription),
	})
}

// UnbindAgent 解除指定用户当前本地程序机器绑定。
func (s *AdminUserService) UnbindAgent(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "super admin access required")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req unbindUserAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	email, ok := normalizeEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid email")
		return
	}
	if s.agents == nil {
		writeError(w, http.StatusInternalServerError, "agent store is not ready")
		return
	}
	if err := s.agents.DisableBindings(email); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unbind agent")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// publicAdminUser 转换用户信息为前端响应。
func publicAdminUser(user AdminUser) map[string]any {
	return map[string]any{
		"id":            user.ID,
		"email":         user.Email,
		"role":          user.Role,
		"status":        user.Status,
		"inviter_email": user.InviterEmail,
		"agent":         publicAdminAgent(user.Agent),
		"subscription":  publicSubscription(user.Subscription),
		"created_at":    user.CreatedAt,
		"last_login_at": user.LastLoginAt,
	}
}

// publicAdminAgent 转换本地程序绑定信息为前端响应。
func publicAdminAgent(agent *AgentBinding) map[string]any {
	if agent == nil {
		return nil
	}
	return map[string]any{
		"machine_id":    agent.MachineID,
		"agent_version": agent.AgentVersion,
		"public_key":    agent.PublicKey,
		"bind_status":   agent.BindStatus,
		"last_seen_at":  agent.LastSeenAt,
		"created_at":    agent.CreatedAt,
	}
}

// ---------- 内存实现 ----------

type MemoryAdminUserStore struct {
	subscriptions *MemorySubscriptionStore
}

// NewMemoryAdminUserStore 创建内存用户管理存储。
func NewMemoryAdminUserStore(subscriptions *MemorySubscriptionStore) *MemoryAdminUserStore {
	return &MemoryAdminUserStore{subscriptions: subscriptions}
}

// ListUsers 读取内存用户列表。
func (s *MemoryAdminUserStore) ListUsers() ([]AdminUser, error) {
	if s == nil || s.subscriptions == nil {
		return []AdminUser{}, nil
	}
	users := make([]AdminUser, 0, len(s.subscriptions.items))
	for email, subscription := range s.subscriptions.items {
		users = append(users, AdminUser{
			ID:           email,
			Email:        email,
			Role:         "user",
			Status:       "active",
			Subscription: subscription,
			CreatedAt:    s.subscriptions.now(),
		})
	}
	return users, nil
}

// ---------- PostgreSQL 实现 ----------

type PostgresAdminUserStore struct {
	db *sql.DB
}

// NewPostgresAdminUserStore 创建 PostgreSQL 用户管理存储。
func NewPostgresAdminUserStore(db *sql.DB) *PostgresAdminUserStore {
	return &PostgresAdminUserStore{db: db}
}

// ListUsers 读取 PostgreSQL 用户列表。
func (s *PostgresAdminUserStore) ListUsers() ([]AdminUser, error) {
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT
			u.id::text,
			u.email,
			COALESCE(u.role, 'user'),
			COALESCE(u.status, 'active'),
			u.subscription,
			u.created_at,
			u.last_login_at,
			COALESCE(inviter.email, '')
		FROM users u
		LEFT JOIN users inviter ON inviter.id = u.inviter_id
		ORDER BY u.created_at DESC
		LIMIT 1000
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []AdminUser{}
	for rows.Next() {
		var user AdminUser
		var rawSubscription []byte
		var lastLoginAt sql.NullTime
		if err := rows.Scan(&user.ID, &user.Email, &user.Role, &user.Status, &rawSubscription, &user.CreatedAt, &lastLoginAt, &user.InviterEmail); err != nil {
			return nil, err
		}
		subscription, err := parseSubscription(rawSubscription)
		if err != nil {
			return nil, err
		}
		user.Subscription = subscription
		if lastLoginAt.Valid {
			user.LastLoginAt = &lastLoginAt.Time
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}
