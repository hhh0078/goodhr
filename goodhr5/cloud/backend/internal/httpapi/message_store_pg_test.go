// 文件作用：在明确授权的本机独立测试schema验证消息迁移、PostgreSQL并发事务、72小时删除与账号设置持久化。
package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// messagePostgresTestDB 只允许回环地址_test库，并为每次测试创建独立schema防止碰到现有业务数据。
func messagePostgresTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOODHR_MESSAGE_TEST_DSN")
	if dsn == "" {
		t.Skip("GOODHR_MESSAGE_TEST_DSN is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("message database test requires loopback _test database")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	schema := fmt.Sprintf("message_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	query := parsed.Query()
	query.Set("options", "-csearch_path="+schema+",public")
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(10)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto; CREATE TABLE users(id UUID PRIMARY KEY DEFAULT gen_random_uuid(),email TEXT UNIQUE NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0087_user_messages.sql", "0088_resume_library_settings.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err = db.Exec(string(raw)); err != nil {
				t.Fatalf("migration %s: %v", name, err)
			}
		}
	}
	var missing int
	if err = db.QueryRow(`SELECT count(*) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname IN ('messages','user_message_state','user_message_receipts','resume_library_settings') AND a.attnum>0 AND NOT a.attisdropped AND col_description(c.oid,a.attnum) IS NULL`, schema).Scan(&missing); err != nil || missing != 0 {
		t.Fatal("columns lack Chinese comments", missing, err)
	}
	return db
}

// TestPostgresMessageConcurrentCleanupReadPush 验证多个清理任务与过期正文展开及新消息推送并发时不死锁、不重复计数。
func TestPostgresMessageConcurrentCleanupReadPush(t *testing.T) {
	db := messagePostgresTestDB(t)
	store := NewPostgresMessageStore(db)
	old, err := store.Push("owner@example.test", messageFixture("concurrent-cleanup-old"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.Read("owner@example.test", old.Message.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err = db.Exec(`UPDATE messages SET read_at=$1 WHERE id=$2::uuid`, now.Add(-73*time.Hour), old.Message.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 48)
	start := make(chan struct{})
	for index := 0; index < 48; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			var operationErr error
			switch index % 3 {
			case 0:
				_, operationErr = store.CleanupRead(now.Add(-72 * time.Hour))
			case 1:
				_, _, operationErr = store.Read("owner@example.test", old.Message.ID)
				if errors.Is(operationErr, ErrNotFound) {
					operationErr = nil
				}
			case 2:
				_, operationErr = store.Push("owner@example.test", messageFixture("concurrent-cleanup-new"))
			}
			if operationErr != nil {
				failures <- operationErr
			}
		}(index)
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	summary, err := store.Summary("owner@example.test")
	if err != nil || summary != (MessageSummary{1, 4}) {
		t.Fatal(summary, err)
	}
	list, err := store.List("owner@example.test", 1, 20)
	if err != nil || list.Total != 1 {
		t.Fatal(list, err)
	}
}

// TestPostgresMessageIdempotencyIsolation 验证真实SQL同账号并发推送与首次展开保持计数、幂等和权限。
func TestPostgresMessageIdempotencyIsolation(t *testing.T) {
	db := messagePostgresTestDB(t)
	store := NewPostgresMessageStore(db)
	if _, err := db.Exec(`INSERT INTO users(email) VALUES('owner@example.test'),('other@example.test')`); err != nil {
		t.Fatal(err)
	}
	verifyMessageIdempotency(t, store)
	list, err := store.List("owner@example.test", 1, 20)
	if err != nil || list.Total != 1 || len(list.Messages) != 1 || list.Messages[0].Body != "" {
		t.Fatal(list, err)
	}
	list, err = store.List("owner@example.test", 10, 20)
	if err != nil || list.Total != 1 || len(list.Messages) != 0 {
		t.Fatal(list, err)
	}
	list, err = store.List("missing@example.test", 1, 20)
	if err != nil || list.Total != 0 || len(list.Messages) != 0 {
		t.Fatal(list, err)
	}
}

// TestPostgresMessageCleanupAndSettings 验证已读满72小时删除、未读保留、幂等凭据继续有效和设置数据库隔离。
func TestPostgresMessageCleanupAndSettings(t *testing.T) {
	db := messagePostgresTestDB(t)
	store := NewPostgresMessageStore(db)
	input := messageFixture("cleanup-original")
	old, err := store.Push("owner@example.test", input)
	if err != nil {
		t.Fatal(err)
	}
	unread, err := store.Push("owner@example.test", messageFixture("unread-long-lived"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.Read("owner@example.test", old.Message.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	readAt := now.Add(-72 * time.Hour)
	if _, err = db.Exec(`UPDATE messages SET read_at=$1 WHERE id=$2::uuid`, readAt, old.Message.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE messages SET created_at=$1 WHERE id=$2::uuid`, now.Add(-365*24*time.Hour), unread.Message.ID); err != nil {
		t.Fatal(err)
	}
	changed, err := store.CleanupRead(readAt.Add(-time.Microsecond))
	if err != nil || len(changed) != 0 {
		t.Fatal(changed, err)
	}
	changed, err = store.CleanupRead(readAt)
	if err != nil || len(changed) != 1 || changed[0].Summary != (MessageSummary{1, 4}) {
		t.Fatal(changed, err)
	}
	if _, _, err = store.Read("owner@example.test", old.Message.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	list, err := store.List("owner@example.test", 1, 20)
	if err != nil || list.Total != 1 || list.Messages[0].ID != unread.Message.ID {
		t.Fatal(list, err)
	}
	repeated, err := store.Push("owner@example.test", input)
	if err != nil || repeated.Created || repeated.Message.ID != old.Message.ID {
		t.Fatal(repeated, err)
	}
	summary, err := store.Summary("owner@example.test")
	if err != nil || summary != (MessageSummary{1, 4}) {
		t.Fatal(summary, err)
	}
	settings := NewPostgresResumeLibrarySettingsStore(db)
	defaults, err := settings.Get("owner@example.test")
	if err != nil || !defaults.AutoImportDownloads || defaults.UpdatedAt != nil {
		t.Fatal(defaults, err)
	}
	saved, err := settings.Save("owner@example.test", false)
	if err != nil || saved.AutoImportDownloads || saved.UpdatedAt == nil {
		t.Fatal(saved, err)
	}
	loaded, err := NewPostgresResumeLibrarySettingsStore(db).Get("owner@example.test")
	if err != nil || loaded.AutoImportDownloads || !loaded.UpdatedAt.Equal(*saved.UpdatedAt) {
		t.Fatal(loaded, err)
	}
	other, err := settings.Get("other@example.test")
	if err != nil || !other.AutoImportDownloads {
		t.Fatal(other, err)
	}
}
