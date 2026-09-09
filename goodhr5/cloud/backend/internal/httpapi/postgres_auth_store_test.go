// 本文件验证数据库会话的重建读取、旧凭证迁移及原始有效期保持。
package httpapi

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestPostgresAuthSessionPersistence 使用专用测试库验证会话持久化和旧会话迁入。
func TestPostgresAuthSessionPersistence(t *testing.T) {
	db := openCandidatePostgresIntegrationDB(t)
	token := fmt.Sprintf("auth-test-%d", time.Now().UnixNano())
	legacyToken := token + "-legacy"
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM auth_sessions WHERE token_hash IN ($1, $2)`, authTokenHash(token), authTokenHash(legacyToken)); err != nil {
			t.Error(err)
		}
	})
	legacy := NewMemoryAuthStore()
	store := NewPostgresAuthStore(db, legacy)
	if err := store.SaveSession(token, Session{Email: "auth-test@example.com"}, sessionTTL); err != nil {
		t.Fatal(err)
	}
	first, err := store.GetSession(token)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewPostgresAuthStore(db, NewMemoryAuthStore())
	got, err := restarted.GetSession(token)
	if err != nil || !got.ExpiresAt.Equal(first.ExpiresAt) || got.Email != first.Email {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if err := legacy.SaveSession(legacyToken, Session{Email: "legacy@example.com", CreatedAt: time.Now().Add(-23 * 24 * time.Hour)}, 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	old, _ := legacy.GetSession(legacyToken)
	if _, err = store.GetSession(legacyToken); err != nil {
		t.Fatal(err)
	}
	got, err = restarted.GetSession(legacyToken)
	if err != nil || got.ExpiresAt.Sub(old.ExpiresAt).Abs() > time.Microsecond {
		t.Fatalf("旧会话到期时间发生变化：%+v err=%v", got, err)
	}
	if _, err = db.Exec(`UPDATE auth_sessions SET expires_at=$1 WHERE token_hash=$2`, time.Now().Add(-time.Second), authTokenHash(legacyToken)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetSession(legacyToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("过期数据库记录不应被旧存储复活：%v", err)
	}
}
