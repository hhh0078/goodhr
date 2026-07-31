// 本文件负责提供用户订阅状态和订阅套餐的 HTTP API。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultMemberType = memberTypeMax
const defaultTrialDuration = 72 * time.Hour

// Subscription 表示用户当前会员订阅状态。
type Subscription struct {
	MemberType string    `json:"member_type"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// SubscriptionGrant 表示一笔需要按业务单号幂等发放的会员权益。
type SubscriptionGrant struct {
	OrderNo        string
	GrantType      string
	Email          string
	MemberType     string
	Days           int
	ReplaceFromNow bool
}

// SubscriptionStore 定义订阅信息读取接口。
type SubscriptionStore interface {
	// UserSubscription 读取指定邮箱的订阅信息，不存在时创建默认试用订阅。
	UserSubscription(email string) (Subscription, error)
	// UserSubscriptionWithCreated 读取或创建订阅，并返回是否本次创建了默认试用订阅。
	UserSubscriptionWithCreated(email string) (Subscription, bool, error)
	// ExtendSubscription 按会员类型和天数延长指定用户订阅。
	ExtendSubscription(email string, memberType string, days int) (Subscription, error)
	// AdjustSubscriptionDays 按正负天数调整指定用户订阅。
	AdjustSubscriptionDays(email string, memberType string, days int) (Subscription, error)
	// ReplaceSubscriptionFromNow 从当前时间重新计算指定会员类型和有效期。
	ReplaceSubscriptionFromNow(email string, memberType string, days int) (Subscription, error)
	// ApplyGrant 按订单号和权益类型幂等发放会员时间。
	ApplyGrant(grant SubscriptionGrant) (Subscription, bool, error)
}

// SubscriptionService 处理订阅状态和套餐接口。
type SubscriptionService struct {
	auth          *AuthService
	store         SubscriptionStore
	systemConfigs SystemConfigStore
}

// NewSubscriptionService 创建订阅服务。
func NewSubscriptionService(auth *AuthService, store SubscriptionStore, systemConfigs SystemConfigStore) *SubscriptionService {
	return &SubscriptionService{auth: auth, store: store, systemConfigs: systemConfigs}
}

// Status 返回当前用户订阅状态。
func (s *SubscriptionService) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	subscription, err := s.store.UserSubscription(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load subscription")
		return
	}
	access, err := subscriptionAccess(s.systemConfigs, subscription, time.Now())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "会员套餐配置暂时没读明白，请稍后再试")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"subscription": publicSubscriptionAccess(access),
	})
}

// Plans 返回系统配置里的订阅套餐列表。
func (s *SubscriptionService) Plans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg, err := s.systemConfigs.Get("system.subscription_plans")
	if err != nil {
		if errors.Is(err, ErrConfigNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "plans": []any{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load subscription plans")
		return
	}
	plans, err := parseSubscriptionPlans(cfg.ConfigValue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "subscription plans config is invalid")
		return
	}
	if plans == nil {
		plans = []subscriptionPlan{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "plans": plans})
}

// publicSubscription 转换订阅状态为前端响应。
func publicSubscription(subscription Subscription) map[string]any {
	return map[string]any{
		"member_type": subscription.MemberType,
		"member_name": memberTypeName(subscription.MemberType),
		"expires_at":  subscription.ExpiresAt,
		"active":      subscriptionActive(subscription),
	}
}

// subscriptionActive 判断订阅是否仍有效。
func subscriptionActive(subscription Subscription) bool {
	memberType := normalizeMemberType(subscription.MemberType)
	return (memberType == memberTypePlus || memberType == memberTypeMax) && time.Now().Before(subscription.ExpiresAt)
}

// ---------- 内存实现 ----------

type MemorySubscriptionStore struct {
	items   map[string]Subscription
	now     func() time.Time
	grantMu sync.Mutex
	grants  map[string]struct{}
}

// NewMemorySubscriptionStore 创建内存订阅存储。
func NewMemorySubscriptionStore() *MemorySubscriptionStore {
	return &MemorySubscriptionStore{
		items:  map[string]Subscription{},
		now:    time.Now,
		grants: map[string]struct{}{},
	}
}

// UserSubscription 读取或创建内存订阅。
func (s *MemorySubscriptionStore) UserSubscription(email string) (Subscription, error) {
	item, _, err := s.UserSubscriptionWithCreated(email)
	return item, err
}

// UserSubscriptionWithCreated 读取或创建内存订阅，并返回是否本次创建。
func (s *MemorySubscriptionStore) UserSubscriptionWithCreated(email string) (Subscription, bool, error) {
	if item, ok := s.items[email]; ok {
		return item, false, nil
	}
	item := Subscription{MemberType: defaultMemberType, ExpiresAt: s.now().Add(defaultTrialDuration)}
	s.items[email] = item
	return item, true, nil
}

// ExtendSubscription 按当前到期时间或当前时间延长内存订阅。
func (s *MemorySubscriptionStore) ExtendSubscription(email string, memberType string, days int) (Subscription, error) {
	if days <= 0 {
		return s.UserSubscription(email)
	}
	current, _ := s.UserSubscription(email)
	base := s.now()
	if current.ExpiresAt.After(base) {
		base = current.ExpiresAt
	}
	if memberType == "" {
		memberType = current.MemberType
	}
	if memberType == "" {
		memberType = defaultMemberType
	}
	current.MemberType = memberType
	current.ExpiresAt = base.Add(time.Duration(days) * 24 * time.Hour)
	s.items[email] = current
	return current, nil
}

// AdjustSubscriptionDays 按正负天数调整内存订阅。
func (s *MemorySubscriptionStore) AdjustSubscriptionDays(email string, memberType string, days int) (Subscription, error) {
	if days == 0 {
		return s.UserSubscription(email)
	}
	current, _ := s.UserSubscription(email)
	if memberType == "" {
		memberType = current.MemberType
	}
	if memberType == "" {
		memberType = defaultMemberType
	}
	base := current.ExpiresAt
	if days > 0 && base.Before(s.now()) {
		base = s.now()
	}
	current.MemberType = memberType
	current.ExpiresAt = base.Add(time.Duration(days) * 24 * time.Hour)
	s.items[email] = current
	return current, nil
}

// ReplaceSubscriptionFromNow 从当前时间重新计算内存订阅。
func (s *MemorySubscriptionStore) ReplaceSubscriptionFromNow(email string, memberType string, days int) (Subscription, error) {
	if days <= 0 {
		return s.UserSubscription(email)
	}
	if memberType == "" {
		memberType = defaultMemberType
	}
	current := Subscription{
		MemberType: memberType,
		ExpiresAt:  s.now().Add(time.Duration(days) * 24 * time.Hour),
	}
	s.items[email] = current
	return current, nil
}

// ApplyGrant 按订单号和权益类型幂等发放内存会员时间。
func (s *MemorySubscriptionStore) ApplyGrant(grant SubscriptionGrant) (Subscription, bool, error) {
	if err := validateSubscriptionGrant(grant); err != nil {
		return Subscription{}, false, err
	}
	s.grantMu.Lock()
	defer s.grantMu.Unlock()
	key := grant.OrderNo + "\x00" + grant.GrantType
	if _, exists := s.grants[key]; exists {
		current, err := s.UserSubscription(grant.Email)
		return current, false, err
	}
	var (
		subscription Subscription
		err          error
	)
	if grant.ReplaceFromNow {
		subscription, err = s.ReplaceSubscriptionFromNow(grant.Email, grant.MemberType, grant.Days)
	} else {
		subscription, err = s.ExtendSubscription(grant.Email, grant.MemberType, grant.Days)
	}
	if err != nil {
		return Subscription{}, false, err
	}
	s.grants[key] = struct{}{}
	return subscription, true, nil
}

// ---------- PostgreSQL 实现 ----------

type PostgresSubscriptionStore struct {
	db *sql.DB
}

// NewPostgresSubscriptionStore 创建 PostgreSQL 订阅存储。
func NewPostgresSubscriptionStore(db *sql.DB) *PostgresSubscriptionStore {
	return &PostgresSubscriptionStore{db: db}
}

// UserSubscription 读取或创建 PostgreSQL 用户订阅信息。
func (s *PostgresSubscriptionStore) UserSubscription(email string) (Subscription, error) {
	subscription, _, err := s.UserSubscriptionWithCreated(email)
	return subscription, err
}

// UserSubscriptionWithCreated 读取或创建 PostgreSQL 用户订阅，并返回是否本次创建。
func (s *PostgresSubscriptionStore) UserSubscriptionWithCreated(email string) (Subscription, bool, error) {
	var raw []byte
	err := s.db.QueryRow(
		`INSERT INTO users (email)
		 VALUES ($1)
		 ON CONFLICT (email) DO NOTHING
		 RETURNING subscription`,
		email,
	).Scan(&raw)
	if err == nil {
		subscription, parseErr := parseSubscription(raw)
		return subscription, true, parseErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Subscription{}, false, err
	}
	if _, err := ensureUserID(context.Background(), s.db, email); err != nil {
		return Subscription{}, false, err
	}
	err = s.db.QueryRow(`SELECT subscription FROM users WHERE email=$1`, email).Scan(&raw)
	if err != nil {
		return Subscription{}, false, err
	}
	subscription, parseErr := parseSubscription(raw)
	return subscription, false, parseErr
}

// ExtendSubscription 按当前到期时间或当前时间延长 PostgreSQL 用户订阅。
func (s *PostgresSubscriptionStore) ExtendSubscription(email string, memberType string, days int) (Subscription, error) {
	if days <= 0 {
		return s.UserSubscription(email)
	}
	if _, err := ensureUserID(context.Background(), s.db, email); err != nil {
		return Subscription{}, err
	}
	nextExpires := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	current, err := s.UserSubscription(email)
	if err == nil && current.ExpiresAt.After(time.Now()) {
		nextExpires = current.ExpiresAt.Add(time.Duration(days) * 24 * time.Hour)
	}
	if memberType == "" && err == nil {
		memberType = current.MemberType
	}
	if memberType == "" {
		memberType = defaultMemberType
	}
	payload, err := json.Marshal(Subscription{MemberType: memberType, ExpiresAt: nextExpires})
	if err != nil {
		return Subscription{}, err
	}
	_, err = s.db.Exec(
		`UPDATE users SET subscription=$2::jsonb WHERE email=$1`,
		email,
		string(payload),
	)
	if err != nil {
		return Subscription{}, err
	}
	return Subscription{MemberType: memberType, ExpiresAt: nextExpires}, nil
}

// AdjustSubscriptionDays 按正负天数调整 PostgreSQL 用户订阅。
func (s *PostgresSubscriptionStore) AdjustSubscriptionDays(email string, memberType string, days int) (Subscription, error) {
	if days == 0 {
		return s.UserSubscription(email)
	}
	if _, err := ensureUserID(context.Background(), s.db, email); err != nil {
		return Subscription{}, err
	}
	current, err := s.UserSubscription(email)
	if err != nil {
		return Subscription{}, err
	}
	if memberType == "" {
		memberType = current.MemberType
	}
	if memberType == "" {
		memberType = defaultMemberType
	}
	base := current.ExpiresAt
	if days > 0 && base.Before(time.Now()) {
		base = time.Now()
	}
	nextExpires := base.Add(time.Duration(days) * 24 * time.Hour)
	payload, err := json.Marshal(Subscription{MemberType: memberType, ExpiresAt: nextExpires})
	if err != nil {
		return Subscription{}, err
	}
	_, err = s.db.Exec(
		`UPDATE users SET subscription=$2::jsonb WHERE email=$1`,
		email,
		string(payload),
	)
	if err != nil {
		return Subscription{}, err
	}
	return Subscription{MemberType: memberType, ExpiresAt: nextExpires}, nil
}

// ReplaceSubscriptionFromNow 从当前时间重新计算 PostgreSQL 用户订阅。
func (s *PostgresSubscriptionStore) ReplaceSubscriptionFromNow(email string, memberType string, days int) (Subscription, error) {
	if days <= 0 {
		return s.UserSubscription(email)
	}
	if memberType == "" {
		memberType = defaultMemberType
	}
	if _, err := ensureUserID(context.Background(), s.db, email); err != nil {
		return Subscription{}, err
	}
	next := Subscription{
		MemberType: memberType,
		ExpiresAt:  time.Now().Add(time.Duration(days) * 24 * time.Hour),
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return Subscription{}, err
	}
	if _, err = s.db.Exec(`UPDATE users SET subscription=$2::jsonb WHERE email=$1`, email, string(payload)); err != nil {
		return Subscription{}, err
	}
	return next, nil
}

// ApplyGrant 在同一事务内按订单号发放一次 PostgreSQL 会员权益。
func (s *PostgresSubscriptionStore) ApplyGrant(grant SubscriptionGrant) (Subscription, bool, error) {
	if err := validateSubscriptionGrant(grant); err != nil {
		return Subscription{}, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	userID, err := ensureUserID(ctx, s.db, grant.Email)
	if err != nil {
		return Subscription{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Subscription{}, false, err
	}
	defer tx.Rollback()

	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT subscription FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&raw); err != nil {
		return Subscription{}, false, err
	}
	current, err := parseSubscription(raw)
	if err != nil {
		return Subscription{}, false, err
	}
	var appliedAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT applied_at
		FROM subscription_order_grants
		WHERE order_no=$1 AND grant_type=$2
	`, grant.OrderNo, grant.GrantType).Scan(&appliedAt)
	if err == nil {
		return current, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Subscription{}, false, err
	}

	next := applySubscriptionGrant(current, grant, time.Now())
	payload, err := json.Marshal(next)
	if err != nil {
		return Subscription{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET subscription=$2::jsonb WHERE id=$1`, userID, string(payload)); err != nil {
		return Subscription{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO subscription_order_grants (
			order_no, grant_type, user_id, user_email, member_type, duration_days,
			replace_from_now, subscription_expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, grant.OrderNo, grant.GrantType, userID, grant.Email, next.MemberType, grant.Days, grant.ReplaceFromNow, next.ExpiresAt); err != nil {
		return Subscription{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Subscription{}, false, err
	}
	return next, true, nil
}

// validateSubscriptionGrant 校验幂等会员权益发放参数。
func validateSubscriptionGrant(grant SubscriptionGrant) error {
	if strings.TrimSpace(grant.OrderNo) == "" || strings.TrimSpace(grant.GrantType) == "" {
		return errors.New("会员权益缺少业务单号或权益类型")
	}
	if strings.TrimSpace(grant.Email) == "" || grant.Days <= 0 {
		return errors.New("会员权益缺少用户或有效天数")
	}
	return nil
}

// applySubscriptionGrant 根据当前会员和发放方式计算新的会员状态。
func applySubscriptionGrant(current Subscription, grant SubscriptionGrant, now time.Time) Subscription {
	memberType := normalizeMemberType(grant.MemberType)
	if memberType == "" {
		memberType = normalizeMemberType(current.MemberType)
	}
	if memberType == "" {
		memberType = defaultMemberType
	}
	base := now
	if !grant.ReplaceFromNow && current.ExpiresAt.After(base) {
		base = current.ExpiresAt
	}
	return Subscription{
		MemberType: memberType,
		ExpiresAt:  base.Add(time.Duration(grant.Days) * 24 * time.Hour),
	}
}

// parseSubscription 解析数据库中的订阅 JSON。
func parseSubscription(raw []byte) (Subscription, error) {
	var payload struct {
		MemberType string `json:"member_type"`
		ExpiresAt  string `json:"expires_at"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Subscription{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, payload.ExpiresAt)
	if err != nil {
		return Subscription{}, err
	}
	return Subscription{MemberType: payload.MemberType, ExpiresAt: expiresAt}, nil
}
