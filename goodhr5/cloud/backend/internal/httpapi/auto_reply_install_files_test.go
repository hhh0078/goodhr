// Package httpapi 本文件使用真实 PG 及私有临时卷验证附件提交、跨账号隔离与进程中断后的恢复。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRuntimeAttachmentInstallAndRecovery 校验 CAS、来源摘要、共享文件和未提交的孤立文件。
func TestRuntimeAttachmentInstallAndRecovery(t *testing.T) {
	db := autoReplyStateTestDB(t)
	var tenant, user string
	if err := db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'archive@example.test') RETURNING id", tenant).Scan(&user); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	t.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", base)
	scope := replyImportHash(tenant + "\x00" + user)
	stage := filepath.Join(base, ".runtime-stage")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	prepare := func(id, data string) (AutoReplyStateChange, string) {
		t.Helper()
		temporary, err := os.CreateTemp(stage, scope+"-")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = temporary.WriteString(data); err != nil {
			t.Fatal(err)
		}
		if err = temporary.Close(); err != nil {
			t.Fatal(err)
		}
		hash := replyImportHash(data)
		rel := replyImportHash("auto_reply:v1:"+replyImportHash(user)) + "/attachments/" + hash + ".pdf"
		raw, _ := json.Marshal(map[string]any{"id": id, "relative_path": rel, "size_bytes": len(data), "sha256": hash})
		return AutoReplyStateChange{Kind: "attachments", ID: id, Payload: raw, StagingName: filepath.Base(temporary.Name()), Revision: 1}, filepath.Join(base, scope, filepath.FromSlash(rel))
	}
	first, path := prepare("att-one", "synthetic1")
	second, otherPath := prepare("att-two", "synthetic2")
	ctx := context.Background()
	store := NewPostgresAutoReplyStore(db)
	if _, err := store.CommitAutoReplyState(ctx, tenant, user, 1, []AutoReplyStateChange{first}); !errors.Is(err, errReplyStateConflict) {
		t.Fatal("stale CAS accepted", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("过期事务已发布附件")
	}
	bad := second
	bad.StagingName = first.StagingName
	if _, err := store.CommitAutoReplyState(ctx, tenant, user, 0, []AutoReplyStateChange{first, bad}); err == nil {
		t.Fatal("第二份摘要错误仍被安装")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("整批核验前不应安装第一份")
	}
	if version, err := store.CommitAutoReplyState(ctx, tenant, user, 0, []AutoReplyStateChange{first}); err != nil || version != 1 {
		t.Fatal("PG和原件未一同确认", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "synthetic1" {
		t.Fatal("最后的原件缺失或被改写", err)
	}
	snapshot, err := store.LoadAutoReplyState(ctx, tenant, user)
	if err != nil || len(snapshot.Entities) != 1 || snapshot.Entities[0].StagingName != "" {
		t.Fatal("暂存文件名不应留在业务数据中", err)
	}
	foreign := second
	foreign.StagingName = replyImportHash("other-scope") + "-3"
	if _, err = store.CommitAutoReplyState(ctx, tenant, user, 1, []AutoReplyStateChange{foreign}); err == nil {
		t.Fatal("跨账号临时文件被接受")
	}
	// 模拟发布文件后断电，一份已被另一个引用使用，另一份没有成功提交。
	journal, err := installReplyStateAttachments(ctx, tenant, user, []AutoReplyStateChange{first, second})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-3 * time.Hour)
	for _, p := range []string{journal, filepath.Join(stage, first.StagingName), filepath.Join(stage, second.StagingName)} {
		if err = os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.RecoverAutoReplyStagedFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("恢复误删已提交的共享原件", err)
	}
	if _, err = os.Stat(otherPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("恢复遗漏断电孤儿附件", err)
	}
	if entries, err := os.ReadDir(stage); err != nil || len(entries) != 0 {
		t.Fatal("过期暂存和修复意图未清理", err)
	}
	// 第一个文件刚安装就掉电时，账号状态的首次 INSERT 也会回滚，恢复仍须清掉孤儿。
	var firstUser string
	if err = db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'first-archive@example.test') RETURNING id", tenant).Scan(&firstUser); err != nil {
		t.Fatal(err)
	}
	scope, user = replyImportHash(tenant+"\x00"+firstUser), firstUser
	fresh, freshPath := prepare("first-in-flight", "uncommitted")
	journal, err = installReplyStateAttachments(ctx, tenant, user, []AutoReplyStateChange{fresh})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(journal, old, old); err != nil {
		t.Fatal(err)
	}
	if err = store.RecoverAutoReplyStagedFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(freshPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("首条账号尚未提交时遗留原件", err)
	}

}
