// 本文件负责保存和查询用户密码摘要，兼容无数据库的本地开发模式与 PostgreSQL 生产环境。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

var ErrPasswordAlreadySet = errors.New("password already set")

// PasswordStore 定义用户账号存在性和密码摘要的持久化能力。
type PasswordStore interface {
	AccountStatus(email string) (exists bool, passwordSet bool, err error)
	PasswordHash(email string) (string, error)
	SavePasswordHash(email string, passwordHash string) error
	UpdatePasswordHash(email string, passwordHash string) error
}

// MemoryPasswordStore 为无数据库的本地开发环境保存临时账号密码。
type MemoryPasswordStore struct {
	mu       sync.Mutex
	accounts map[string]string
}

// NewMemoryPasswordStore 创建内存密码存储。
func NewMemoryPasswordStore() *MemoryPasswordStore {
	return &MemoryPasswordStore{accounts: make(map[string]string)}
}

// AccountStatus 返回内存账号是否存在以及是否已经设置密码。
func (s *MemoryPasswordStore) AccountStatus(email string) (bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash, exists := s.accounts[email]
	return exists, hash != "", nil
}

// PasswordHash 返回内存账号的密码摘要。
func (s *MemoryPasswordStore) PasswordHash(email string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash, exists := s.accounts[email]
	if !exists || hash == "" {
		return "", ErrNotFound
	}
	return hash, nil
}

// SavePasswordHash 保存内存账号的密码摘要，同时创建尚不存在的账号。
func (s *MemoryPasswordStore) SavePasswordHash(email string, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, exists := s.accounts[email]; exists && current != "" {
		return ErrPasswordAlreadySet
	}
	s.accounts[email] = passwordHash
	return nil
}

// UpdatePasswordHash 覆盖内存账号已有密码摘要，账号不存在时返回 ErrNotFound。
func (s *MemoryPasswordStore) UpdatePasswordHash(email string, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.accounts[email]; !exists {
		return ErrNotFound
	}
	s.accounts[email] = passwordHash
	return nil
}

// AddLegacyAccount 为测试和兼容流程添加一个尚未设置密码的老账号。
func (s *MemoryPasswordStore) AddLegacyAccount(email string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[email] = ""
}

// PostgresPasswordStore 使用 users 表保存用户密码摘要。
type PostgresPasswordStore struct {
	db *sql.DB
}

// NewPostgresPasswordStore 创建 PostgreSQL 密码存储。
func NewPostgresPasswordStore(db *sql.DB) *PostgresPasswordStore {
	return &PostgresPasswordStore{db: db}
}

// AccountStatus 返回 PostgreSQL 账号是否存在以及是否已经设置密码。
func (s *PostgresPasswordStore) AccountStatus(email string) (bool, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var passwordHash sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE email=$1`, email).Scan(&passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, passwordHash.Valid && passwordHash.String != "", nil
}

// PasswordHash 返回 PostgreSQL 账号的密码摘要。
func (s *PostgresPasswordStore) PasswordHash(email string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var passwordHash sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE email=$1`, email).Scan(&passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !passwordHash.Valid || passwordHash.String == "" {
		return "", ErrNotFound
	}
	return passwordHash.String, nil
}

// SavePasswordHash 保存 PostgreSQL 账号的密码摘要，同时创建尚不存在的账号。
func (s *PostgresPasswordStore) SavePasswordHash(email string, passwordHash string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET password_hash=EXCLUDED.password_hash
		WHERE NULLIF(users.password_hash, '') IS NULL`, email, passwordHash)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrPasswordAlreadySet
	}
	return nil
}

// UpdatePasswordHash 覆盖 PostgreSQL 账号已有密码摘要，账号不存在时返回 ErrNotFound。
func (s *PostgresPasswordStore) UpdatePasswordHash(email string, passwordHash string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash=$2 WHERE email=$1`, email, passwordHash)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
