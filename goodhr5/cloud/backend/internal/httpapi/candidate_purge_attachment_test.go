// 本文件验证真实 Redis 与 PostgreSQL 删除待办衔接及内存模式回归，不访问外部缓存或业务资料。
package httpapi

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCandidatePurgeAttachmentRedis 验证已确认导入关联的旧 quick/full 缓存删除、迟到解析不可重建、同名未关联资料保留。
func TestCandidatePurgeAttachmentRedis(t *testing.T) {
	addr := os.Getenv("GOODHR_EXTENSION_REDIS_TEST_ADDR")
	host, _, err := net.SplitHostPort(addr)
	if addr == "" {
		t.Skip("需要显式指定临时回环Redis")
	}
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		t.Fatal("只允许回环测试Redis")
	}
	ctx := context.Background()
	auth := NewRedisAuthStore(addr, "", 0)
	defer auth.client.Close()
	cache := newAttachmentParseStore(auth).(*redisAttachmentParseStore)
	db, screen, _ := screeningPostgresFixture(t)
	for _, name := range []string{"0090_recruitment_workflows.sql", "0091_recruitment_mail.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	store := NewPostgresCandidateStore(db)
	guarded := guardedAttachmentStore{inner: cache, guard: store}
	stamp := t.TempDir()
	owner := screen.UserEmail
	scope := CandidateLibraryScope{TenantID: screen.TenantID, UserEmail: owner}
	record := func(phase, capture, text string) attachmentParseRecord {
		return attachmentParseRecord{Owner: owner, Version: 1, LeaseUntil: time.Now().Add(time.Minute).UnixMilli(), Task: attachmentParseTask{ID: purgeDigest(phase, capture, text, stamp), Phase: phase, CaptureID: capture, TextHash: purgeDigest(text), Status: "running", Resume: map[string]any{"candidate_name": "同名测试", "raw_text": text}}}
	}
	anchor := record("import", "capture-"+stamp, "测试正文")
	anchor.Import = &attachmentImportState{Scope: scope, Fields: map[string]any{"candidate_name": "同名测试", "phone": "13800138000"}}
	full := record("full", anchor.Task.CaptureID, "测试正文")
	quick := record("quick", anchor.Task.CaptureID, "测试正文")
	unrelated := record("full", "other-"+stamp, "不同正文")
	save := func(r attachmentParseRecord) {
		t.Helper()
		if _, ok, err := cache.Reserve(ctx, r); err != nil || !ok {
			t.Fatal("预占失败", err)
		}
		r.Task.Status = "completed"
		r.Version++
		if ok, err := cache.Swap(ctx, r, 1); err != nil || !ok {
			t.Fatal("保存终态失败", err)
		}
		t.Cleanup(func() { _ = cache.erasePurgeRecord(ctx, r) })
	}
	for _, r := range []attachmentParseRecord{anchor, full, quick, unrelated} {
		save(r)
	}
	item, err := store.SaveLibraryCandidate(scope, "", map[string]any{"candidate_name": "同名测试", "phone": "13800138000"}, 0, "attachment-import:"+anchor.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.purgeCandidate(ctx, scope, item.ID, item.RowVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = guarded.Get(ctx, owner, full.Task.ID); !errors.Is(err, errCandidateDeleted) {
		t.Fatal("清理开始前暴露旧全文", err)
	}
	// 独立后台实例从数据库续接游标；每轮只扫描有限数量的本账号记录。
	for i := 0; i < 100; i++ {
		worked, e := advanceAttachmentPurge(ctx, db, cache)
		if e != nil {
			t.Fatal(e)
		}
		if !worked {
			break
		}
	}
	for _, r := range []attachmentParseRecord{anchor, full, quick} {
		if _, e := cache.Get(ctx, owner, r.Task.ID); !errors.Is(e, ErrNotFound) {
			t.Fatal("物理缓存还在", r.Task.Phase, e)
		}
		if _, ok, e := guarded.Reserve(ctx, r); ok || !errors.Is(e, errCandidateDeleted) {
			t.Fatal("旧请求可以重建", r.Task.Phase, e)
		}
		if ok, e := guarded.Swap(ctx, r, 1); ok || !errors.Is(e, errCandidateDeleted) {
			t.Fatal("迟到模型结果复活", r.Task.Phase, e)
		}
	}
	if _, err = guarded.Get(ctx, owner, unrelated.Task.ID); err != nil {
		t.Fatal("删除了同名但不同捕获", err)
	}
}

// TestCandidatePurgeMemoryAttachments 验证服务装配的内存解析记录同步清除，本账号不同捕获与伪造的幂等键不使无关资料被删。
func TestCandidatePurgeMemoryAttachments(t *testing.T) {
	svc := mustNewServer(t)
	store := svc.candidates.store.(*MemoryCandidateStore)
	ctx := context.Background()
	owner := "cache-synthetic@example.com"
	tenant, _ := svc.candidates.tenantStore.GetOrCreateTenant(owner)
	scope := CandidateLibraryScope{TenantID: tenant.ID, UserEmail: owner}
	cache := store.purgeLinks.attachments
	target, err := store.SaveLibraryCandidate(scope, "", map[string]any{"candidate_name": "同名", "phone": "13800138000"}, 0, "new")
	if err != nil {
		t.Fatal(err)
	}
	a := attachmentParseRecord{Owner: owner, Version: 1, Task: attachmentParseTask{ID: purgeDigest("import"), Phase: "import", CaptureID: "bind", TextHash: purgeDigest("exact raw")}, Import: &attachmentImportState{Scope: scope, CandidateID: target.ID}}
	b := attachmentParseRecord{Owner: owner, Version: 1, Task: attachmentParseTask{ID: purgeDigest("full"), Phase: "full", CaptureID: "bind", TextHash: a.Task.TextHash}}
	other := b
	other.Task.ID = purgeDigest("unrelated")
	other.Task.CaptureID = "different"
	other.Task.TextHash = purgeDigest("other raw")
	cache.mu.Lock()
	for _, r := range []attachmentParseRecord{a, b, other} {
		cache.records[r.Task.ID] = r
	}
	cache.mu.Unlock()
	store.mu.Lock()
	store.requests[candidateRequestKey(scope, "candidate:", "attachment-import:"+other.Task.ID)] = candidateRequestReceipt{ResultID: target.ID, Fingerprint: "forged"}
	store.mu.Unlock()
	if err := store.purgeCandidate(ctx, scope, target.ID, target.RowVersion); err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	preserved := len(cache.records) == 1 && cache.records[other.Task.ID].Owner == owner
	cache.mu.Unlock()
	if !preserved {
		t.Fatal("内存清理范围不正确")
	}
	if gone, err := store.attachmentRevoked(ctx, b); err != nil || !gone {
		t.Fatal("旧快照仍可写回", err)
	}
	if gone, err := store.attachmentRevoked(ctx, other); err != nil || gone {
		t.Fatal("无关快照受影响", err)
	}
}
