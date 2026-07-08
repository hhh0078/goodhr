// 本文件负责超级管理员查看用户列表，并手动调整用户会员天数。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AdminUser 表示超级管理员页面可见的用户信息。
type AdminUser struct {
	ID                  string              `json:"id"`
	Email               string              `json:"email"`
	Role                string              `json:"role"`
	Status              string              `json:"status"`
	InviterEmail        string              `json:"inviter_email"`
	Agent               *AgentBinding       `json:"agent,omitempty"`
	Subscription        Subscription        `json:"subscription"`
	NotificationProfile NotificationProfile `json:"notification_profile"`
	AIBalanceCents      int                 `json:"ai_balance_cents"`
	Flow                AdminUserFlow       `json:"flow"`
	CreatedAt           time.Time           `json:"created_at"`
	LastLoginAt         *time.Time          `json:"last_login_at,omitempty"`
}

// AdminUserFlow 表示用户关键流程完成情况。
type AdminUserFlow struct {
	Steps       []AdminUserFlowStep `json:"steps"`
	CurrentStep string              `json:"current_step"`
	Completed   bool                `json:"completed"`
}

// AdminUserFlowStep 表示用户流程中的一个是/否节点。
type AdminUserFlowStep struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Done bool   `json:"done"`
}

// AdminUserListQuery 表示后台用户列表查询条件。
type AdminUserListQuery struct {
	Query    string
	Page     int
	PageSize int
}

// AdminUserListResult 表示后台用户列表分页结果。
type AdminUserListResult struct {
	Users    []AdminUser
	Total    int
	Page     int
	PageSize int
}

// AdminUserStats 表示后台用户管理统计数据。
type AdminUserStats struct {
	TodayRegisteredCount int `json:"today_registered_count"`
	AgentBindingCount    int `json:"agent_binding_count"`
}

// AdminUserStore 定义用户管理读取接口。
type AdminUserStore interface {
	// ListUsers 读取用户分页列表。
	ListUsers(query AdminUserListQuery) (AdminUserListResult, error)
	// Stats 读取用户管理统计数据。
	Stats() (AdminUserStats, error)
}

type adjustUserSubscriptionRequest struct {
	Email  string `json:"email"`
	Days   int    `json:"days"`
	Reason string `json:"reason"`
}

type adjustUserAIBalanceRequest struct {
	Email       string `json:"email"`
	AmountCents int    `json:"amount_cents"`
	AmountYuan  string `json:"amount_yuan"`
	Reason      string `json:"reason"`
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
	aiWallet      AIWalletStore
}

// NewAdminUserService 创建超级管理员用户管理服务。
func NewAdminUserService(auth *AuthService, users AdminUserStore, subscriptions SubscriptionStore, mailer Mailer, agents AgentStore, aiWallet AIWalletStore) *AdminUserService {
	return &AdminUserService{auth: auth, users: users, subscriptions: subscriptions, mailer: mailer, agents: agents, aiWallet: aiWallet}
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
func (s *AdminUserService) list(w http.ResponseWriter, r *http.Request) {
	query := adminUserListQueryFromRequest(r)
	result, err := s.users.ListUsers(query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load users")
		return
	}
	stats, err := s.users.Stats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user stats")
		return
	}
	if s.agents != nil {
		if count, err := s.agents.ActiveBindingCount(); err == nil {
			stats.AgentBindingCount = count
		}
	}
	users := make([]map[string]any, 0, len(result.Users))
	for _, user := range result.Users {
		if s.auth.IsSuperAdmin(user.Email) {
			user.Role = "super_admin"
		}
		if s.agents != nil {
			if binding, err := s.agents.CurrentBinding(user.Email); err == nil {
				user.Agent = &binding
			}
		}
		users = append(users, publicAdminUser(user))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"users":     users,
		"total":     result.Total,
		"page":      result.Page,
		"page_size": result.PageSize,
		"stats":     stats,
	})
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

