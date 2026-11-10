// 本文件在独立PostgreSQL与临时附件目录核验在线临时清理的来源边界、多成员保护及删除回执和文件回收。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// retentionActorCase 描述同一平台来源在某位成员下已保存的真实服务器资料。
type retentionActorCase struct {
	User, Source, Canonical, Phone string
	ProfileAt, MessageAt           time.Time
	Online, Attachment             bool
	FilesBase                      string
}

// saveRetentionActor 建立带精确原生身份、聊天及可选私有附件的成员状态，不使用真实候选人数据。
func saveRetentionActor(t *testing.T, db *sql.DB, tenant string, c retentionActorCase) string {
	t.Helper()
	local := "local-" + c.Source
	profile := map[string]any{"ID": local, "Phone": c.Phone}
	if c.Online {
		profile["online_profile"] = map[string]any{"candidate_id": c.Canonical, "updated_at": c.ProfileAt, "profile_key": "synthetic-online"}
	}
	changes := []AutoReplyStateChange{
		retentionStateChange(t, "candidates", local, profile, c.ProfileAt),
		retentionStateChange(t, "identities", "identity-"+c.Source, map[string]any{"candidate_id": local, "platform_id": "boss", "platform_candidate_id": c.Source}, c.ProfileAt),
		retentionStateChange(t, "conversations", "thread-"+c.Source, map[string]any{"candidate_id": local, "platform_identity_id": "identity-" + c.Source, "platform_id": "boss", "platform_account_id": "synthetic-account", "platform_thread_id": c.Source}, c.ProfileAt),
		retentionStateChange(t, "messages", "message-"+c.Source, map[string]any{"conversation_id": "thread-" + c.Source, "direction": "candidate", "platform_sent_at": c.MessageAt}, c.MessageAt),
	}
	var filePath string
	if c.Attachment {
		data := []byte("多成员临时简历附件合成内容")
		digest := replyImportHash(string(data))
		relative := replyImportHash("auto_reply:v1:"+replyImportHash(c.User)) + "/attachments/" + digest + ".pdf"
		filePath = filepath.Join(c.FilesBase, replyImportHash(tenant+"\x00"+c.User), filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filePath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, data, 0600); err != nil {
			t.Fatal(err)
		}
		changes = append(changes, retentionStateChange(t, "attachments", "attachment-"+c.Source, map[string]any{"candidate_id": local, "sha256": digest, "relative_path": relative, "size_bytes": len(data)}, c.ProfileAt))
	}
	var version int64
	err := db.QueryRow(`SELECT version FROM auto_reply_state_owners WHERE tenant_id=$1 AND user_id=$2`, tenant, c.User).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if _, err = NewPostgresAutoReplyStore(db).CommitAutoReplyState(context.Background(), tenant, c.User, version, changes); err != nil {
		t.Fatal(err)
	}
	return filePath
}

// TestTemporaryRetentionPreservesUnrelatedLibraryRecords 验证没有在线临时凭据的旧人工及插件简历在选取和事务复核两处均不会被清理。
func TestTemporaryRetentionPreservesUnrelatedLibraryRecords(t *testing.T) {
	db, store, tenant, user := retentionPostgresFixture(t)
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	for _, sourceType := range []string{"manual", "browser_extension"} {
		id := retentionProfile(t, db, tenant, user, sourceType, "", cutoff.Add(-time.Hour), nil)
		if _, err := db.Exec(`UPDATE candidate_profile_sources SET source_type=$2,online_profile='{}' WHERE candidate_id=$1`, id, sourceType); err != nil {
			t.Fatal(err)
		}
		if err := store.purgeCandidate(context.Background(), CandidateLibraryScope{TenantID: tenant, IsAdmin: true, retentionCutoff: &cutoff}, id, 1); !errors.Is(err, errCandidateRetentionChanged) {
			t.Fatalf("缺在线凭据仍进入永久删除：%v", err)
		}
	}
	service := &CandidateRetentionService{store: store}
	items, err := service.expiredCandidates(context.Background(), cutoff)
	if err != nil || len(items) != 0 {
		t.Fatalf("人工或插件资料进入自动临时清理：%+v %v", items, err)
	}
}

