// Package httpapi 本文件用隔离 PG 验证旧会话审计迁移、权限边界、幂等和后来一次真实删除事务触发的清理。
package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSealedAuditMigrationAndDeletionTrigger 只允许清理同成员已封印又已消失的会话，空关联、其他成员及仍在用的会话必须保留。
func TestSealedAuditMigrationAndDeletionTrigger(t *testing.T) {
	const migration = "0118_auto_reply_sealed_audits.sql"
	db := autoReplyStateTestDB(t, migration)
	var tenant, owner, other string
	if err := db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	for i, p := range []*string{&owner, &other} {
		if err := db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,$2) RETURNING id", tenant, []string{"owner@fixture.test", "other@fixture.test"}[i]).Scan(p); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2)", tenant, *p); err != nil {
			t.Fatal(err)
		}
	}
	add := func(user, kind, id, key, value string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{key: value})
		_, err := db.Exec(`INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents) VALUES($1,$2,$3,$4,$5,1,1,'','{}')`, tenant, user, kind, id, raw)
		if err != nil {
			t.Fatal(err)
		}
	}
	version := func(user string) int {
		t.Helper()
		var n int
		if err := db.QueryRow("SELECT version FROM auto_reply_state_owners WHERE tenant_id=$1 AND user_id=$2", tenant, user).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	count := func(user string) int {
		t.Helper()
		var n int
		if err := db.QueryRow("SELECT count(*) FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind IN ('runtime_commands','runtime_events')", tenant, user).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	add(owner, "purge_keys", "seal-a", "entity_key", "conversations:a")
	add(owner, "purge_keys", "seal-live", "entity_key", "conversations:live")
	add(owner, "conversations", "live", "id", "live")
	add(owner, "runtime_events", "a", "conversation_id", "a")
	add(owner, "runtime_commands", "b", "conversation_id", "a")
	add(owner, "runtime_events", "keep", "conversation_id", "live")
	add(owner, "runtime_events", "global", "conversation_id", "")
	add(owner, "runtime_commands", "unsealed", "conversation_id", "other")
	add(other, "runtime_events", "same", "conversation_id", "a")
	body, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", migration))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	if count(owner) != 3 || count(other) != 1 || version(owner) != 1 || version(other) != 0 {
		t.Fatal("迁移清理范围异常")
	}
	var deleted int
	if err = db.QueryRow("SELECT purge_sealed_auto_reply_audits($1,$2)", tenant, owner).Scan(&deleted); err != nil || deleted != 0 || version(owner) != 1 {
		t.Fatal("重复核对改变了版本", err)
	}
	add(owner, "runtime_commands", "late", "conversation_id", "a")
	_, err = db.Exec(`INSERT INTO candidate_deletions(tenant_id,candidate_id,row_version,owner_hash,audiences,source_hashes,contact_hashes) VALUES($1,gen_random_uuid(),1,$2,jsonb_build_array($2::text),'[]','[]')`, tenant, replyImportHash("owner@fixture.test"))
	if err != nil {
		t.Fatal(err)
	}
	if count(owner) != 3 || count(other) != 1 || version(owner) <= 1 {
		t.Fatal("后续真实删除没有清理同一成员积欠的审计")
	}
}
