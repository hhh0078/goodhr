// 本文件负责 WorkBuddy 连接器 OAuth 数据（客户端、授权码、刷新令牌）的存储接口和实现。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// WorkBuddyOAuthClient 表示一个通过动态注册接入的 MCP 客户端。
type WorkBuddyOAuthClient struct {
	ClientID    string
	ClientName  string
	RedirectURI []string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
}

// WorkBuddyOAuthCode 表示一次未消费的授权码记录。
type WorkBuddyOAuthCode struct {
	Code          string
	ClientID      string
	UserEmail     string
	RedirectURI   string
	Scope         string
	CodeChallenge string
	ExpiresAt     time.Time
}

// WorkBuddyRefreshToken 表示一条刷新令牌记录，RotatedAt 非空代表已被轮换消费。
type WorkBuddyRefreshToken struct {
	Token      string
	ClientID   string
	UserEmail  string
	Scope      string
	ExpiresAt  time.Time
	RotatedAt  *time.Time
}

// WorkBuddyOAuthStore 定义连接器 OAuth 客户端、授权码和刷新令牌的持久化能力。
type WorkBuddyOAuthStore interface {
	// SaveClient 保存动态注册的客户端记录。
	SaveClient(client WorkBuddyOAuthClient) error
	// ClientByID 按 client_id 读取客户端，不存在时返回 ErrNotFound。
	ClientByID(clientID string) (WorkBuddyOAuthClient, error)
	// TouchClient 更新客户端最近使用时间，失败只影响统计不影响授权。
	TouchClient(clientID string) error
	// SaveCode 保存授权码。
	SaveCode(code WorkBuddyOAuthCode) error
	// ConsumeCode 原子取出并删除授权码，已消费或不存在时返回 ErrNotFound。
	ConsumeCode(code string) (WorkBuddyOAuthCode, error)
	// SaveRefreshToken 保存新的刷新令牌。
	SaveRefreshToken(token WorkBuddyRefreshToken) error
	// ConsumeRefreshToken 原子标记刷新令牌已轮换并返回内容，已消费或不存在时返回 ErrNotFound。
	ConsumeRefreshToken(token string) (WorkBuddyRefreshToken, error)
}

// MemoryWorkBuddyOAuthStore 提供开发期使用的内存 OAuth 存储。
type MemoryWorkBuddyOAuthStore struct {
	mu      sync.Mutex
	clients map[string]WorkBuddyOAuthClient
	codes   map[string]WorkBuddyOAuthCode
	refresh map[string]WorkBuddyRefreshToken
	now     func() time.Time
}

// NewMemoryWorkBuddyOAuthStore 创建内存 OAuth 存储。
func NewMemoryWorkBuddyOAuthStore() *MemoryWorkBuddyOAuthStore {
	return &MemoryWorkBuddyOAuthStore{
		clients: map[string]WorkBuddyOAuthClient{},
		codes:   map[string]WorkBuddyOAuthCode{},
		refresh: map[string]WorkBuddyRefreshToken{},
		now:     time.Now,
	}
}

// SaveClient 保存内存中的客户端记录。
func (s *MemoryWorkBuddyOAuthStore) SaveClient(client WorkBuddyOAuthClient) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[client.ClientID] = client
	return nil
}

// ClientByID 读取内存中的客户端记录。
func (s *MemoryWorkBuddyOAuthStore) ClientByID(clientID string) (WorkBuddyOAuthClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	client, ok := s.clients[clientID]
	if !ok {
		return WorkBuddyOAuthClient{}, ErrNotFound
	}
	return client, nil
}

// TouchClient 更新内存中客户端最近使用时间。
func (s *MemoryWorkBuddyOAuthStore) TouchClient(clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if client, ok := s.clients[clientID]; ok {
		client.LastUsedAt = &now
		s.clients[clientID] = client
	}
	return nil
}

// SaveCode 保存内存中的授权码。
func (s *MemoryWorkBuddyOAuthStore) SaveCode(code WorkBuddyOAuthCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[code.Code] = code
	return nil
}

// ConsumeCode 原子取出并删除内存中的授权码。
func (s *MemoryWorkBuddyOAuthStore) ConsumeCode(code string) (WorkBuddyOAuthCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.codes[code]
	if !ok || s.now().After(item.ExpiresAt) {
		delete(s.codes, code)
		return WorkBuddyOAuthCode{}, ErrNotFound
	}
	delete(s.codes, code)
	return item, nil
}

// SaveRefreshToken 保存内存中的刷新令牌。
func (s *MemoryWorkBuddyOAuthStore) SaveRefreshToken(token WorkBuddyRefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh[token.Token] = token
	return nil
}

// ConsumeRefreshToken 原子标记内存中的刷新令牌已轮换。
func (s *MemoryWorkBuddyOAuthStore) ConsumeRefreshToken(token string) (WorkBuddyRefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.refresh[token]
	if !ok || item.RotatedAt != nil || s.now().After(item.ExpiresAt) {
		delete(s.refresh, token)
		return WorkBuddyRefreshToken{}, ErrNotFound
	}
	now := s.now()
	item.RotatedAt = &now
	s.refresh[token] = item
	return item, nil
}

