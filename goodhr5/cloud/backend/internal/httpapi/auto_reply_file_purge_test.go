// 本文件用实际 PostgreSQL 事务和私有卷核验自动回复停止、并发导入与共享附件的物理清理。
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

// TestReplyFilePurgeSharesImportOwnerLock 验证导入占锁时不会误删、最后一个引用删除后无需启动任何岗位即可清理附件。
func TestReplyFilePurgeSharesImportOwnerLock(t *testing.T) {
	db := autoReplyStateTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewPostgresAutoReplyStore(db)
	var tenant, user string
	if err := db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'files@example.test') RETURNING id", tenant).Scan(&user); err != nil {
		t.Fatal(err)
	}
	var a, b string
	for _, id := range []*string{&a, &b} {
		if err := db.QueryRow("INSERT INTO candidate_profiles(tenant_id,candidate_name,source_platform_id,source_platform_candidate_id) VALUES($1,'测试','test',gen_random_uuid()::text) RETURNING id", tenant).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	data := "候选人共享附件：测试内容"
	base := t.TempDir()
	t.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", base)
	scopeRoot := filepath.Join(base, replyImportHash(tenant+"\x00"+user))
	digest := replyImportHash(data)
	relative := replyImportHash("auto_reply:v1:"+replyImportHash(user)) + "/attachments/" + digest + ".pdf"
	file := replyImportFile{RelativePath: relative, SHA256: digest, SizeBytes: int64(len(data))}
	dest := filepath.Join(scopeRoot, filepath.FromSlash(relative))
	stage := t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, filepath.Base(dest)), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	held, release := make(chan struct{}), make(chan struct{})
	importFile := func(candidate string, wait bool) error {
		change := func(kind, id string, fields map[string]string) AutoReplyStateChange {
			payload, _ := json.Marshal(fields)
			return AutoReplyStateChange{Kind: kind, ID: id, Payload: payload}
		}
		local := "local-" + candidate
		changes := []AutoReplyStateChange{change("candidates", local, map[string]string{}), change("cloud_links", "candidate:"+local, map[string]string{"value": candidate}), change("attachments", "file-"+local, map[string]string{"candidate_id": local, "sha256": digest, "relative_path": relative})}
		_, err := s.ImportAutoReplyState(ctx, tenant, user, replyImportHash(candidate), changes, 1, func(live map[string]bool) error {
			if wait {
				close(held)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return installReplyImportFiles(ctx, stage, scopeRoot, []replyImportFile{file}, live)
		})
		return err
	}
	if err := importFile(a, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM candidate_profiles WHERE id=$1", a); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- importFile(b, true) }()
	select {
	case <-held:
	case err := <-result:
		t.Fatalf("第二份导入未取得成员锁：%v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("第二份导入超时")
	}
	if did, err := advanceReplyFilePurge(ctx, db, base); err != nil || did {
		t.Fatal("并发导入期间清理器不应领取该成员", did, err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if did, err := advanceReplyFilePurge(ctx, db, base); !did || err != nil {
		t.Fatal("处理共享附件待办", did, err)
	}
	if bytes, err := os.ReadFile(dest); err != nil || string(bytes) != data {
		t.Fatal("仍有候选人引用的文件丢失", err)
	}
	if _, err := db.Exec("DELETE FROM candidate_profiles WHERE id=$1", b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("替换过的内容"), 0600); err != nil {
		t.Fatal(err)
	}
	if did, err := advanceReplyFilePurge(ctx, db, base); !did || err == nil {
		t.Fatal("内容不符时没有保留待办", did, err)
	}
	if did, err := advanceReplyFilePurge(ctx, db, base); did || err != nil {
		t.Fatal("内容异常的待办不应立即占用下一轮", did, err)
	}
	if err := os.WriteFile(dest, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE auto_reply_cloud_entities SET updated_at='' WHERE kind='purge_files' AND processed_status='retry'"); err != nil {
		t.Fatal(err)
	}
	startReplyFilePurge(ctx, db, base)
	deadline := time.Now().Add(4 * time.Second)
	for {
		var remaining int
		if err := db.QueryRow("SELECT count(*) FROM auto_reply_cloud_entities WHERE kind='purge_files'").Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(dest); remaining == 0 && errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("岗位停止后，最后一份附件未被后台清理")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRemoveReplyFileGuards 验证路径逃逸、替换内容与软链接不会导致不属于该成员的文件被删除。
func TestRemoveReplyFileGuards(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	user, tenant := "u1", "t1"
	data, bad := "真实附件", "替换附件"
	hash := replyImportHash(data)
	scope := filepath.Join(base, replyImportHash(tenant+"\x00"+user))
	owner := replyImportHash("auto_reply:v1:" + replyImportHash(user))
	relative := owner + "/attachments/" + hash + ".pdf"
	dest := filepath.Join(scope, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	if removeReplyFile(base, tenant, user, relative, hash) == nil || removeReplyFile(base, tenant, "other", relative, hash) == nil || removeReplyFile(base, tenant, user, "../"+relative, hash) == nil {
		t.Fatal("未拒绝内容替换或路径越界")
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != bad {
		t.Fatal("替换文件被误删", err)
	}
	if err := os.RemoveAll(filepath.Dir(dest)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Dir(dest)); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, filepath.Base(dest))
	if err := os.WriteFile(target, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeReplyFile(base, tenant, user, relative, hash); err == nil {
		t.Fatal("没有拒绝指向其他目录的链接")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("链接外的附件被删除", err)
	}
}

// TestFailedReplyImportRecoversInstalledFiles 验证新成员首次迁移部分安装后失败，事务回滚后的卷内文件也可恢复清理。
func TestFailedReplyImportRecoversInstalledFiles(t *testing.T) {
	db := autoReplyStateTestDB(t)
	s := NewPostgresAutoReplyStore(db)
	ctx := context.Background()
	var tenant, user string
	if err := db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'first-import@example.test') RETURNING id", tenant).Scan(&user); err != nil {
		t.Fatal(err)
	}
	base, stage := t.TempDir(), t.TempDir()
	t.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", base)
	scope := filepath.Join(base, replyImportHash(tenant+"\x00"+user))
	owner := replyImportHash("auto_reply:v1:" + replyImportHash(user))
	var entities []AutoReplyStateChange
	var files []replyImportFile
	for i, content := range []string{"第一份字节", "第二份字节"} {
		digest := replyImportHash(content)
		relative := owner + "/attachments/" + digest + ".pdf"
		f := replyImportFile{RelativePath: relative, SHA256: digest, SizeBytes: int64(len(content))}
		raw, _ := json.Marshal(f)
		entities = append(entities, AutoReplyStateChange{Kind: "attachments", ID: digest, Payload: raw})
		files = append(files, f)
		if i == 0 {
			if err := os.WriteFile(filepath.Join(stage, digest+".pdf"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	var journals []string
	_, err := s.ImportAutoReplyState(ctx, tenant, user, replyImportHash("这个新成员的归档"), entities, len(files), func(live map[string]bool) error {
		var err error
		if journals, err = journalReplyImportFiles(base, tenant, user, files, live); err != nil {
			return err
		}
		return installReplyImportFiles(ctx, stage, scope, files, live)
	})
	if err == nil || len(journals) != 1 {
		t.Fatalf("预期中途失败并保留安装意图：%v %d", err, len(journals))
	}
	orphan := filepath.Join(scope, filepath.FromSlash(files[0].RelativePath))
	if _, err = os.Stat(orphan); err != nil {
		t.Fatal("未复现第一份文件已经安装", err)
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_state_owners WHERE tenant_id=$1 AND user_id=$2", tenant, user).Scan(&n); err != nil || n != 0 {
		t.Fatal("首次失败的导入应已回滚成员行", err, n)
	}
	old := time.Now().Add(-3 * time.Hour)
	if err = os.Chtimes(journals[0], old, old); err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverAutoReplyStagedFiles(ctx); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{orphan, journals[0]} {
		if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("首次导入中断后仍有孤立文件", err)
		}
	}
}
