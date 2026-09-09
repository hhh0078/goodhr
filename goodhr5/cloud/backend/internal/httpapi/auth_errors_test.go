// 本文件验证临时认证故障不会被误报为过期，以及登录会话固定保持三十天。
package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// failingSessionStore 模拟可恢复的会话存储故障。
type failingSessionStore struct {
	AuthStore
	err error
}

// GetSession 在故障解除后继续读取原凭证。
func (s *failingSessionStore) GetSession(token string) (Session, error) {
	if s.err != nil {
		return Session{}, s.err
	}
	return s.AuthStore.GetSession(token)
}

// TestAuthTemporaryFailureRetainsSession 验证认证临时故障与真正失效使用不同状态码。
func TestAuthTemporaryFailureRetainsSession(t *testing.T) {
	store := &failingSessionStore{AuthStore: NewMemoryAuthStore()}
	if err := store.SaveSession("valid-token", Session{Email: "test@example.com"}, sessionTTL); err != nil {
		t.Fatal(err)
	}
	auth := NewAuthService(store, DevMailer{}, true, nil, nil, nil, nil, &recordingUserActivityStore{}, nil, nil, 0)
	for _, tc := range []struct {
		name, token, code string
		err               error
		status            int
	}{
		{"temporary", "valid-token", "AUTH_UNAVAILABLE", errors.New("private-storage-details"), 503},
		{"recovered", "valid-token", "", nil, 200},
		{"missing", "", "SESSION_REQUIRED", nil, 401},
		{"expired", "unknown-token", "SESSION_EXPIRED", nil, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store.err = tc.err
			req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp := httptest.NewRecorder()
			auth.Me(resp, req)
			if resp.Code != tc.status || !strings.Contains(resp.Body.String(), tc.code) {
				t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
			}
			if strings.Contains(resp.Body.String(), "private-storage-details") {
				t.Fatal("泄露存储内部错误")
			}
		})
	}
}

// TestSessionFixedThirtyDays 验证访问不续期，第二十九天有效，三十天后到期。
func TestSessionFixedThirtyDays(t *testing.T) {
	if sessionTTL != 30*24*time.Hour {
		t.Fatalf("TTL=%v", sessionTTL)
	}
	store := NewMemoryAuthStore()
	start := time.Now()
	clock := start
	store.now = func() time.Time { return clock }
	if err := store.SaveSession("token", Session{Email: "test@example.com", CreatedAt: start}, sessionTTL); err != nil {
		t.Fatal(err)
	}
	clock = start.Add(29 * 24 * time.Hour)
	session, err := store.GetSession("token")
	if err != nil || !session.ExpiresAt.Equal(start.Add(sessionTTL)) {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	clock = start.Add(sessionTTL + time.Second)
	if _, err := store.GetSession("token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}