// PostgresWorkBuddyOAuthStore 使用 PostgreSQL 持久化连接器 OAuth 数据。
type PostgresWorkBuddyOAuthStore struct {
	db *sql.DB
}

// NewPostgresWorkBuddyOAuthStore 创建 PostgreSQL OAuth 存储。
func NewPostgresWorkBuddyOAuthStore(db *sql.DB) *PostgresWorkBuddyOAuthStore {
	return &PostgresWorkBuddyOAuthStore{db: db}
}

// SaveClient 保存或更新 PostgreSQL 中的客户端记录。
func (s *PostgresWorkBuddyOAuthStore) SaveClient(client WorkBuddyOAuthClient) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := json.Marshal(client.RedirectURI)
	if err != nil {
		raw = []byte("[]")
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO workbuddy_oauth_clients (client_id, client_name, redirect_uris)
		VALUES ($1, $2, $3)
		ON CONFLICT (client_id) DO UPDATE SET client_name = EXCLUDED.client_name, redirect_uris = EXCLUDED.redirect_uris
	`, client.ClientID, client.ClientName, string(raw))
	return err
}

// ClientByID 读取 PostgreSQL 中的客户端记录。
func (s *PostgresWorkBuddyOAuthStore) ClientByID(clientID string) (WorkBuddyOAuthClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var client WorkBuddyOAuthClient
	var raw string
	var lastUsed sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT client_id, client_name, redirect_uris, created_at, last_used_at
		FROM workbuddy_oauth_clients WHERE client_id = $1
	`, clientID).Scan(&client.ClientID, &client.ClientName, &raw, &client.CreatedAt, &lastUsed)
	if err == sql.ErrNoRows {
		return WorkBuddyOAuthClient{}, ErrNotFound
	}
	if err != nil {
		return WorkBuddyOAuthClient{}, err
	}
	if lastUsed.Valid {
		client.LastUsedAt = &lastUsed.Time
	}
	_ = json.Unmarshal([]byte(raw), &client.RedirectURI)
	return client, nil
}

// TouchClient 更新 PostgreSQL 中客户端最近使用时间。
func (s *PostgresWorkBuddyOAuthStore) TouchClient(clientID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE workbuddy_oauth_clients SET last_used_at = now() WHERE client_id = $1`, clientID)
	return err
}

// SaveCode 保存 PostgreSQL 中的授权码。
func (s *PostgresWorkBuddyOAuthStore) SaveCode(code WorkBuddyOAuthCode) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workbuddy_oauth_codes (code, client_id, user_email, redirect_uri, scope, code_challenge, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, code.Code, code.ClientID, code.UserEmail, code.RedirectURI, code.Scope, code.CodeChallenge, code.ExpiresAt)
	return err
}

// ConsumeCode 原子取出并删除 PostgreSQL 中的授权码。
func (s *PostgresWorkBuddyOAuthStore) ConsumeCode(code string) (WorkBuddyOAuthCode, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var item WorkBuddyOAuthCode
	err := s.db.QueryRowContext(ctx, `
		DELETE FROM workbuddy_oauth_codes
		WHERE code = $1 AND expires_at > now()
		RETURNING code, client_id, user_email, redirect_uri, scope, code_challenge, expires_at
	`, code).Scan(&item.Code, &item.ClientID, &item.UserEmail, &item.RedirectURI, &item.Scope, &item.CodeChallenge, &item.ExpiresAt)
	if err == sql.ErrNoRows {
		return WorkBuddyOAuthCode{}, ErrNotFound
	}
	if err != nil {
		return WorkBuddyOAuthCode{}, err
	}
	return item, nil
}

// SaveRefreshToken 保存 PostgreSQL 中的刷新令牌。
func (s *PostgresWorkBuddyOAuthStore) SaveRefreshToken(token WorkBuddyRefreshToken) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workbuddy_oauth_refresh_tokens (token, client_id, user_email, scope, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, token.Token, token.ClientID, token.UserEmail, token.Scope, token.ExpiresAt)
	return err
}

// ConsumeRefreshToken 原子标记 PostgreSQL 中的刷新令牌已轮换并返回内容。
func (s *PostgresWorkBuddyOAuthStore) ConsumeRefreshToken(token string) (WorkBuddyRefreshToken, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var item WorkBuddyRefreshToken
	err := s.db.QueryRowContext(ctx, `
		UPDATE workbuddy_oauth_refresh_tokens SET rotated_at = now()
		WHERE token = $1 AND rotated_at IS NULL AND expires_at > now()
		RETURNING token, client_id, user_email, scope, expires_at, rotated_at
	`, token).Scan(&item.Token, &item.ClientID, &item.UserEmail, &item.Scope, &item.ExpiresAt, &item.RotatedAt)
	if err == sql.ErrNoRows {
		return WorkBuddyRefreshToken{}, ErrNotFound
	}
	if err != nil {
		return WorkBuddyRefreshToken{}, err
	}
	return item, nil
}

// workBuddyRedirectURIsAllowed 校验请求的回调地址是否在客户端注册列表内。
func workBuddyRedirectURIsAllowed(client WorkBuddyOAuthClient, redirectURI string) bool {
	uri := strings.TrimSpace(redirectURI)
	for _, item := range client.RedirectURI {
		if strings.TrimSpace(item) == uri {
			return true
		}
	}
	return false
}
