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
	SaveSession(token string, session Session, ttl time.Duration) error
	GetSession(token string) (Session, error)
}

type Session struct {
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type MemoryAuthStore struct {
	mu       sync.Mutex
	codes    map[string]loginCode
	sessions map[string]Session
	now      func() time.Time
}

type loginCode struct {
	Code      string
	ExpiresAt time.Time
}

func NewMemoryAuthStore() *MemoryAuthStore {
	return &MemoryAuthStore{
		codes:    make(map[string]loginCode),
		sessions: make(map[string]Session),
		now:      time.Now,
	}
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

func (s *MemoryAuthStore) SaveSession(token string, session Session, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session.ExpiresAt = s.now().Add(ttl)
	s.sessions[token] = session
	return nil
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
