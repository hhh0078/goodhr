// 本文件负责超级管理员查看用户列表及全库金额统计，并手动调整用户会员天数和 AI 余额。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	HasOpenID           bool                `json:"has_openid"`
	Role                string              `json:"role"`
	Status              string              `json:"status"`
	InviterEmail        string              `json:"inviter_email"`
	Agent               *AgentBinding       `json:"agent,omitempty"`
	Subscription        Subscription        `json:"subscription"`
	NotificationProfile NotificationProfile `json:"notification_profile"`
	AIBalanceUnits      int64               `json:"ai_balance_units"`
	ResumeCount         int64               `json:"resume_count"`
	Flow                UserFlowState       `json:"flow"`
	CreatedAt           time.Time           `json:"created_at"`
	LastLoginAt         *time.Time          `json:"last_login_at,omitempty"`
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
// 钱包单位为 0.0001 元，充值单位为分；金额字符串提供无浮点损失的前端展示值。
type AdminUserStats struct {
	TodayRegisteredCount     int    `json:"today_registered_count"`
	AgentBindingCount        int    `json:"agent_binding_count"`
	TotalUserCount           int64  `json:"total_user_count"`
	TotalAIBalanceUnits      int64  `json:"total_ai_balance_units"`
	SignupGiftDeductionUnits int64  `json:"signup_gift_deduction_units"`
	RemainingAIBalanceUnits  int64  `json:"remaining_ai_balance_units"`
	AIRechargeAmountCents    int64  `json:"ai_recharge_amount_cents"`
	TotalAIBalance           string `json:"total_ai_balance"`
	SignupGiftDeduction      string `json:"signup_gift_deduction"`
	RemainingAIBalance       string `json:"remaining_ai_balance"`
	AIRechargeAmount         string `json:"ai_recharge_amount"`
}

// adminUserSignupGiftDeductionUnits 固定按每位当前用户赠送 0.50 元扣除统计金额，不跟随新的注册赠送配置变化。
const adminUserSignupGiftDeductionUnits int64 = 50 * aiWalletUnitsPerCent

// withFinancialTotals 按每位当前用户固定赠送 0.50 元计算统计扣除额，并保留四位余额精度和负值；不修改任何钱包数据。
func (stats AdminUserStats) withFinancialTotals() AdminUserStats {
	stats.SignupGiftDeductionUnits = stats.TotalUserCount * adminUserSignupGiftDeductionUnits
	stats.RemainingAIBalanceUnits = stats.TotalAIBalanceUnits - stats.SignupGiftDeductionUnits
	stats.TotalAIBalance = aiUnitsToYuanString(stats.TotalAIBalanceUnits)
	stats.SignupGiftDeduction = aiUnitsToYuanString(stats.SignupGiftDeductionUnits)
	stats.RemainingAIBalance = aiUnitsToYuanString(stats.RemainingAIBalanceUnits)
	stats.AIRechargeAmount = adminStatsCentsToYuanString(stats.AIRechargeAmountCents)
	return stats
}

// adminStatsCentsToYuanString 使用整数计算分到元的两位小数表示，避免汇总充值金额时丢失浮点精度。
func adminStatsCentsToYuanString(cents int64) string {
	whole, fraction, sign := cents/100, cents%100, ""
	if cents < 0 {
		sign, whole, fraction = "-", -whole, -fraction
	}
	return fmt.Sprintf("%s%d.%02d", sign, whole, fraction)
}

// AdminUserStore 定义用户管理读取接口。
type AdminUserStore interface {
	// ListUsers 读取用户分页列表。
	ListUsers(query AdminUserListQuery) (AdminUserListResult, error)
	// Stats 读取用户管理统计数据。
	Stats() (AdminUserStats, error)
}

type adjustUserSubscriptionRequest struct {
	Email      string `json:"email"`
	Days       int    `json:"days"`
	MemberType string `json:"member_type"`
	Reason     string `json:"reason"`
}

type adjustUserAIBalanceRequest struct {
	Email       string `json:"email"`
	AmountCents int    `json:"amount_cents"`
	AmountYuan  string `json:"amount_yuan"`
	Reason      string `json:"reason"`
}

// batchAdjustUsersRequest 表示一次批量调整会员天数和 AI 余额的请求。
type batchAdjustUsersRequest struct {
	Target      string   `json:"target"`
	Emails      []string `json:"emails"`
	Days        int      `json:"days"`
	AmountCents int      `json:"amount_cents"`
	AmountYuan  string   `json:"amount_yuan"`
	Reason      string   `json:"reason"`
}

// batchAdjustUserResult 表示一个用户的批量调整结果。
type batchAdjustUserResult struct {
	Email           string   `json:"email"`
	DaysAdjusted    bool     `json:"days_adjusted"`
	BalanceAdjusted bool     `json:"balance_adjusted"`
	Errors          []string `json:"errors"`
}

