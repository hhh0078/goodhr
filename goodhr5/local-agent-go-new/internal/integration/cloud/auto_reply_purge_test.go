// Package cloud 本文件验证永久删除按账号和稳定身份执行，清除旧快照、受管文件及在途任务，不接触用户下载原件。
package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/storage"
)

// purgeTestClient 构造隔离数据库、已认证测试账号与私有附件目录，测试期间不连接真实服务。
func purgeTestClient(t *testing.T, base string) (*Client, *storage.Store, AgentCredentials) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	c := New(base)
	c.EnableLocalAutoReply(store, filepath.Join(t.TempDir(), "auto-reply"))
	cred := AgentCredentials{Token: "test-A", MachineID: "test-machine"}
	c.localAutoOwners.Store(autoReplyDigest(cred.Token), "owner-A")
	return c, store, cred
}

// TestPurgeAutoReplyEntitiesAndLegacy 验证同一云ID的多个本地行、反向引用、旧快照和后到写入均被正确隔离。
func TestPurgeAutoReplyEntitiesAndLegacy(t *testing.T) {
	c, store, cred := purgeTestClient(t, "")
	ctx := context.Background()
	raw := `{"candidates":{"a":{"ID":"a","CandidateName":"private A"},"b":{"ID":"b","CandidateName":"keep B"}},"identities":{"ia":{"id":"ia","candidate_id":"a"}},"conversations":{"chat":{"id":"chat","candidate_id":"a"}},"messages":{"chat":[{"id":"m","fingerprint":"p","text_content":"private A"}]},"runs":{"r":{"id":"r","conversation_id":"chat"}},"tools":{"t":{"id":"t","ai_run_id":"r","tool_name":"private A"}},"reports":{"hash":{"candidate":{"id":"a"},"executive_summary":"private A"}},"report_attempts":{"hash":1},"cloud_candidates":{"a":"cloud-A","a2":"cloud-A","b":"cloud-B"},"cloud_candidate_hashes":{"a|position":"private A","b|position":"keep"},"unknown_future_metadata":{"keep":true}}`
	if err := store.SetSetting(ctx, c.localAutoReplyKey(cred), raw); err != nil {
		t.Fatal(err)
	}
	other := AgentCredentials{Token: "test-B", MachineID: "machine"}
	c.localAutoOwners.Store(autoReplyDigest(other.Token), "owner-B")
	if err := store.SetSetting(ctx, c.localAutoReplyKey(other), raw); err != nil {
		t.Fatal(err)
	}
	refs := map[string]map[string]string{
		"confirmations": {"candidate_id": "a2"}, "suggestions": {"conversation_id": "chat"}, "sends": {"conversation_id": "chat"},
		"proposal_receipts": {"conversation_id": "chat"}, "recommendations": {"conversation_id": "chat", "candidate_id": "a", "input_hash": "leased-report"},
	}
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		for kind, fields := range refs {
			if err := tx.Put(kind, "old", fields); err != nil {
				return err
			}
		}
		if err := tx.Put("reports", "leased-report", localReportState{Attempts: 1}); err != nil {
			return err
		}
		return tx.Put("outbox", "notice:old", map[string]any{"kind": "notice", "payload": map[string]string{"conversation_id": "chat", "reason": "private A"}})
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := c.PurgeLocalAutoReplyCandidate(ctx, cred, "cloud-A", nil); err != nil {
			t.Fatal(err)
		}
	}
	cleaned, err := store.GetSetting(ctx, c.localAutoReplyKey(cred))
	if err != nil || strings.Contains(cleaned, "private A") || strings.Contains(cleaned, "cloud-A") || !strings.Contains(cleaned, "keep B") || !strings.Contains(cleaned, "unknown_future_metadata") {
		t.Fatalf("旧快照清理或未知字段保持失败 %v", err)
	}
	if otherRaw, _ := store.GetSetting(ctx, c.localAutoReplyKey(other)); otherRaw != raw {
		t.Fatal("改动了其他账号旧快照")
	}
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		for _, kind := range []string{"identities", "conversations", "messages", "runs", "tools", "confirmations", "suggestions", "sends", "proposal_receipts", "recommendations", "reports", "outbox"} {
			rows, err := tx.List(kind)
			if err != nil || len(rows) != 0 {
				return fmt.Errorf("%s未清空 %d %v", kind, len(rows), err)
			}
		}
		if _, found, err := tx.Get("cloud_links", "hash:a|position"); err != nil || found {
			return fmt.Errorf("旧版本岗位同步摘要未清理")
		}
		if _, found, err := tx.Get("cloud_links", "hash:b|position"); err != nil || !found {
			return fmt.Errorf("影响了另一人旧版本岗位摘要")
		}
		if _, found, err := tx.Get("candidates", "b"); err != nil || !found {
			return fmt.Errorf("影响了其他简历 %v", err)
		}
		for kind, value := range map[string]any{"messages": map[string]string{"conversation_id": "chat"}, "tools": map[string]string{"ai_run_id": "r"}, "outbox": map[string]any{"payload": map[string]string{"local_candidate_id": "a2"}}} {
			if err := tx.Put(kind, "late", value); !errors.Is(err, storage.ErrAutoReplyCandidateDeleted) {
				return fmt.Errorf("%s旧写入未拦截 %v", kind, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPurgeSourceAndSharedAttachments 验证唯一平台来源、下载原件、共享文件、冲突账号以及重新采集的隔离。
func TestPurgeSourceAndSharedAttachments(t *testing.T) {
	c, _, cred := purgeTestClient(t, "")
	ctx := context.Background()
	original := filepath.Join(t.TempDir(), "source.pdf")
	if err := os.WriteFile(original, []byte("shared synthetic attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		for _, v := range []struct{ id, account, platformID string }{{"a", "owner-A", "platform-one"}, {"b", "owner-A", "platform-two"}, {"c", "owner-A", "ambiguous"}, {"d", "owner-B", "ambiguous"}, {"e", "owner-A", "belongs-to-different-cloud"}} {
			if err := tx.Put("candidates", v.id, AutoReplyStoredCandidate{ID: v.id}); err != nil {
				return err
			}
			if err := tx.Put("identities", "i"+v.id, CandidatePlatformIdentity{ID: "i" + v.id, CandidateID: v.id, PlatformID: "liepin", PlatformAccountID: v.account, PlatformCandidateID: v.platformID}); err != nil {
				return err
			}
			if err := tx.Put("conversations", "t"+v.id, AutoReplyConversation{ID: "t" + v.id, PlatformID: "liepin", PlatformAccountID: v.account, PlatformThreadID: "thread-" + v.id, PlatformIdentityID: "i" + v.id}); err != nil {
				return err
			}
		}
		return tx.Put("cloud_links", "candidate:e", localCloudLink{"other-cloud"})
	}); err != nil {
		t.Fatal(err)
	}
	var path string
	for _, id := range []string{"a", "b"} {
		v, err := c.saveLocalAutoReplyAttachment(ctx, cred, AutoReplyAttachmentUpload{FilePath: original, CandidateID: id, ConversationID: "t" + id})
		if err != nil {
			t.Fatal(err)
		}
		path = filepath.Join(c.localAutoDir, autoReplyDigest(c.localAutoReplyKey(cred)), "attachments", v.SHA256+".pdf")
	}
	hashes := func(s string) []string { return []string{autoReplyDigest("liepin\x00" + s)} }
	for _, s := range []string{"platform-one", "ambiguous", "belongs-to-different-cloud"} {
		if err := c.PurgeLocalAutoReplyCandidate(ctx, cred, "cloud-A", hashes(s)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("删除了仍由B引用的受管文件")
	}
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		for _, id := range []string{"b", "c", "d", "e"} {
			if _, found, err := tx.Get("candidates", id); err != nil || !found {
				return fmt.Errorf("误删候选人%s", id)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// 回执丢失后重投，不能把原本跨账号同平台ID的另一个人误认成新出现的唯一匹配。
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		return tx.Put("cloud_links", "candidate:c", localCloudLink{"resolved-cloud"})
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := c.PurgeLocalAutoReplyCandidate(ctx, cred, "resolved-cloud", hashes("ambiguous")); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		_, ok, e := tx.Get("candidates", "d")
		if e != nil || !ok {
			return fmt.Errorf("重复事件错误删除了另一账号的同名平台来源")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.saveLocalAutoReplyAttachment(ctx, cred, AutoReplyAttachmentUpload{FilePath: original, CandidateID: "a", ConversationID: "ta"}); !errors.Is(err, storage.ErrAutoReplyCandidateDeleted) {
		t.Fatalf("旧附件回写被允许 %v", err)
	}
	if err := c.PurgeLocalAutoReplyCandidate(ctx, cred, "cloud-B", hashes("platform-two")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("独占受管文件未清理 %v", err)
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatal("下载目录原件受到影响")
	}
}

// TestPurgeLateCloudReceipt 验证删除先于首次上传回执，或云端拒绝旧上传时，网络回调不能复活本地数据。
func TestPurgeLateCloudReceipt(t *testing.T) {
	for _, gone := range []bool{false, true} {
		t.Run(fmt.Sprint("gone-", gone), func(t *testing.T) {
			received, release := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(received)
				<-release
				if gone {
					w.WriteHeader(http.StatusGone)
					fmt.Fprint(w, `{"code":"CANDIDATE_DELETED","message":"已删除"}`)
					return
				}
				fmt.Fprint(w, `{"candidate_id":"cloud-late"}`)
			}))
			defer server.Close()
			c, _, cred := purgeTestClient(t, server.URL)
			ctx := context.Background()
			local := "local-late"
			payload, _ := json.Marshal(autoReplyCandidateUpload{LocalCandidateID: local, Input: AutoReplyCandidateInput{PlatformID: "liepin"}})
			if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
				if err := tx.Put("candidates", local, AutoReplyStoredCandidate{ID: local}); err != nil {
					return err
				}
				if gone {
					// 此时云端编号已建立精确映射；先到的独立删除事件可以确定本地归属。
					if err := tx.Put("cloud_links", "candidate:"+local, localCloudLink{Value: "cloud-late"}); err != nil {
						return err
					}
				}
				return tx.Put("outbox", "in-flight", autoReplyOutboxItem{ID: "in-flight", Kind: "candidate", Status: "pending", Payload: payload})
			}); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- c.processAutoReplyOutbox(ctx, cred, "in-flight") }()
			<-received
			if err := c.PurgeLocalAutoReplyCandidate(ctx, cred, "cloud-late", nil); err != nil {
				close(release)
				t.Fatal(err)
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
				for _, kind := range []string{"candidates", "cloud_links", "outbox"} {
					rows, err := tx.List(kind)
					if err != nil || len(rows) > 0 {
						return fmt.Errorf("迟到回执恢复了 %s %v", kind, err)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestPurgeAttachmentReplacement 保留被替换的文件并返回未完成；原文件恢复后可重复投递完成回收。
func TestPurgeAttachmentReplacement(t *testing.T) {
	c, _, cred := purgeTestClient(t, "")
	ctx := context.Background()
	original := filepath.Join(t.TempDir(), "original.pdf")
	data := []byte("original content")
	if err := os.WriteFile(original, data, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := c.saveLocalAutoReplyAttachment(ctx, cred, AutoReplyAttachmentUpload{CandidateID: "a", ConversationID: "t", FilePath: original})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.localAutoDir, autoReplyDigest(c.localAutoReplyKey(cred)), "attachments", a.SHA256+".pdf")
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		return tx.Put("cloud_links", "candidate:a", localCloudLink{"cloud"})
	}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = c.PurgeLocalAutoReplyCandidate(ctx, cred, "cloud", nil); err == nil {
		t.Fatal("替换后的文件不应报告清理成功")
	}
	if got, _ := os.ReadFile(path); string(got) != "different" {
		t.Fatal("未知来源文件被删除")
	}
	// A的受管文件仍待核对，B自己的文件和回执却必须可以独立完成。
	otherOriginal := filepath.Join(t.TempDir(), "other.pdf")
	if err = os.WriteFile(otherOriginal, []byte("other private bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	other, err := c.saveLocalAutoReplyAttachment(ctx, cred, AutoReplyAttachmentUpload{CandidateID: "b", ConversationID: "t2", FilePath: otherOriginal})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		return tx.Put("cloud_links", "candidate:b", localCloudLink{"cloud-B"})
	}); err != nil {
		t.Fatal(err)
	}
	if err = c.PurgeLocalAutoReplyCandidate(ctx, cred, "cloud-B", nil); err != nil {
		t.Fatalf("A的失败阻塞了B：%v", err)
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(path), other.SHA256+".pdf")); !os.IsNotExist(err) {
		t.Fatal("B的独占文件没有清理")
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = c.PurgeLocalAutoReplyCandidate(ctx, cred, "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("未恢复已提交的文件回收意图")
	}
	h := sha256.Sum256(data)
	digest := hex.EncodeToString(h[:])
	if c.validPurgeAttachmentPath(cred, "../"+digest+".pdf", digest) {
		t.Fatal("路径穿越被放行")
	}
}

// TestPurgeSkipsSymlinkTarget 验证目录符号链接即使指向内容摘要一致的文件，也不能使清理越过私有目录。
func TestPurgeSkipsSymlinkTarget(t *testing.T) {
	c, _, cred := purgeTestClient(t, "")
	outside := t.TempDir()
	data := []byte("user source")
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	name := digest + ".pdf"
	if err := os.WriteFile(filepath.Join(outside, name), data, 0600); err != nil {
		t.Fatal(err)
	}
	owner := autoReplyDigest(c.localAutoReplyKey(cred))
	base := filepath.Join(c.localAutoDir, owner)
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "attachments")); err != nil {
		t.Skip("当前系统账号无法创建测试符号链接")
	}
	if err := c.removePrivateAutoReplyAttachment(cred, filepath.Join(owner, "attachments", name), digest); err == nil {
		t.Fatal("符号链接目录没有被识别")
	}
	if got, err := os.ReadFile(filepath.Join(outside, name)); err != nil || string(got) != string(data) {
		t.Fatal("外部文件被修改")
	}
}

// serverPurgeFixture 只保存测试事务版本，让临时投影复用真实的 PG 回收意图结构。
type serverPurgeFixture struct {
	snapshot storage.AutoReplyStateSnapshot
}

// Load 返回上次提交的数据，已加载版本的读取只返回命中信号。
func (f *serverPurgeFixture) Load(_ context.Context, version *int64) (storage.AutoReplyStateSnapshot, bool, error) {
	return f.snapshot, version != nil && *version == f.snapshot.Version, nil
}

// Commit 保存测试回收意图；若临时投影擅自删除，调用方能从版本和文件状态发现。
func (f *serverPurgeFixture) Commit(_ context.Context, expected int64, changes []storage.AutoReplyStateChange) (int64, error) {
	if expected != f.snapshot.Version {
		return 0, errors.New("版本冲突")
	}
	f.snapshot.Version++
	f.snapshot.Entities = append(f.snapshot.Entities, changes...)
	return f.snapshot.Version, nil
}

// TestServerPurgeDelegatesToPGLockedCollector 验证即使共享卷可写，服务器投影也不直接删除文件或提前确认回收意图。
func TestServerPurgeDelegatesToPGLockedCollector(t *testing.T) {
	authority := &serverPurgeFixture{}
	db, err := storage.OpenAutoReplyScratch(authority)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cred := AgentCredentials{Token: "test-token", MachineID: "device"}
	c := New("http://127.0.0.1")
	c.localAutoOwners.Store(autoReplyDigest(cred.Token), "server-user")
	root := t.TempDir()
	c.EnableLocalAutoReply(db, root)
	data := []byte("synthetic")
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	relative := filepath.Join(autoReplyDigest(c.localAutoReplyKey(cred)), "attachments", digest+".pdf")
	path := filepath.Join(root, relative)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error { return tx.QueuePurgedAttachment("test-event", relative, digest) }); err != nil {
		t.Fatal(err)
	}
	v := authority.snapshot.Version
	if err = c.cleanPurgedEventFiles(ctx, cred, "test-event"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil || authority.snapshot.Version != v {
		t.Fatal("投影擅自回收了共享卷或确认了待办", err)
	}
}
