// 本文件负责提供邀请关系、邀请奖励和邀请页面接口。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// InviteConfig 表示邀请活动奖励配置。
type InviteConfig struct {
	RegisterRewardDays  int    `json:"register_reward_days"`
	PaidMonthRewardDays int    `json:"paid_month_reward_days"`
	ActivityTitle       string `json:"activity_title"`
	ActivityDescription string `json:"activity_description"`
}

// Invitee 表示当前用户邀请来的一个注册用户。
type Invitee struct {
	ID                         string     `json:"id"`
	Email                      string     `json:"email"`
	CreatedAt                  time.Time  `json:"created_at"`
	InviteRegisteredRewardedAt *time.Time `json:"invite_registered_rewarded_at,omitempty"`
}

// InvitationStore 定义邀请关系持久化能力。
type InvitationStore interface {
	// InviteID 读取或创建用户，并返回可用于邀请链接的用户ID。
	InviteID(email string) (string, error)
	// BindInviterIfPossible 在用户首次登录时绑定邀请人。
	BindInviterIfPossible(email string, inviterID string) (string, bool, error)
	// ListInvitees 列出当前用户邀请来的用户。
	ListInvitees(email string) ([]Invitee, error)
	// InviterEmailByInvitee 读取指定用户的邀请人邮箱。
	InviterEmailByInvitee(email string) (string, error)
}

// InvitationService 处理邀请页面接口。
type InvitationService struct {
	auth          *AuthService
	store         InvitationStore
	systemConfigs SystemConfigStore
}

// NewInvitationService 创建邀请服务。
func NewInvitationService(auth *AuthService, store InvitationStore, systemConfigs SystemConfigStore) *InvitationService {
	return &InvitationService{auth: auth, store: store, systemConfigs: systemConfigs}
}

// Summary 返回当前用户邀请信息和邀请活动配置。
func (s *InvitationService) Summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	inviteID, err := s.store.InviteID(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load invite id")
		return
	}
	invitees, err := s.store.ListInvitees(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invitees")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"invite_id":  inviteID,
		"config":     loadInviteConfig(s.systemConfigs),
		"invitees":   invitees,
		"invite_url": "",
	})
}

// loadInviteConfig 读取邀请奖励配置。
func loadInviteConfig(store SystemConfigStore) InviteConfig {
	config := InviteConfig{
		RegisterRewardDays:  3,
		PaidMonthRewardDays: 5,
		ActivityTitle:       "邀请好友奖励会员天数",
		ActivityDescription: "邀请好友注册成功后，邀请人可获得注册奖励；好友充值会员后，邀请人还可按购买月份获得额外会员天数。",
	}
	if store == nil {
		return config
	}
	cfg, err := store.Get("system.invite_config")
	if err != nil {
		return config
	}
	_ = json.Unmarshal([]byte(cfg.ConfigValue), &config)
	if config.RegisterRewardDays < 0 {
		config.RegisterRewardDays = 0
	}
	if config.PaidMonthRewardDays < 0 {
		config.PaidMonthRewardDays = 0
	}
	return config
}

// MemoryInvitationStore 提供开发期内存邀请存储。
type MemoryInvitationStore struct {
	mu        sync.Mutex
	ids       map[string]string
	emails    map[string]string
	inviters  map[string]string
	rewarded  map[string]time.Time
	createdAt map[string]time.Time
	nextID    int
}

// NewMemoryInvitationStore 创建内存邀请存储。
func NewMemoryInvitationStore() *MemoryInvitationStore {
	return &MemoryInvitationStore{
		ids:       map[string]string{},
		emails:    map[string]string{},
		inviters:  map[string]string{},
		rewarded:  map[string]time.Time{},
		createdAt: map[string]time.Time{},
	}
}

// InviteID 读取或创建内存用户邀请ID。
func (s *MemoryInvitationStore) InviteID(email string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inviteIDLocked(email), nil
}

// BindInviterIfPossible 绑定内存邀请人。
func (s *MemoryInvitationStore) BindInviterIfPossible(email string, inviterID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inviteeID := s.inviteIDLocked(email)
	if inviteeID == inviterID || s.inviters[email] != "" {
		return "", false, nil
	}
	inviterEmail := s.emails[inviterID]
	if inviterEmail == "" {
		return "", false, nil
	}
	now := time.Now()
	s.inviters[email] = inviterEmail
	s.rewarded[email] = now
	return inviterEmail, true, nil
}

