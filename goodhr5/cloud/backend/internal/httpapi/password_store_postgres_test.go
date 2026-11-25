// 本文件在显式指定的隔离 PostgreSQL 测试库中验证仅注册的并发唯一性与老账号保护。
package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPostgresCreateAccount 验证真实 PostgreSQL 拒绝重复和老账号，并发只允许一次创建且不覆盖密码。
func TestPostgresCreateAccount(t *testing.T) {
	dsn := os.Getenv("GOODHR_AUTH_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("GOODHR_AUTH_TEST_PG_DSN is not configured")
	}
	base, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	var database string
	if err := base.QueryRow(`SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(database, "goodhr_auth_test_") {
		t.Fatal("仅允许使用以 goodhr_auth_test_ 开头的独立测试库")
	}
	schema := fmt.Sprintf("registration_%d", time.Now().UnixNano())
	if _, err := base.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error(err)
		}
	}()
	if _, err := base.Exec(`CREATE TABLE ` + schema + `.users (email TEXT PRIMARY KEY, password_hash TEXT);
		COMMENT ON TABLE ` + schema + `.users IS '仅注册接口隔离测试用户';
		COMMENT ON COLUMN ` + schema + `.users.email IS '测试账号邮箱';
		COMMENT ON COLUMN ` + schema + `.users.password_hash IS '测试密码摘要';`); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	} else {
		dsn += " search_path=" + schema
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewPostgresPasswordStore(db)
	if err := store.CreateAccount("new@example.test", "first-hash"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAccount("new@example.test", "replacement-hash"); !errors.Is(err, ErrAccountAlreadyExists) {
		t.Fatalf("duplicate create err=%v", err)
	}
	if hash, err := store.PasswordHash("new@example.test"); err != nil || hash != "first-hash" {
		t.Fatal("duplicate registration modified existing password")
	}
	if _, err := db.Exec(`INSERT INTO users (email) VALUES ('legacy@example.test')`); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAccount("legacy@example.test", "unwanted-hash"); !errors.Is(err, ErrAccountAlreadyExists) {
		t.Fatalf("legacy create err=%v", err)
	}
	if exists, set, err := store.AccountStatus("legacy@example.test"); err != nil || !exists || set {
		t.Fatal("registration modified legacy password")
	}
	var group sync.WaitGroup
	winners := make(chan string, 16)
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			hash := fmt.Sprintf("synthetic-hash-%d", i)
			err := store.CreateAccount("concurrent@example.test", hash)
			if err == nil {
				winners <- hash
			} else if !errors.Is(err, ErrAccountAlreadyExists) {
				t.Errorf("concurrent create err=%v", err)
			}
		}(i)
	}
	group.Wait()
	close(winners)
	if len(winners) != 1 {
		t.Fatalf("successful registrations=%d want=1", len(winners))
	}
	if hash, err := store.PasswordHash("concurrent@example.test"); err != nil || hash != <-winners {
		t.Fatal("concurrent losing request replaced the winner password")
	}
	if err := store.SavePasswordHash("legacy@example.test", "legacy-setup-hash"); err != nil {
		t.Fatalf("original legacy password setup behavior changed: %v", err)
	}
}