// adjustmentNoticeError 表示数据调整成功但通知邮件发送失败。
type adjustmentNoticeError struct {
	err error
}

// Error 返回通知邮件发送失败原因。
func (e adjustmentNoticeError) Error() string {
	return e.err.Error()
}

// Unwrap 返回底层邮件错误，方便统一判断错误类型。
func (e adjustmentNoticeError) Unwrap() error {
	return e.err
}

type unbindUserAgentRequest struct {
	Email string `json:"email"`
}

// AdminUserService 处理超级管理员用户管理接口。
type AdminUserService struct {
	auth          *AuthService
	users         AdminUserStore
	subscriptions SubscriptionStore
	systemConfigs SystemConfigStore
	mailer        Mailer
	agents        AgentStore
	aiWallet      AIWalletStore
}

// NewAdminUserService 创建超级管理员用户管理服务。
func NewAdminUserService(auth *AuthService, users AdminUserStore, subscriptions SubscriptionStore, systemConfigs SystemConfigStore, mailer Mailer, agents AgentStore, aiWallet AIWalletStore) *AdminUserService {
	return &AdminUserService{auth: auth, users: users, subscriptions: subscriptions, systemConfigs: systemConfigs, mailer: mailer, agents: agents, aiWallet: aiWallet}
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
		if _, memoryStore := s.users.(*MemoryAdminUserStore); memoryStore && s.auth.wechatFollowStore != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			status, statusErr := s.auth.wechatFollowStore.BindingStatus(ctx, user.Email)
			cancel()
			if statusErr == nil {
				user.HasOpenID = status.Bound
			}
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
	memberType := normalizeMemberType(req.MemberType)
	if memberType != "" && memberType != memberTypePlus && memberType != memberTypeMax {
		writeError(w, http.StatusBadRequest, "会员类型只能选择 Plus 或 Max")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "超级管理员调整会员天数"
	}

	subscription, err := s.adjustSubscriptionForUser(email, req.Days, memberType, reason)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send subscription notice")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"subscription": publicSubscription(subscription),
	})
}

// UnbindAgent 解除指定用户全部有效设备占用，使这些电脑可以重新绑定其他账号。
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
	balance, err := s.adjustAIBalanceForUser(email, amountCents, reason)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed adjust ai balance")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "balance_units": balance, "balance_cents": aiUnitsToCents(balance), "balance": aiUnitsToYuanString(balance)})
}

// BatchAdjust 批量调整指定用户或全部用户的会员天数和 AI 余额。
func (s *AdminUserService) BatchAdjust(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req batchAdjustUsersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	amountCents, err := batchAdjustmentAmountCents(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "余额金额不太对，我没敢动。")
		return
	}
	if req.Days == 0 && amountCents == 0 {
		writeError(w, http.StatusBadRequest, "天数和余额至少填一个，我才能开工。")
		return
	}
	emails, err := s.batchAdjustmentEmails(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(emails) == 0 {
		writeError(w, http.StatusBadRequest, "没有找到要调整的用户")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "超级管理员批量调整"
	}
	results := make([]batchAdjustUserResult, 0, len(emails))
	successCount := 0
	for _, email := range emails {
		result := batchAdjustUserResult{Email: email, Errors: []string{}}
		if req.Days != 0 {
			if _, err := s.adjustSubscriptionForUser(email, req.Days, "", reason); err != nil {
				var noticeErr adjustmentNoticeError
				if errors.As(err, &noticeErr) {
					result.DaysAdjusted = true
					result.Errors = append(result.Errors, "会员天数已调整，但通知邮件发送失败")
				} else {
					result.Errors = append(result.Errors, "会员天数调整失败："+err.Error())
				}
			} else {
				result.DaysAdjusted = true
			}
		}
		if amountCents != 0 {
			if _, err := s.adjustAIBalanceForUser(email, amountCents, reason); err != nil {
				var noticeErr adjustmentNoticeError
				if errors.As(err, &noticeErr) {
					result.BalanceAdjusted = true
					result.Errors = append(result.Errors, "AI 余额已调整，但通知邮件发送失败")
				} else {
					result.Errors = append(result.Errors, "AI 余额调整失败："+err.Error())
				}
			} else {
				result.BalanceAdjusted = true
			}
		}
		if len(result.Errors) == 0 {
			successCount++
		}
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"total_count":   len(results),
		"success_count": successCount,
		"failed_count":  len(results) - successCount,
		"results":       results,
	})
}

// requireSuperAdmin 校验请求是否来自超级管理员。
func (s *AdminUserService) requireSuperAdmin(w http.ResponseWriter, r *http.Request) bool {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return false
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "super admin access required")
		return false
	}
	return true
}