// ListInvitees 列出内存邀请用户。
func (s *MemoryInvitationStore) ListInvitees(email string) ([]Invitee, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []Invitee{}
	for inviteeEmail, inviterEmail := range s.inviters {
		if inviterEmail != email {
			continue
		}
		rewardedAt := s.rewarded[inviteeEmail]
		item := Invitee{ID: s.ids[inviteeEmail], Email: inviteeEmail, CreatedAt: s.createdAt[inviteeEmail], InviteRegisteredRewardedAt: &rewardedAt}
		result = append(result, item)
	}
	return result, nil
}

// InviterEmailByInvitee 读取内存邀请人邮箱。
func (s *MemoryInvitationStore) InviterEmailByInvitee(email string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inviterEmail := s.inviters[email]
	if inviterEmail == "" {
		return "", ErrNotFound
	}
	return inviterEmail, nil
}

// inviteIDLocked 在持锁状态下读取或创建邀请ID。
func (s *MemoryInvitationStore) inviteIDLocked(email string) string {
	if id := s.ids[email]; id != "" {
		return id
	}
	s.nextID += 1
	id := fmt.Sprintf("mem-invite-%d", s.nextID)
	s.ids[email] = id
	s.emails[id] = email
	s.createdAt[email] = time.Now()
	return id
}

// PostgresInvitationStore 使用 PostgreSQL 持久化邀请关系。
type PostgresInvitationStore struct {
	db *sql.DB
}

// NewPostgresInvitationStore 创建 PostgreSQL 邀请存储。
func NewPostgresInvitationStore(db *sql.DB) *PostgresInvitationStore {
	return &PostgresInvitationStore{db: db}
}

// InviteID 读取或创建 PostgreSQL 用户邀请ID。
func (s *PostgresInvitationStore) InviteID(email string) (string, error) {
	return ensureUserID(context.Background(), s.db, email)
}

// BindInviterIfPossible 首次登录时写入邀请人并返回邀请人邮箱。
func (s *PostgresInvitationStore) BindInviterIfPossible(email string, inviterID string) (string, bool, error) {
	if inviterID == "" {
		if _, err := ensureUserID(context.Background(), s.db, email); err != nil {
			return "", false, err
		}
		return "", false, nil
	}
	var inviterEmail string
	err := s.db.QueryRow(`
		WITH invitee_row AS (
			INSERT INTO users (email)
			VALUES ($1)
			ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
			RETURNING id
		)
		UPDATE users invitee
		SET inviter_id = inviter.id,
		    invite_registered_rewarded_at = now()
		FROM users inviter, invitee_row
		WHERE invitee.email = $1
		  AND inviter.id::text = $2
		  AND invitee.id <> inviter.id
		  AND invitee.inviter_id IS NULL
		  AND invitee.invite_registered_rewarded_at IS NULL
		RETURNING inviter.email
		`, email, inviterID).Scan(&inviterEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return inviterEmail, true, nil
}

// ListInvitees 列出 PostgreSQL 邀请用户。
func (s *PostgresInvitationStore) ListInvitees(email string) ([]Invitee, error) {
	inviterID, err := ensureUserID(context.Background(), s.db, email)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
		SELECT id::text, email, created_at, invite_registered_rewarded_at
		FROM users
		WHERE inviter_id::text = $1
		ORDER BY created_at DESC
		`, inviterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Invitee{}
	for rows.Next() {
		var item Invitee
		if err := rows.Scan(&item.ID, &item.Email, &item.CreatedAt, &item.InviteRegisteredRewardedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// InviterEmailByInvitee 读取 PostgreSQL 邀请人邮箱。
func (s *PostgresInvitationStore) InviterEmailByInvitee(email string) (string, error) {
	var inviterEmail string
	err := s.db.QueryRow(`
		SELECT inviter.email
		FROM users invitee
		INNER JOIN users inviter ON inviter.id = invitee.inviter_id
		WHERE invitee.email = $1
		`, email).Scan(&inviterEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return inviterEmail, err
}
