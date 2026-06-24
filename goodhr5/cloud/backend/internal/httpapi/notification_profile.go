// 本文件负责提供用户邮件通知画像的 HTTP API 和存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

var notificationUserTypes = []string{"headhunter", "hr", "recruiting_manager", "owner"}
var notificationGenders = []string{"female", "male", "unknown"}

// NotificationProfile 表示用户用于邮件通知分组的画像。
type NotificationProfile struct {
	Completed   bool       `json:"completed"`
	DismissedAt *time.Time `json:"dismissed_at"`
	UserType    string     `json:"user_type"`
	Gender      string     `json:"gender"`
	Platforms   []string   `json:"platforms"`
	OS          string     `json:"os"`
	Browser     string     `json:"browser"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

// NotificationProfileStore 定义邮件通知画像存储能力。
type NotificationProfileStore interface {
	GetNotificationProfile(email string) (NotificationProfile, error)
	SaveNotificationProfile(email string, profile NotificationProfile) (NotificationProfile, error)
}

// NotificationProfileService 处理邮件通知画像 HTTP 接口。
type NotificationProfileService struct {
	auth  *AuthService
	store NotificationProfileStore
}

// NewNotificationProfileService 创建邮件通知画像服务。
func NewNotificationProfileService(auth *AuthService, store NotificationProfileStore) *NotificationProfileService {
	return &NotificationProfileService{auth: auth, store: store}
}

// User 读取或保存当前用户的邮件通知画像。
func (s *NotificationProfileService) User(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.Get(w, r)
	case http.MethodPut:
		s.Update(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// Get 返回当前用户的邮件通知画像。
func (s *NotificationProfileService) Get(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session invalid or expired")
		return
	}
	profile, err := s.store.GetNotificationProfile(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed load notification profile")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "profile": profile})
}

// Update 保存当前用户的邮件通知画像。
func (s *NotificationProfileService) Update(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session invalid or expired")
		return
	}
	var req struct {
		Completed bool     `json:"completed"`
		Dismissed bool     `json:"dismissed"`
		UserType  string   `json:"user_type"`
		Gender    string   `json:"gender"`
		Platforms []string `json:"platforms"`
		OS        string   `json:"os"`
		Browser   string   `json:"browser"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	now := time.Now()
	profile := NotificationProfile{
		Completed: req.Completed,
		UserType:  strings.TrimSpace(req.UserType),
		Gender:    strings.TrimSpace(req.Gender),
		Platforms: cleanNotificationPlatforms(req.Platforms),
		OS:        strings.TrimSpace(req.OS),
		Browser:   strings.TrimSpace(req.Browser),
		UpdatedAt: &now,
	}
	if profile.Gender == "" {
		profile.Gender = "female"
	}
	if req.Dismissed {
		profile.DismissedAt = &now
	}
	if profile.Completed {
		if !slices.Contains(notificationUserTypes, profile.UserType) {
			writeError(w, http.StatusBadRequest, "invalid user_type")
			return
		}
		if !slices.Contains(notificationGenders, profile.Gender) {
			writeError(w, http.StatusBadRequest, "invalid gender")
			return
		}
	}
	saved, err := s.store.SaveNotificationProfile(session.Email, profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed save notification profile")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "profile": saved})
}

// DefaultNotificationProfile 返回默认邮件通知画像。
func DefaultNotificationProfile() NotificationProfile {
	return NotificationProfile{Gender: "female", Platforms: []string{}}
}

// cleanNotificationPlatforms 去重并清理平台名称。
func cleanNotificationPlatforms(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// MemoryNotificationProfileStore 提供开发期使用的内存邮件通知画像存储。
type MemoryNotificationProfileStore struct {
	mu    sync.Mutex
	items map[string]NotificationProfile
}

// NewMemoryNotificationProfileStore 创建内存邮件通知画像存储。
func NewMemoryNotificationProfileStore() *MemoryNotificationProfileStore {
	return &MemoryNotificationProfileStore{items: map[string]NotificationProfile{}}
}

// GetNotificationProfile 读取内存邮件通知画像。
func (s *MemoryNotificationProfileStore) GetNotificationProfile(email string) (NotificationProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, ok := s.items[email]
	if !ok {
		return DefaultNotificationProfile(), nil
	}
	return profile, nil
}

// SaveNotificationProfile 保存内存邮件通知画像。
func (s *MemoryNotificationProfileStore) SaveNotificationProfile(email string, profile NotificationProfile) (NotificationProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[email] = profile
	return profile, nil
}

// PostgresNotificationProfileStore 提供 PostgreSQL 邮件通知画像存储。
type PostgresNotificationProfileStore struct {
	db *sql.DB
}

// NewPostgresNotificationProfileStore 创建 PostgreSQL 邮件通知画像存储。
func NewPostgresNotificationProfileStore(db *sql.DB) *PostgresNotificationProfileStore {
	return &PostgresNotificationProfileStore{db: db}
}

// GetNotificationProfile 读取 PostgreSQL 邮件通知画像。
func (s *PostgresNotificationProfileStore) GetNotificationProfile(email string) (NotificationProfile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return NotificationProfile{}, err
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT notification_profile FROM users WHERE email=$1`, email).Scan(&raw)
	if err != nil {
		return NotificationProfile{}, err
	}
	return decodeNotificationProfile(raw)
}

// SaveNotificationProfile 保存 PostgreSQL 邮件通知画像。
func (s *PostgresNotificationProfileStore) SaveNotificationProfile(email string, profile NotificationProfile) (NotificationProfile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return NotificationProfile{}, err
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return NotificationProfile{}, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET notification_profile=$2::jsonb WHERE email=$1`, email, string(raw))
	if err != nil {
		return NotificationProfile{}, err
	}
	return profile, nil
}

// decodeNotificationProfile 将数据库 JSON 转换为邮件通知画像。
func decodeNotificationProfile(raw []byte) (NotificationProfile, error) {
	profile := DefaultNotificationProfile()
	if len(raw) == 0 {
		return profile, nil
	}
	if err := json.Unmarshal(raw, &profile); err != nil {
		return NotificationProfile{}, err
	}
	if profile.Gender == "" {
		profile.Gender = "female"
	}
	if profile.Platforms == nil {
		profile.Platforms = []string{}
	}
	return profile, nil
}
