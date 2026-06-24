// 本文件负责记录用户活跃时间等轻量账号行为。
package httpapi

import (
	"context"
	"database/sql"
	"time"
)

// UserActivityStore 定义用户活跃记录存储能力。
type UserActivityStore interface {
	// RecordLogin 记录指定邮箱最近一次登录成功时间。
	RecordLogin(email string, at time.Time) error
}

// NoopUserActivityStore 提供无数据库环境下的空实现。
type NoopUserActivityStore struct{}

// RecordLogin 在无数据库环境下不做任何写入。
func (NoopUserActivityStore) RecordLogin(email string, at time.Time) error {
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
