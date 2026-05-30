// 本文件负责提供用户订阅状态和订阅套餐的 HTTP API。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const defaultMemberType = "plus"
const defaultTrialDuration = 72 * time.Hour

// Subscription 表示用户当前会员订阅状态。
type Subscription struct {
	MemberType string    `json:"member_type"`
	ExpiresAt  time.Time `json:"expires_at"`
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

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"subscription": publicSubscription(subscription),
	})
}

// Plans 返回系统配置里的订阅套餐列表。
func (s *SubscriptionService) Plans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, err := s.auth.SessionFromRequest(r); err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
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
	var plans []map[string]any
	if err := json.Unmarshal([]byte(cfg.ConfigValue), &plans); err != nil {
		writeError(w, http.StatusInternalServerError, "subscription plans config is invalid")
		return
	}
	if plans == nil {
		plans = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "plans": plans})
}

// publicSubscription 转换订阅状态为前端响应。
func publicSubscription(subscription Subscription) map[string]any {
	return map[string]any{
		"member_type": subscription.MemberType,
		"expires_at":  subscription.ExpiresAt,
		"active":      subscriptionActive(subscription),
	}
}

// subscriptionActive 判断订阅是否仍有效。
func subscriptionActive(subscription Subscription) bool {
	return subscription.MemberType != "" && time.Now().Before(subscription.ExpiresAt)
}

// ---------- 内存实现 ----------

type MemorySubscriptionStore struct {
	items map[string]Subscription
	now   func() time.Time
}

// NewMemorySubscriptionStore 创建内存订阅存储。
func NewMemorySubscriptionStore() *MemorySubscriptionStore {
	return &MemorySubscriptionStore{items: map[string]Subscription{}, now: time.Now}
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
	if memberType == "" {
		memberType = defaultMemberType
	}
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
	if memberType == "" {
		memberType = defaultMemberType
	}
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
