// Package httpapi 本文件复现已执行旧0112的数据库缺少GC登记，并验证前向升级、共享引用与最后附件回收。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplyFileGCForwardMigrationFromAppliedLegacy 验证已有0112账本不会阻止0123修复实际旧函数，重复升级安全且共享附件最后引用删除后由现有GC回收。
func TestReplyFileGCForwardMigrationFromAppliedLegacy(t *testing.T) {
	db := autoReplyStateTestDB(t)
	legacy, err := os.ReadFile(filepath.Join("testdata", "auto_reply_gc_trigger_pre_0123.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(legacy)); err != nil {
		t.Fatal(err)
	}
	if err = ensureMigrationTable(db); err != nil {
		t.Fatal(err)
	}
	if err = markMigrationApplied(db, "0112_auto_reply_cloud_state.sql"); err != nil {
		t.Fatal(err)
	}
	var definition string
	if err = db.QueryRow("SELECT pg_get_functiondef('purge_cloud_auto_reply_candidate()'::regprocedure)").Scan(&definition); err != nil || strings.Contains(definition, "'purge_files'") {
		t.Fatal("未复现旧函数缺少GC登记", err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0123_repair_auto_reply_file_gc_trigger.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	if err = markMigrationApplied(db, "0123_repair_auto_reply_file_gc_trigger.sql"); err != nil {
		t.Fatal(err)
	}
	if applied, err := migrationApplied(db, "0112_auto_reply_cloud_state.sql"); err != nil || !applied {
		t.Fatal("升级修改了历史迁移账本", err)
	}
	if err = db.QueryRow("SELECT pg_get_functiondef('purge_cloud_auto_reply_candidate()'::regprocedure)").Scan(&definition); err != nil || !strings.Contains(definition, "'purge_files'") {
		t.Fatal("前向迁移未修复实际旧函数", err)
	}
	// 重复执行不会重放旧数据或清理附件，后续仍复用原来的GC事务。
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	var tenant, user, a, b string
	if err = db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'gc-upgrade@example.test') RETURNING id", tenant).Scan(&user); err != nil {
		t.Fatal(err)
	}
	for _, id := range []*string{&a, &b} {
		if err = db.QueryRow("INSERT INTO candidate_profiles(tenant_id,candidate_name,source_platform_id,source_platform_candidate_id) VALUES($1,'共享附件合成候选人','test',gen_random_uuid()::text) RETURNING id", tenant).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	base := t.TempDir()
	t.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", base)
	content := []byte("只用于旧GC升级回归的共享附件")
	digest := replyImportHash(string(content))
	relative := replyImportHash("auto_reply:v1:"+replyImportHash(user)) + "/attachments/" + digest + ".pdf"
	path := filepath.Join(base, replyImportHash(tenant+"\x00"+user), filepath.FromSlash(relative))
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	// 两份候选人引用相同受管文件，服务器状态提交自动生成真实父子关联。
	changes := []AutoReplyStateChange{}
	for index, id := range []string{a, b} {
		local := []string{"local-a", "local-b"}[index]
		for _, item := range []struct {
			kind, id string
			payload  map[string]any
		}{
			{"candidates", local, map[string]any{}},
			{"cloud_links", "candidate:" + local, map[string]any{"value": id}},
			{"attachments", "file-" + local, map[string]any{"candidate_id": local, "relative_path": relative, "sha256": digest, "size_bytes": len(content)}},
		} {
			raw, _ := json.Marshal(item.payload)
			changes = append(changes, AutoReplyStateChange{Kind: item.kind, ID: item.id, Payload: raw})
		}
	}
	store := NewPostgresAutoReplyStore(db)
	ctx := context.Background()
	if _, err = store.CommitAutoReplyState(ctx, tenant, user, 0, changes); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DELETE FROM candidate_profiles WHERE id=$1", a); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_cloud_entities WHERE kind='purge_files'").Scan(&pending); err != nil || pending != 1 {
		t.Fatal("删除未持久登记附件GC", pending, err)
	}
	if did, err := advanceReplyFilePurge(ctx, db, base); err != nil || !did {
		t.Fatal("共享附件待办未处理", did, err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(content) {
		t.Fatal("仍有引用的附件被误删", err)
	}
	if _, err = db.Exec("DELETE FROM candidate_profiles WHERE id=$1", b); err != nil {
		t.Fatal(err)
	}
	if did, err := advanceReplyFilePurge(ctx, db, base); err != nil || !did {
		t.Fatal("最后引用的附件未回收", did, err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("最后引用删除后附件仍保留", err)
	}
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_cloud_entities WHERE kind IN ('attachments','purge_files')").Scan(&pending); err != nil || pending != 0 {
		t.Fatal("附件及GC待办没有归零", pending, err)
	}
}
