// 本文件仅在显式指定的本机_test数据库创建独立schema，验证文章迁移、真实CRUD和完整HTTP发布边界。
package httpapi

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// articlePostgresTestDB 复用既有候选人测试DSN的安全约束，仅创建本轮独立schema，不加载或改写任何业务表。
func articlePostgresTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOODHR_CANDIDATE_TEST_DSN")
	if dsn == "" {
		t.Skip("未指定本机文章测试数据库")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !(parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost") || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("文章数据库集成测试仅允许回环地址的_test库")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	schema := fmt.Sprintf("articles_test_%d", time.Now().UnixNano())
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
	t.Cleanup(func() { db.Close() })
	return db
}

// TestArticlesPostgres 验证新表迁移可重复执行、中文备注完整、旧行不变，以及真实数据库与HTTP合同一致。
func TestArticlesPostgres(t *testing.T) {
	db := articlePostgresTestDB(t)
	if _, err := db.Exec(`CREATE TABLE migration_sentinel(id INTEGER PRIMARY KEY,body TEXT NOT NULL); INSERT INTO migration_sentinel VALUES(1,'保留旧行和换行' || chr(10) || '第二行');`); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0097_public_articles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatalf("article migration pass=%d: %v", pass, err)
		}
	}
	var body string
	if err = db.QueryRow(`SELECT body FROM migration_sentinel WHERE id=1`).Scan(&body); err != nil || body != "保留旧行和换行\n第二行" {
		t.Fatal("article migration changed an existing row", err)
	}
	var comments int
	if err = db.QueryRow(`SELECT count(*) FROM pg_description WHERE objoid='public_articles'::regclass AND description<>''`).Scan(&comments); err != nil || comments != 8 {
		t.Fatal("article table or column comments missing", comments, err)
	}
	store := NewPostgresArticleStore(db)
	exerciseArticleStore(t, store)
	if _, err = db.Exec(`TRUNCATE public_articles`); err != nil {
		t.Fatal(err)
	}
	// 认证及其他服务仍为合成内存，仅文章CRUD使用本轮真实PostgreSQL连接。
	server := mustNewServer(t)
	server.articles.store = store
	exerciseArticlesHTTP(t, server)
	if err = db.QueryRow(`SELECT body FROM migration_sentinel WHERE id=1`).Scan(&body); err != nil || body != "保留旧行和换行\n第二行" {
		t.Fatal("article CRUD changed unrelated data", err)
	}
}