// TestTemporaryRetentionProtectsAllActors 验证主档与未入库缓存均保护任一成员的新资料、手机或真实沟通，全部过期才精确清除同源全部成员。
func TestTemporaryRetentionProtectsAllActors(t *testing.T) {
	for _, canonical := range []bool{false, true} {
		for _, protection := range []string{"phone", "communication", "profile", "expired"} {
			t.Run(map[bool]string{false: "cache", true: "library"}[canonical]+"/"+protection, func(t *testing.T) {
				db, store, tenant, user := retentionPostgresFixture(t)
				now := time.Now().UTC().Truncate(time.Microsecond)
				old, cutoff := now.Add(-31*24*time.Hour), now.Add(-30*24*time.Hour)
				var peer string
				if err := db.QueryRow(`INSERT INTO users(tenant_id,email) VALUES($1,'retention-peer@example.test') RETURNING id::text`, tenant).Scan(&peer); err != nil {
					t.Fatal(err)
				}
				id := ""
				if canonical {
					id = retentionProfile(t, db, tenant, user, "shared-source", "", old, nil)
				}
				base := t.TempDir()
				t.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", base)
				first := retentionActorCase{User: user, Source: "shared-source", Canonical: id, ProfileAt: old, MessageAt: old, Online: true, Attachment: protection == "expired", FilesBase: base}
				firstPath := saveRetentionActor(t, db, tenant, first)
				second := first
				second.User = peer
				switch protection {
				case "phone":
					second.Phone = "13800000000"
				case "communication":
					second.MessageAt = now.Add(-time.Hour)
				case "profile":
					second.ProfileAt = now.Add(-time.Hour)
				}
				secondPath := saveRetentionActor(t, db, tenant, second)
				// 不同来源保留同一个成员下的共享文件，不能因为这次删除误清最后一个有效引用。
				unrelated := first
				unrelated.Source, unrelated.Canonical, unrelated.Online = "other-source", "", false
				saveRetentionActor(t, db, tenant, unrelated)
				var job string
				if err := db.QueryRow(`INSERT INTO candidate_retention_jobs(request_day,cutoff,status) VALUES('2026-11-10',$1,'running') RETURNING id::text`, cutoff).Scan(&job); err != nil {
					t.Fatal(err)
				}
				service := &CandidateRetentionService{store: store}
				if err := service.runJob(context.Background(), CandidateRetentionJob{ID: job, Cutoff: cutoff}); err != nil {
					t.Fatal(err)
				}
				var count int
				if err := db.QueryRow(`SELECT count(*) FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND kind='candidates' AND id='local-shared-source'`, tenant).Scan(&count); err != nil {
					t.Fatal(err)
				}
				want := 2
				if protection == "expired" {
					want = 0
				}
				if count != want {
					t.Fatalf("任一成员有效信息保护不符：got=%d want=%d", count, want)
				}
				if err := db.QueryRow(`SELECT count(*) FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND kind='candidates' AND id='local-other-source'`, tenant).Scan(&count); err != nil || count != 1 {
					t.Fatalf("跨来源误删：%d %v", count, err)
				}
				if protection != "expired" {
					return
				}
				if err := db.QueryRow(`SELECT count(*) FROM candidate_attachment_purge_jobs`).Scan(&count); err != nil || count != 2 {
					t.Fatalf("多成员解析清理任务遗漏：%d %v", count, err)
				}
				for _, email := range []string{"retention@example.test", "retention-peer@example.test"} {
					events, err := store.candidateDeletions(context.Background(), tenant, email, false, 0)
					if err != nil || len(events) != 1 {
						t.Fatalf("成员缺少可见删除回执：%s %d %v", email, len(events), err)
					}
				}
				for i := 0; i < 8; i++ {
					did, err := advanceReplyFilePurge(context.Background(), db, base)
					if err != nil {
						t.Fatal(err)
					}
					if !did {
						break
					}
				}
				if _, err := os.Stat(firstPath); err != nil {
					t.Fatalf("不同来源仍引用的共享文件被删：%v", err)
				}
				if _, err := os.Stat(secondPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("另一成员已无引用的附件没有清除：%v", err)
				}
			})
		}
	}
}
