// 本文件将登录会话持久保存到 PostgreSQL，并按原有效期兼容尚未迁入的 Redis 会话。
package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// PostgresAuthStore 使用数据库保存会话，短期验证码继续复用原认证存储。
type PostgresAuthStore struct {
	db *sql.DB
	AuthStore
}

// NewPostgresAuthStore 创建持久会话存储；legacy 同时提供验证码及旧会话兼容读取。
func NewPostgresAuthStore(db *sql.DB, legacy AuthStore) *PostgresAuthStore {
	return &PostgresAuthStore{db: db, AuthStore: legacy}
}

// authTokenHash 计算凭证摘要，避免数据库直接保存可使用的登录凭证。
func authTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// SaveSession 在首次登录时保存固定有效期，失败时不降级为易丢失的内存会话。
func (s *PostgresAuthStore) SaveSession(token string, session Session, ttl time.Duration) error {
	now := time.Now()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.ExpiresAt = now.Add(ttl)
	if err := s.persistSession(token, session); err != nil {
		return err
	}
	// 已过期会话按到期索引清理；清理失败不影响已成功保存的新会话。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at <= $1`, now)
	return nil
}

// persistSession 写入会话，重复迁移不会覆盖原记录或延长到期时间。
func (s *PostgresAuthStore) persistSession(token string, session Session) error {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(session.Email) == "" || session.ExpiresAt.IsZero() {
		return errors.New("登录会话数据不完整")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO auth_sessions (token_hash, email, created_at, expires_at)
		VALUES ($1, $2, $3, $4) ON CONFLICT (token_hash) DO NOTHING`,
		authTokenHash(token), session.Email, session.CreatedAt, session.ExpiresAt)
	return err
}

// GetSession 查询持久会话；仅在记录不存在时迁入有效旧会话，数据库故障不会绕过校验。
func (s *PostgresAuthStore) GetSession(token string) (Session, error) {
	if strings.TrimSpace(token) == "" {
		return Session{}, ErrSessionRequired
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var session Session
	err := s.db.QueryRowContext(ctx, `SELECT email, created_at, expires_at FROM auth_sessions WHERE token_hash=$1`,
		authTokenHash(token)).Scan(&session.Email, &session.CreatedAt, &session.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		if s.AuthStore == nil {
			return Session{}, ErrNotFound
		}
		session, err = s.AuthStore.GetSession(token)
		if err != nil {
			return Session{}, err
		}
		if !time.Now().Before(session.ExpiresAt) {
			return Session{}, ErrNotFound
		}
		if err = s.persistSession(token, session); err != nil {
			return Session{}, err
		}
	} else if err != nil {
		return Session{}, err
	}
	if session.Email == "" || !time.Now().Before(session.ExpiresAt) {
		return Session{}, ErrNotFound
	}
	return session, nil
}

// GetSessionUnsafe 保持现有通知兜底接口，仍拒绝已到期的会话。
func (s *PostgresAuthStore) GetSessionUnsafe(token string) (Session, error) {
	return s.GetSession(token)
}