// adjustSubscriptionForUser 调整单个用户会员天数并发送通知邮件。
func (s *AdminUserService) adjustSubscriptionForUser(email string, days int, memberType string, reason string) (Subscription, error) {
	subscription, err := s.subscriptions.AdjustSubscriptionDays(email, memberType, days)
	if err != nil {
		return Subscription{}, err
	}
	if err := sendSubscriptionRewardNotice(s.mailer, s.systemConfigs, email, SubscriptionRewardNotice{
		Reason:     reason,
		Days:       days,
		MemberType: subscription.MemberType,
		ExpiresAt:  subscription.ExpiresAt,
	}); err != nil {
		return Subscription{}, adjustmentNoticeError{err: err}
	}
	return subscription, nil
}

// adjustAIBalanceForUser 调整单个用户 AI 余额、写入流水并发送通知邮件。
func (s *AdminUserService) adjustAIBalanceForUser(email string, amountCents int, reason string) (int64, error) {
	if s.aiWallet == nil {
		return 0, fmt.Errorf("ai wallet is not ready")
	}
	changeUnits := centsToAIUnits(amountCents)
	balance, err := s.aiWallet.AdjustBalance(AIWalletRecord{
		UserEmail:   email,
		ChangeUnits: changeUnits,
		Category:    "admin_adjust",
		Reason:      reason,
	})
	if err != nil {
		return 0, err
	}
	if err := sendAIBalanceNotice(s.mailer, email, AIBalanceNotice{
		Reason:       reason,
		ChangeUnits:  changeUnits,
		BalanceUnits: balance,
	}); err != nil {
		return 0, adjustmentNoticeError{err: err}
	}
	return balance, nil
}

// batchAdjustmentAmountCents 解析批量调整请求中的余额金额。
func batchAdjustmentAmountCents(req batchAdjustUsersRequest) (int, error) {
	if req.AmountCents != 0 {
		return req.AmountCents, nil
	}
	if strings.TrimSpace(req.AmountYuan) == "" {
		return 0, nil
	}
	return yuanTextToCents(req.AmountYuan)
}

// batchAdjustmentEmails 解析批量调整目标，all 会读取系统内全部用户。
func (s *AdminUserService) batchAdjustmentEmails(req batchAdjustUsersRequest) ([]string, error) {
	all := strings.EqualFold(strings.TrimSpace(req.Target), "all")
	seen := map[string]bool{}
	for _, raw := range req.Emails {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '，' || r == '\n' || r == ';' || r == '；' || r == ' '
		}) {
			if strings.EqualFold(strings.TrimSpace(part), "all") {
				all = true
				continue
			}
			if email, ok := normalizeEmail(part); ok {
				seen[email] = true
			}
		}
	}
	if all {
		seen = map[string]bool{}
		for page := 1; ; page++ {
			result, err := s.users.ListUsers(AdminUserListQuery{Page: page, PageSize: 100})
			if err != nil {
				return nil, err
			}
			for _, user := range result.Users {
				if email, ok := normalizeEmail(user.Email); ok {
					seen[email] = true
				}
			}
			if page*result.PageSize >= result.Total || len(result.Users) == 0 {
				break
			}
		}
	}
	emails := make([]string, 0, len(seen))
	for email := range seen {
		emails = append(emails, email)
	}
	sort.Strings(emails)
	return emails, nil
}

