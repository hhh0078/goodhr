// 本文件负责记录用户活跃时间等轻量账号行为。
package httpapi

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// UserActivityStore 定义用户活跃记录存储能力。
type UserActivityStore interface {
	// RecordLogin 记录指定邮箱最近一次登录成功时间。
	RecordLogin(email string, at time.Time) error
	// ShouldShowTrialWelcome 判断是否需要展示新用户试用会员到账弹框。
	ShouldShowTrialWelcome(email string) (bool, error)
	// AckTrialWelcome 记录用户已确认试用会员到账弹框。
	AckTrialWelcome(email string, at time.Time) error
}

// NoopUserActivityStore 提供无数据库环境下的空实现。
type NoopUserActivityStore struct{}

// RecordLogin 在无数据库环境下不做任何写入。
func (NoopUserActivityStore) RecordLogin(email string, at time.Time) error {
	return nil
}

// ShouldShowTrialWelcome 在无数据库环境下默认不展示弹框。
func (NoopUserActivityStore) ShouldShowTrialWelcome(email string) (bool, error) {
	return false, nil
}

// AckTrialWelcome 在无数据库环境下不做任何写入。
func (NoopUserActivityStore) AckTrialWelcome(email string, at time.Time) error {
	return nil
}

// MemoryUserActivityStore 在无数据库环境下记录用户活跃和弹框确认状态。
type MemoryUserActivityStore struct {
	mu              sync.Mutex
	trialWelcomeAck map[string]time.Time
}

// NewMemoryUserActivityStore 创建内存用户活跃存储。
func NewMemoryUserActivityStore() *MemoryUserActivityStore {
	return &MemoryUserActivityStore{trialWelcomeAck: make(map[string]time.Time)}
}

// RecordLogin 在内存环境下不需要记录登录时间。
func (s *MemoryUserActivityStore) RecordLogin(email string, at time.Time) error {
	return nil
}

// ShouldShowTrialWelcome 判断内存用户是否还未确认试用会员到账弹框。
func (s *MemoryUserActivityStore) ShouldShowTrialWelcome(email string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, acked := s.trialWelcomeAck[email]
	return !acked, nil
}

// AckTrialWelcome 记录内存用户已确认试用会员到账弹框。
func (s *MemoryUserActivityStore) AckTrialWelcome(email string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trialWelcomeAck[email] = at
	return nil
}

// PostgresUserActivityStore 使用 PostgreSQL 记录用户活跃信息。
type PostgresUserActivityStore struct {
	db *sql.DB
}

// NewPostgresUserActivityStore 创建 PostgreSQL 用户活跃存储。
func NewPostgresUserActivityStore(db *sql.DB) *PostgresUserActivityStore {
	return &PostgresUserActivityStore{db: db}
}

// RecordLogin 更新用户最近一次登录成功时间。
func (s *PostgresUserActivityStore) RecordLogin(email string, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at=$2 WHERE email=$1`, email, at)
	return err
}

// ShouldShowTrialWelcome 判断当前用户是否还未确认试用会员到账弹框。
func (s *PostgresUserActivityStore) ShouldShowTrialWelcome(email string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return false, err
	}
	var show bool
	err := s.db.QueryRowContext(ctx, `SELECT trial_welcome_ack_at IS NULL FROM users WHERE email=$1`, email).Scan(&show)
	return show, err
}

// AckTrialWelcome 记录用户已确认试用会员到账弹框。
func (s *PostgresUserActivityStore) AckTrialWelcome(email string, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET trial_welcome_ack_at=$2 WHERE email=$1`, email, at)
	return err
}
