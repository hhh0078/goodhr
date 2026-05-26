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
	if item, ok := s.items[email]; ok {
		return item, nil
	}
	item := Subscription{MemberType: defaultMemberType, ExpiresAt: s.now().Add(defaultTrialDuration)}
	s.items[email] = item
	return item, nil
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
	if _, err := ensureUserID(context.Background(), s.db, email); err != nil {
		return Subscription{}, err
	}
	var raw []byte
	err := s.db.QueryRow(`SELECT subscription FROM users WHERE email=$1`, email).Scan(&raw)
	if err != nil {
		return Subscription{}, err
	}
	return parseSubscription(raw)
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