// publicAdminUser 转换用户信息为前端响应。
func publicAdminUser(user AdminUser) map[string]any {
	return map[string]any{
		"id":                   user.ID,
		"email":                user.Email,
		"has_openid":           user.HasOpenID,
		"role":                 user.Role,
		"status":               user.Status,
		"inviter_email":        user.InviterEmail,
		"agent":                publicAdminAgent(user.Agent),
		"subscription":         publicSubscription(user.Subscription),
		"notification_profile": user.NotificationProfile,
		"ai_balance_units":     user.AIBalanceUnits,
		"ai_balance_cents":     aiUnitsToCents(user.AIBalanceUnits),
		"ai_balance":           aiUnitsToYuanString(user.AIBalanceUnits),
		"resume_count":         user.ResumeCount,
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
	aiWallet      AIWalletStore
	payments      PaymentStore
	candidates    *MemoryCandidateStore
}

// NewMemoryAdminUserStore 创建内存用户管理存储，复用订阅、钱包、支付和可选简历存储计算统计。
func NewMemoryAdminUserStore(subscriptions *MemorySubscriptionStore, aiWallet AIWalletStore, payments PaymentStore, candidates ...CandidateStore) *MemoryAdminUserStore {
	store := &MemoryAdminUserStore{subscriptions: subscriptions, aiWallet: aiWallet, payments: payments}
	if len(candidates) > 0 {
		store.candidates, _ = candidates[0].(*MemoryCandidateStore)
	}
	return store
}

// ListUsers 读取内存用户分页列表。
func (s *MemoryAdminUserStore) ListUsers(query AdminUserListQuery) (AdminUserListResult, error) {
	if s == nil || s.subscriptions == nil {
		return AdminUserListResult{Users: []AdminUser{}, Page: 1, PageSize: 20}, nil
	}
	resumeCounts := s.resumeCounts()
	users := make([]AdminUser, 0, len(s.subscriptions.items))
	for email, subscription := range s.subscriptions.items {
		users = append(users, AdminUser{
			ID:           email,
			Email:        email,
			Role:         "user",
			Status:       "active",
			Subscription: subscription,
			ResumeCount:  resumeCounts[email],
			Flow:         defaultUserFlowState(),
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

// resumeCounts 在同一锁内按首次创建账号汇总真实简历，不计共享、岗位关联和内置示例。
func (s *MemoryAdminUserStore) resumeCounts() map[string]int64 {
	counts := map[string]int64{}
	if s.candidates == nil {
		return counts
	}
	s.candidates.mu.Lock()
	defer s.candidates.mu.Unlock()
	for _, candidate := range s.candidates.profiles {
		if candidate.ID != builtinCandidateID {
			counts[candidate.UserEmail]++
		}
	}
	return counts
}

// Stats 读取内存用户管理统计。
func (s *MemoryAdminUserStore) Stats() (AdminUserStats, error) {
	if s == nil || s.subscriptions == nil {
		return (AdminUserStats{}).withFinancialTotals(), nil
	}
	today := s.subscriptions.now().Format(time.DateOnly)
	count := 0
	var stats AdminUserStats
	for email := range s.subscriptions.items {
		// 内存订阅没有真实注册时间，测试环境按当前用户数计算今日注册。
		count++
		stats.TotalUserCount++
		if s.aiWallet != nil {
			balance, err := s.aiWallet.BalanceUnits(email)
			if err != nil {
				return AdminUserStats{}, err
			}
			stats.TotalAIBalanceUnits += balance
		}
	}
	if today == "" {
		count = 0
	}
	stats.TodayRegisteredCount = count
	if s.payments != nil {
		orders, err := s.payments.ListAll()
		if err != nil {
			return AdminUserStats{}, err
		}
		for _, order := range orders {
			if order.OrderType == "ai_balance" && order.Status == "paid" {
				stats.AIRechargeAmountCents += int64(order.AmountCents)
			}
		}
	}
	return stats.withFinancialTotals(), nil
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
			COALESCE(u.wechat_openid,'') <> '',
			COALESCE(u.role, 'user'),
			COALESCE(u.status, 'active'),
			COALESCE(u.ai_balance_units, 0),
			COALESCE(resume_stats.resume_count, 0),
			u.subscription,
			u.notification_profile,
			u.created_at,
			u.last_login_at,
			COALESCE(inviter.email, ''),
			COALESCE(u.flow_state, '{}'::jsonb)
		FROM users u
		LEFT JOIN users inviter ON inviter.id = u.inviter_id
		LEFT JOIN (
			SELECT cp.created_by_user_id, COUNT(*) AS resume_count
			FROM candidate_profiles cp
			WHERE cp.id <> '`+builtinCandidateID+`'::uuid
			GROUP BY cp.created_by_user_id
		) resume_stats ON resume_stats.created_by_user_id = u.id
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
		var rawFlow []byte
		var lastLoginAt sql.NullTime
		if err := rows.Scan(&user.ID, &user.Email, &user.HasOpenID, &user.Role, &user.Status, &user.AIBalanceUnits, &user.ResumeCount, &rawSubscription, &rawNotificationProfile, &user.CreatedAt, &lastLoginAt, &user.InviterEmail, &rawFlow); err != nil {
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
		flow, err := parseUserFlowState(rawFlow)
		if err != nil {
			return AdminUserListResult{}, err
		}
		user.Flow = flow
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
			(SELECT COUNT(*)::int FROM local_agents la WHERE la.bind_status = 'active'),
			COUNT(*),
			COALESCE(SUM(u.ai_balance_units), 0)::bigint,
			(SELECT COALESCE(SUM(po.amount_cents), 0)::bigint
			 FROM payment_orders po WHERE po.order_type = 'ai_balance' AND po.status = 'paid')
		FROM users u
	`).Scan(&stats.TodayRegisteredCount, &stats.AgentBindingCount, &stats.TotalUserCount, &stats.TotalAIBalanceUnits, &stats.AIRechargeAmountCents)
	return stats.withFinancialTotals(), err
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