// AdjustAIBalance 调整指定用户的内置 AI 余额。
func (s *AdminUserService) AdjustAIBalance(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session invalid or expired")
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
	var req adjustUserAIBalanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	email, ok := normalizeEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid email")
		return
	}
	amountCents := req.AmountCents
	if amountCents == 0 && strings.TrimSpace(req.AmountYuan) != "" {
		amountCents, err = yuanTextToCents(req.AmountYuan)
		if err != nil {
			writeError(w, http.StatusBadRequest, "余额金额不太对，我没敢动。")
			return
		}
	}
	if amountCents == 0 {
		writeError(w, http.StatusBadRequest, "amount must not be zero")
		return
	}
	if s.aiWallet == nil {
		writeError(w, http.StatusInternalServerError, "ai wallet is not ready")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "超级管理员调整AI余额"
	}
	balance, err := s.aiWallet.AdjustBalance(AIWalletRecord{
		UserEmail:   email,
		ChangeCents: amountCents,
		Category:    "admin_adjust",
		Reason:      reason,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed adjust ai balance")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "balance_cents": balance, "balance": centsToYuanString(balance)})
}

// publicAdminUser 转换用户信息为前端响应。
func publicAdminUser(user AdminUser) map[string]any {
	return map[string]any{
		"id":                   user.ID,
		"email":                user.Email,
		"role":                 user.Role,
		"status":               user.Status,
		"inviter_email":        user.InviterEmail,
		"agent":                publicAdminAgent(user.Agent),
		"subscription":         publicSubscription(user.Subscription),
		"notification_profile": user.NotificationProfile,
		"ai_balance_cents":     user.AIBalanceCents,
		"ai_balance":           centsToYuanString(user.AIBalanceCents),
		"flow":                 user.Flow,
		"created_at":           user.CreatedAt,
		"last_login_at":        user.LastLoginAt,
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

// buildAdminUserFlow 生成用户流程链条和当前卡点。
// hasAgent 到 hasPaid 依次表示流程节点是否完成。
func buildAdminUserFlow(hasAgent bool, _ bool, _ bool, hasPosition bool, hasGreeted bool, hasPaid bool) AdminUserFlow {
	steps := []AdminUserFlowStep{
		{Key: "local_agent", Name: "未绑定本地程序", Done: hasAgent},
		{Key: "position", Name: "未创建岗位", Done: hasPosition},
		{Key: "greet_success", Name: "未打招呼成功", Done: hasGreeted},
		{Key: "paid", Name: "未支付", Done: hasPaid},
	}
	for _, step := range steps {
		if !step.Done {
			return AdminUserFlow{Steps: steps, CurrentStep: step.Name, Completed: false}
		}
	}
	return AdminUserFlow{Steps: steps, CurrentStep: "流程完成", Completed: true}
}

// adminUserListQueryFromRequest 从请求中读取用户列表分页和搜索条件。
// r 为 HTTP 请求，返回规范化后的查询条件。
func adminUserListQueryFromRequest(r *http.Request) AdminUserListQuery {
	values := r.URL.Query()
	return AdminUserListQuery{
		Query:    strings.TrimSpace(values.Get("q")),
		Page:     normalizeAdminUserPage(parseAdminPositiveInt(values.Get("page"), 1)),
		PageSize: normalizeAdminUserPageSize(parseAdminPositiveInt(values.Get("page_size"), 20)),
	}
}

// parseAdminPositiveInt 解析后台用户列表正整数参数。
// value 为原始字符串，fallback 为解析失败时的默认值。
func parseAdminPositiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

// normalizeAdminUserPage 规范用户列表页码。
// page 为原始页码，返回至少为 1 的页码。
func normalizeAdminUserPage(page int) int {
	if page < 1 {
		return 1
	}
	return page
}

// normalizeAdminUserPageSize 规范用户列表每页数量。
// pageSize 为原始数量，返回 1 到 100 之间的数量。
func normalizeAdminUserPageSize(pageSize int) int {
	if pageSize < 1 {
		return 20
	}
	if pageSize > 100 {
		return 100
	}
	return pageSize
}

// ---------- 内存实现 ----------

type MemoryAdminUserStore struct {
	subscriptions *MemorySubscriptionStore
}

// NewMemoryAdminUserStore 创建内存用户管理存储。
func NewMemoryAdminUserStore(subscriptions *MemorySubscriptionStore) *MemoryAdminUserStore {
	return &MemoryAdminUserStore{subscriptions: subscriptions}
}

// ListUsers 读取内存用户分页列表。
func (s *MemoryAdminUserStore) ListUsers(query AdminUserListQuery) (AdminUserListResult, error) {
	if s == nil || s.subscriptions == nil {
		return AdminUserListResult{Users: []AdminUser{}, Page: 1, PageSize: 20}, nil
	}
	users := make([]AdminUser, 0, len(s.subscriptions.items))
	for email, subscription := range s.subscriptions.items {
		users = append(users, AdminUser{
			ID:           email,
			Email:        email,
			Role:         "user",
			Status:       "active",
			Subscription: subscription,
			Flow:         buildAdminUserFlow(false, false, false, false, false, subscriptionActive(subscription)),
			CreatedAt:    s.subscriptions.now(),
		})
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].CreatedAt.After(users[j].CreatedAt)
	})
	users = filterAdminUsers(users, query.Query)
	page, pageSize := normalizeAdminUserPage(query.Page), normalizeAdminUserPageSize(query.PageSize)
	total := len(users)
	start := (page - 1) * pageSize
	if start >= total {
		return AdminUserListResult{Users: []AdminUser{}, Total: total, Page: page, PageSize: pageSize}, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return AdminUserListResult{Users: users[start:end], Total: total, Page: page, PageSize: pageSize}, nil
}

// Stats 读取内存用户管理统计。
func (s *MemoryAdminUserStore) Stats() (AdminUserStats, error) {
	if s == nil || s.subscriptions == nil {
		return AdminUserStats{}, nil
	}
	today := s.subscriptions.now().Format(time.DateOnly)
	count := 0
	for range s.subscriptions.items {
		// 内存订阅没有真实注册时间，测试环境按当前用户数计算今日注册。
		count++
	}
	if today == "" {
		count = 0
	}
	return AdminUserStats{TodayRegisteredCount: count}, nil
}

// ---------- PostgreSQL 实现 ----------

type PostgresAdminUserStore struct {
	db *sql.DB
}

// NewPostgresAdminUserStore 创建 PostgreSQL 用户管理存储。
func NewPostgresAdminUserStore(db *sql.DB) *PostgresAdminUserStore {
	return &PostgresAdminUserStore{db: db}
}

// ListUsers 读取 PostgreSQL 用户分页列表。
func (s *PostgresAdminUserStore) ListUsers(query AdminUserListQuery) (AdminUserListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	page, pageSize := normalizeAdminUserPage(query.Page), normalizeAdminUserPageSize(query.PageSize)
	whereSQL, args := adminUserWhere(query.Query)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u LEFT JOIN users inviter ON inviter.id = u.inviter_id WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return AdminUserListResult{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			u.id::text,
			u.email,
			COALESCE(u.role, 'user'),
			COALESCE(u.status, 'active'),
			COALESCE(u.ai_balance_cents, 0),
			u.subscription,
			u.notification_profile,
			u.created_at,
			u.last_login_at,
			COALESCE(inviter.email, ''),
			EXISTS (SELECT 1 FROM local_agents la WHERE la.user_id = u.id AND la.bind_status = 'active'),
			EXISTS (SELECT 1 FROM user_ai_configs ai WHERE ai.user_id = u.id AND ai.enabled = true AND COALESCE(ai.base_url, '') <> '' AND COALESCE(ai.model, '') <> '' AND COALESCE(ai.api_key_encrypted, '') <> ''),
			EXISTS (SELECT 1 FROM platform_accounts pa WHERE pa.user_id = u.id),
			EXISTS (SELECT 1 FROM positions p WHERE p.user_id = u.id),
			EXISTS (SELECT 1 FROM task_runs tr WHERE tr.user_id = u.id AND (tr.greeted_count > 0 OR tr.daily_greeted_count > 0)),
			EXISTS (SELECT 1 FROM payment_orders po WHERE po.user_id = u.id AND po.status = 'paid')
		FROM users u
		LEFT JOIN users inviter ON inviter.id = u.inviter_id
		WHERE `+whereSQL+`
		ORDER BY u.created_at DESC
		LIMIT $`+intString(len(args)-1)+` OFFSET $`+intString(len(args))+`
	`, args...)
	if err != nil {
		return AdminUserListResult{}, err
	}
	defer rows.Close()

	users := []AdminUser{}
	for rows.Next() {
		var user AdminUser
		var rawSubscription []byte
		var rawNotificationProfile []byte
		var lastLoginAt sql.NullTime
		var hasAgent, hasAI, hasPlatformAccount, hasPosition, hasGreeted, hasPaid bool
		if err := rows.Scan(&user.ID, &user.Email, &user.Role, &user.Status, &user.AIBalanceCents, &rawSubscription, &rawNotificationProfile, &user.CreatedAt, &lastLoginAt, &user.InviterEmail, &hasAgent, &hasAI, &hasPlatformAccount, &hasPosition, &hasGreeted, &hasPaid); err != nil {
			return AdminUserListResult{}, err
		}
		subscription, err := parseSubscription(rawSubscription)
		if err != nil {
			return AdminUserListResult{}, err
		}
		user.Subscription = subscription
		notificationProfile, err := decodeNotificationProfile(rawNotificationProfile)
		if err != nil {
			return AdminUserListResult{}, err
		}
		user.NotificationProfile = notificationProfile
		user.Flow = buildAdminUserFlow(hasAgent, hasAI, hasPlatformAccount, hasPosition, hasGreeted, hasPaid)
		if lastLoginAt.Valid {
			user.LastLoginAt = &lastLoginAt.Time
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return AdminUserListResult{}, err
	}
	return AdminUserListResult{Users: users, Total: total, Page: page, PageSize: pageSize}, nil
}

// Stats 读取 PostgreSQL 用户管理统计。
func (s *PostgresAdminUserStore) Stats() (AdminUserStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var stats AdminUserStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE u.created_at >= CURRENT_DATE)::int,
			(SELECT COUNT(*)::int FROM local_agents la WHERE la.bind_status = 'active')
		FROM users u
	`).Scan(&stats.TodayRegisteredCount, &stats.AgentBindingCount)
	return stats, err
}

// adminUserWhere 构建用户列表搜索条件。
// keyword 为搜索关键词，返回 WHERE SQL 和参数。
func adminUserWhere(keyword string) (string, []any) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "true", []any{}
	}
	return `(u.email ILIKE $1 OR COALESCE(u.role, 'user') ILIKE $1 OR COALESCE(u.status, 'active') ILIKE $1 OR COALESCE(inviter.email, '') ILIKE $1)`, []any{"%" + keyword + "%"}
}

// filterAdminUsers 根据关键词过滤内存用户列表。
// users 为用户列表，keyword 为空时返回原列表。
func filterAdminUsers(users []AdminUser, keyword string) []AdminUser {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return users
	}
	result := make([]AdminUser, 0, len(users))
	for _, user := range users {
		text := strings.ToLower(strings.Join([]string{user.Email, user.Role, user.Status, user.InviterEmail}, " "))
		if strings.Contains(text, keyword) {
			result = append(result, user)
		}
	}
	return result
}
