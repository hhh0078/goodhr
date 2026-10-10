// 本文件验证插件会话轮换和退出的原子性，包括两个独立 Redis 客户端的并发竞争。
package httpapi

import (
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// TestMemoryExtensionSessionReplacement 验证内存存储只能消费一次旧凭证并保护会话类型和目标 token。
func TestMemoryExtensionSessionReplacement(t *testing.T) {
	store := NewMemoryAuthStore()
	verifyExtensionSessionReplacement(t, []AuthStore{store})
}

// TestRedisExtensionSessionReplacement 验证真实 Redis 多客户端轮换；仅接受显式配置的本机隔离测试地址。
func TestRedisExtensionSessionReplacement(t *testing.T) {
	addr := os.Getenv("GOODHR_EXTENSION_REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("GOODHR_EXTENSION_REDIS_TEST_ADDR is not set")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		t.Fatal("Redis integration test requires an explicit loopback address")
	}
	first := NewRedisAuthStore(addr, "", 0)
	second := NewRedisAuthStore(addr, "", 0)
	t.Cleanup(func() {
		_ = first.client.Close()
		_ = second.client.Close()
	})
	verifyExtensionSessionReplacement(t, []AuthStore{first, second})
}

// verifyExtensionSessionReplacement 共用黑盒断言，验证两种存储在并发、退出、权限和冲突时遵守相同约定。
func verifyExtensionSessionReplacement(t *testing.T, stores []AuthStore) {
	t.Helper()
	store := stores[0]
	email := "session-test@example.com"
	session := Session{Email: email, Kind: extensionSessionKind, CreatedAt: time.Now()}
	newToken := func() string {
		token, err := randomToken()
		if err != nil {
			t.Fatal(err)
		}
		token = "extension-test-" + token
		t.Cleanup(func() { _ = store.SaveSession(token, Session{}, -time.Second) })
		return token
	}
	t.Run("scope-and-collision", func(t *testing.T) {
		old, collision, fresh := newToken(), newToken(), newToken()
		web := Session{Email: email}
		if err := store.SaveSession(old, web, time.Hour); err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{fresh, ""} {
			if _, err := store.ReplaceSession(old, target, time.Hour, extensionSessionKind); !errors.Is(err, ErrNotFound) {
				t.Fatalf("web session accepted as plugin: %v", err)
			}
		}
		if _, err := store.GetSession(old); err != nil {
			t.Fatalf("wrong-kind rejection consumed web token: %v", err)
		}
		if _, err := store.GetSession(fresh); !errors.Is(err, ErrNotFound) {
			t.Fatalf("wrong-kind rejection created token: %v", err)
		}
		if err := store.SaveSession(old, session, time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveSession(collision, web, time.Hour); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReplaceSession(old, collision, time.Hour, extensionSessionKind); err == nil {
			t.Fatal("existing target session overwritten")
		}
		if preserved, err := store.GetSession(old); err != nil || preserved.Kind != extensionSessionKind {
			t.Fatalf("collision consumed source session: %+v %v", preserved, err)
		}
		if preserved, err := store.GetSession(collision); err != nil || preserved.Kind != "" {
			t.Fatalf("collision overwrote target session: %+v %v", preserved, err)
		}
		if err := store.SaveSession(old, session, -time.Second); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReplaceSession(old, fresh, time.Hour, extensionSessionKind); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expired session renewed: %v", err)
		}
	})
	t.Run("concurrent-refresh", func(t *testing.T) {
		old := newToken()
		if err := store.SaveSession(old, session, time.Hour); err != nil {
			t.Fatal(err)
		}
		const requests = 64
		tokens := make([]string, requests)
		for i := range tokens {
			tokens[i] = newToken()
		}
		start := make(chan struct{})
		results := make(chan error, requests)
		var wg sync.WaitGroup
		for i, token := range tokens {
			wg.Add(1)
			go func(index int, token string) {
				defer wg.Done()
				<-start
				replacement, err := stores[index%len(stores)].ReplaceSession(old, token, time.Hour, extensionSessionKind)
				if err == nil && (replacement.Email != email || replacement.Kind != extensionSessionKind) {
					err = errors.New("replacement lost original identity or scope")
				}
				results <- err
			}(i, token)
		}
		close(start)
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else if !errors.Is(err, ErrNotFound) {
				t.Fatalf("unexpected rotation error: %v", err)
			}
		}
		if successes != 1 {
			t.Fatalf("one old token issued %d replacements, want exactly one", successes)
		}
		active := 0
		for _, token := range tokens {
			if _, err := store.GetSession(token); err == nil {
				active++
			} else if !errors.Is(err, ErrNotFound) {
				t.Fatal(err)
			}
		}
		if active != 1 {
			t.Fatalf("active replacement sessions = %d, want one", active)
		}
		if _, err := store.GetSession(old); !errors.Is(err, ErrNotFound) {
			t.Fatalf("consumed source token remains valid: %v", err)
		}
	})
	t.Run("refresh-logout-race", func(t *testing.T) {
		for attempt := 0; attempt < 32; attempt++ {
			old, fresh := newToken(), newToken()
			if err := store.SaveSession(old, session, time.Hour); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			refreshResult, logoutResult := make(chan error, 1), make(chan error, 1)
			go func() {
				<-start
				_, err := stores[0].ReplaceSession(old, fresh, time.Hour, extensionSessionKind)
				refreshResult <- err
			}()
			go func() {
				<-start
				_, err := stores[len(stores)-1].ReplaceSession(old, "", 0, extensionSessionKind)
				logoutResult <- err
			}()
			close(start)
			refreshErr, logoutErr := <-refreshResult, <-logoutResult
			if (refreshErr == nil) == (logoutErr == nil) {
				t.Fatalf("refresh and logout must have exactly one winner: refresh=%v logout=%v", refreshErr, logoutErr)
			}
			for _, err := range []error{refreshErr, logoutErr} {
				if err != nil && !errors.Is(err, ErrNotFound) {
					t.Fatal(err)
				}
			}
			_, freshErr := store.GetSession(fresh)
			if logoutErr == nil && !errors.Is(freshErr, ErrNotFound) {
				t.Fatalf("successful logout was followed by a new session: %v", freshErr)
			}
			if refreshErr == nil && freshErr != nil {
				t.Fatalf("successful refresh has no replacement: %v", freshErr)
			}
			if _, err := store.GetSession(old); !errors.Is(err, ErrNotFound) {
				t.Fatalf("race left old session active: %v", err)
			}
		}
	})
}
