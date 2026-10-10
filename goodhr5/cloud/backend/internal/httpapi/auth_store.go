package httpapi

import (
	"errors"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

type AuthStore interface {
	SaveLoginCode(email string, code string, ttl time.Duration) error
	ConsumeLoginCode(email string, code string) (bool, error)
	SavePasswordResetCode(email string, code string, ttl time.Duration) error
	ConsumePasswordResetCode(email string, code string) (bool, error)
	SaveSession(token string, session Session, ttl time.Duration) error
	// ReplaceSession 原子校验并消费旧会话；新 token 为空时仅注销，失败时不签发新会话。
	ReplaceSession(oldToken string, newToken string, ttl time.Duration, expectedKind string) (Session, error)
	GetSession(token string) (Session, error)
	GetSessionUnsafe(token string) (Session, error)
	PasswordLoginAllowed(email string) (bool, error)
	RecordPasswordLoginFailure(email string) error
	ClearPasswordLoginFailures(email string) error
}

type Session struct {
	Kind      string
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type MemoryAuthStore struct {
	mu               sync.Mutex
	codes            map[string]loginCode
	resetCodes       map[string]loginCode
	sessions         map[string]Session
	passwordFailures map[string]passwordLoginFailures
	now              func() time.Time
}

type passwordLoginFailures struct {
	Attempts  int
	ExpiresAt time.Time
}

type loginCode struct {
	Code      string
	ExpiresAt time.Time
}

func NewMemoryAuthStore() *MemoryAuthStore {
	return &MemoryAuthStore{
		codes:            make(map[string]loginCode),
		resetCodes:       make(map[string]loginCode),
		sessions:         make(map[string]Session),
		passwordFailures: make(map[string]passwordLoginFailures),
		now:              time.Now,
	}
}

// PasswordLoginAllowed 判断账号是否已因连续输错密码而进入短暂等待期。
func (s *MemoryAuthStore) PasswordLoginAllowed(email string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	failures, ok := s.passwordFailures[email]
	if !ok || s.now().After(failures.ExpiresAt) {
		delete(s.passwordFailures, email)
		return true, nil
	}
	return failures.Attempts < maxPasswordLoginAttempts, nil
}

// RecordPasswordLoginFailure 记录一次密码错误，并刷新等待期。
func (s *MemoryAuthStore) RecordPasswordLoginFailure(email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	failures := s.passwordFailures[email]
	if s.now().After(failures.ExpiresAt) {
		failures.Attempts = 0
	}
	failures.Attempts++
	failures.ExpiresAt = s.now().Add(passwordFailureTTL)
	s.passwordFailures[email] = failures
	return nil
}

// ClearPasswordLoginFailures 在密码登录成功后清除账号的错误计数。
func (s *MemoryAuthStore) ClearPasswordLoginFailures(email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.passwordFailures, email)
	return nil
}

func (s *MemoryAuthStore) SaveLoginCode(email string, code string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.codes[email] = loginCode{
		Code:      code,
		ExpiresAt: s.now().Add(ttl),
	}
	return nil
}

func (s *MemoryAuthStore) ConsumeLoginCode(email string, code string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	saved, ok := s.codes[email]
	if !ok {
		return false, nil
	}
	if s.now().After(saved.ExpiresAt) {
		delete(s.codes, email)
		return false, nil
	}
	if saved.Code != code {
		return false, nil
	}

	delete(s.codes, email)
	return true, nil
}

// SavePasswordResetCode 保存一个只能用于重置密码的验证码。
func (s *MemoryAuthStore) SavePasswordResetCode(email string, code string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.resetCodes[email] = loginCode{
		Code:      code,
		ExpiresAt: s.now().Add(ttl),
	}
	return nil
}

// ConsumePasswordResetCode 校验并消费一个重置密码验证码，验证成功后立即失效。
func (s *MemoryAuthStore) ConsumePasswordResetCode(email string, code string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	saved, ok := s.resetCodes[email]
	if !ok {
		return false, nil
	}
	if s.now().After(saved.ExpiresAt) {
		delete(s.resetCodes, email)
		return false, nil
	}
	if saved.Code != code {
		return false, nil
	}

	delete(s.resetCodes, email)
	return true, nil
}

func (s *MemoryAuthStore) SaveSession(token string, session Session, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ttl <= 0 {
		delete(s.sessions, token)
		return nil
	}
	session.ExpiresAt = s.now().Add(ttl)
	s.sessions[token] = session
	return nil
}

// ReplaceSession 在同一把锁内验证旧会话并轮换或注销，保证并发请求只有一个能消费旧 token。
func (s *MemoryAuthStore) ReplaceSession(oldToken string, newToken string, ttl time.Duration, expectedKind string) (Session, error) {
	if oldToken == "" || (newToken != "" && (newToken == oldToken || ttl <= 0)) {
		return Session{}, errors.New("invalid session replacement")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	session, ok := s.sessions[oldToken]
	if !ok || !now.Before(session.ExpiresAt) {
		delete(s.sessions, oldToken)
		return Session{}, ErrNotFound
	}
	if session.Kind != expectedKind {
		return Session{}, ErrNotFound
	}
	if newToken != "" {
		if existing, exists := s.sessions[newToken]; exists && now.Before(existing.ExpiresAt) {
			return Session{}, errors.New("replacement token already exists")
		}
		session.CreatedAt = now
		session.ExpiresAt = now.Add(ttl)
		s.sessions[newToken] = session
	}
	delete(s.sessions, oldToken)
	return session, nil
}

func (s *MemoryAuthStore) GetSession(token string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[token]
	if !ok {
		return Session{}, ErrNotFound
	}
	if s.now().After(session.ExpiresAt) {
		delete(s.sessions, token)
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (s *MemoryAuthStore) GetSessionUnsafe(token string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[token]
	if !ok {
		return Session{}, ErrNotFound
	}
	if s.now().After(session.ExpiresAt) {
		delete(s.sessions, token)
		return Session{}, ErrNotFound
	}
	return session, nil
}
