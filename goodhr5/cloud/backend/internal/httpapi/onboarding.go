// 本文件负责提供用户新手教学状态的 HTTP API 和存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
)

// OnboardingState 表示用户新手教学完成状态。
type OnboardingState struct {
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completed_at"`
}

// OnboardingStore 定义用户新手教学状态存储能力。
type OnboardingStore interface {
	// Get 读取指定用户的新手教学状态。
	Get(email string) (OnboardingState, error)
	// Complete 标记指定用户已完成新手教学。
	Complete(email string) (OnboardingState, error)
}

// OnboardingService 处理新手教学 HTTP 接口。
type OnboardingService struct {
	auth          *AuthService
	store         OnboardingStore
	systemConfigs SystemConfigStore
}

// NewOnboardingService 创建新手教学服务。
func NewOnboardingService(auth *AuthService, store OnboardingStore, systemConfigs SystemConfigStore) *OnboardingService {
	return &OnboardingService{auth: auth, store: store, systemConfigs: systemConfigs}
}

// Status 返回当前用户的新手教学状态和教学配置。
func (s *OnboardingService) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	state, err := s.store.Get(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load onboarding")
		return
	}
	config := map[string]any{
		"local_agent_download_url":         "",
		"local_agent_download_url_mac":     "",
		"local_agent_download_url_windows": "",
		"runtime_components":               map[string]any{},
		"trial_days":                       3,
	}
	if cfg, err := s.systemConfigs.Get("system.onboarding_config"); err == nil {
		_ = json.Unmarshal([]byte(cfg.ConfigValue), &config)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "onboarding": state, "config": config})
}

// Complete 标记当前用户已完成新手教学。
func (s *OnboardingService) Complete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	state, err := s.store.Complete(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "onboarding": state})
}

// MemoryOnboardingStore 在内存中保存用户新手教学状态。
type MemoryOnboardingStore struct {
	mu    sync.Mutex
	items map[string]OnboardingState
}

// NewMemoryOnboardingStore 创建内存新手教学状态存储。
func NewMemoryOnboardingStore() *MemoryOnboardingStore {
	return &MemoryOnboardingStore{items: map[string]OnboardingState{}}
}

// Get 读取内存新手教学状态。
func (s *MemoryOnboardingStore) Get(email string) (OnboardingState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[email], nil
}

// Complete 标记内存新手教学已完成。
func (s *MemoryOnboardingStore) Complete(email string) (OnboardingState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	state := OnboardingState{Completed: true, CompletedAt: &now}
	s.items[email] = state
	return state, nil
}

// PostgresOnboardingStore 在 PostgreSQL 中保存用户新手教学状态。
type PostgresOnboardingStore struct {
	db *sql.DB
}

// NewPostgresOnboardingStore 创建 PostgreSQL 新手教学状态存储。
func NewPostgresOnboardingStore(db *sql.DB) *PostgresOnboardingStore {
	return &PostgresOnboardingStore{db: db}
}

// Get 读取 PostgreSQL 新手教学状态。
func (s *PostgresOnboardingStore) Get(email string) (OnboardingState, error) {
	if _, err := ensureUserID(context.Background(), s.db, email); err != nil {
		return OnboardingState{}, err
	}
	var raw []byte
	err := s.db.QueryRow(`SELECT onboarding FROM users WHERE email=$1`, email).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return OnboardingState{}, nil
	}
	if err != nil {
		return OnboardingState{}, err
	}
	return parseOnboardingState(raw)
}

// Complete 标记 PostgreSQL 新手教学已完成。
func (s *PostgresOnboardingStore) Complete(email string) (OnboardingState, error) {
	if _, err := ensureUserID(context.Background(), s.db, email); err != nil {
		return OnboardingState{}, err
	}
	now := time.Now()
	payload, _ := json.Marshal(OnboardingState{Completed: true, CompletedAt: &now})
	_, err := s.db.Exec(`UPDATE users SET onboarding=$2::jsonb WHERE email=$1`, email, string(payload))
	if err != nil {
		return OnboardingState{}, err
	}
	return OnboardingState{Completed: true, CompletedAt: &now}, nil
}

// parseOnboardingState 解析数据库中的新手教学 JSON。
func parseOnboardingState(raw []byte) (OnboardingState, error) {
	var state OnboardingState
	if len(raw) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return OnboardingState{}, err
	}
	return state, nil
}
