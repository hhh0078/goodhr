package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisAuthStore struct {
	client *redis.Client
}

func NewRedisAuthStore(addr string, password string, db int) *RedisAuthStore {
	return &RedisAuthStore{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
	}
}

// Ping 检查 Redis 客户端是否能正常连接。
func (s *RedisAuthStore) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.client.Ping(pingCtx).Err(); err != nil {
		return fmt.Errorf("Redis 连接失败: %w", err)
	}
	return nil
}

func (s *RedisAuthStore) SaveLoginCode(email string, code string, ttl time.Duration) error {
	return s.client.Set(context.Background(), loginCodeKey(email), code, ttl).Err()
}

func (s *RedisAuthStore) ConsumeLoginCode(email string, code string) (bool, error) {
	ctx := context.Background()
	key := loginCodeKey(email)
	saved, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if saved != code {
		return false, nil
	}
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *RedisAuthStore) SaveSession(token string, session Session, ttl time.Duration) error {
	ctx := context.Background()
	session.ExpiresAt = time.Now().Add(ttl)
	body, err := json.Marshal(session)
	if err != nil {
		return err
	}
	if err := s.client.Set(ctx, sessionKey(token), body, ttl).Err(); err != nil {
		return err
	}
	return nil
}

func (s *RedisAuthStore) GetSession(token string) (Session, error) {
	ctx := context.Background()
	body, err := s.client.Get(ctx, sessionKey(token)).Bytes()
	if err == redis.Nil {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}

	var session Session
	if err := json.Unmarshal(body, &session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *RedisAuthStore) GetSessionUnsafe(token string) (Session, error) {
	body, err := s.client.Get(context.Background(), sessionKey(token)).Bytes()
	if err == redis.Nil {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(body, &session); err != nil {
		return Session{}, err
	}
	return session, nil
}

// PasswordLoginAllowed 从 Redis 读取密码错误次数，限制连续密码尝试。
func (s *RedisAuthStore) PasswordLoginAllowed(email string) (bool, error) {
	count, err := s.client.Get(context.Background(), passwordFailuresKey(email)).Int()
	if err == redis.Nil {
		return true, nil
	}
	return count < maxPasswordLoginAttempts, err
}

// RecordPasswordLoginFailure 在 Redis 原子递增密码错误次数，并刷新等待期。
func (s *RedisAuthStore) RecordPasswordLoginFailure(email string) error {
	ctx := context.Background()
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Incr(ctx, passwordFailuresKey(email))
		pipe.Expire(ctx, passwordFailuresKey(email), passwordFailureTTL)
		return nil
	})
	return err
}

// ClearPasswordLoginFailures 在密码登录成功后删除 Redis 中的错误计数。
func (s *RedisAuthStore) ClearPasswordLoginFailures(email string) error {
	return s.client.Del(context.Background(), passwordFailuresKey(email)).Err()
}

// passwordFailuresKey 返回账号密码错误计数的 Redis 键。
func passwordFailuresKey(email string) string {
	return "password_login_failures:" + email
}

func loginCodeKey(email string) string {
	return "login_code:" + email
}

func sessionKey(token string) string {
	return "session:" + token
}

func currentSessionKey(email string) string {
	return "session_current:" + email
}
