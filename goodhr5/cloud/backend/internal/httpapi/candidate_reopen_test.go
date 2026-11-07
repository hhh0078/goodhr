// 本文件验证云端真实删除代次、证据重放、不同租户和来源隔离，以及旧事件迁移的保守截止时间。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// candidateReopenTestStore 仅暴露同一场景使用的真实业务方法，复用回归覆盖内存和 PostgreSQL 两种模式。
type candidateReopenTestStore interface {
	candidatePurgeStore
	SaveLibraryCandidate(CandidateLibraryScope, string, map[string]any, int64, string) (PositionCandidate, error)
	SaveCandidateProfile(CandidateProfileInput) (PositionCandidate, error)
	GetPositionCandidate(string, string, string, string, bool) (PositionCandidate, error)
}

// TestCandidateReopenGeneration 验证新一代可入库且不复用手机号档案，旧证明、迟到附件及跨租户重放始终不能改资料。
func TestCandidateReopenGeneration(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		checkCandidateReopen(t, NewMemoryCandidateStore(), ScreeningScope{TenantID: "one", UserEmail: "a@example.com", PositionID: "p"}, ScreeningScope{TenantID: "two", UserEmail: "b@example.com", PositionID: "q"})
	})
	t.Run("postgres", func(t *testing.T) {
		db, scope, other := screeningPostgresFixture(t)
		for _, name := range []string{"0090_recruitment_workflows.sql", "0091_recruitment_mail.sql", "0104_auto_reply_configuration.sql", "0105_auto_reply_result_summaries.sql"} {
			raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(string(raw)); err != nil {
				t.Fatal(err)
			}
		}
		checkCandidateReopen(t, NewPostgresCandidateStore(db), scope, other)
	})
}

// checkCandidateReopen 使用真实删除和正式保存入口，验证接连删除两代及同一新附件并发提交的处理结果。
func checkCandidateReopen(t *testing.T, s candidateReopenTestStore, scope, foreign ScreeningScope) {
	t.Helper()
	ctx := context.Background()
	lib := CandidateLibraryScope{TenantID: scope.TenantID, UserEmail: scope.UserEmail}
	in := CandidateProfileInput{TenantID: scope.TenantID, UserEmail: scope.UserEmail, PlatformID: "liepin", PlatformCandidateID: "source-one", CandidateName: "旧资料", Phone: "13800138000"}
	old, err := s.SaveCandidateProfile(in)
	if err != nil {
		t.Fatal(err)
	}
	old, err = s.SaveLibraryCandidate(lib, old.ID, map[string]any{"platform_id": in.PlatformID, "platform_candidate_id": "second-known-alias"}, old.RowVersion, "attach-verified-source")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.purgeCandidate(ctx, lib, old.ID, old.RowVersion); err != nil {
		t.Fatal(err)
	}
	ev, err := s.candidateDeletions(ctx, scope.TenantID, scope.UserEmail, false, 0)
	if err != nil || len(ev) != 1 || ev[0].DeletedAt.IsZero() || ev[0].CutoffFromMigration || ev[0].TenantID != scope.TenantID || len(ev[0].ContactHashes) != 1 || ev[0].ContactHashes[0] != purgeDigest("phone", scope.TenantID, in.Phone) {
		t.Fatal("删除事件边界不完整", ev, err)
	}
	in.AutoReplyPositionID = scope.PositionID
	in.ReopenPlatformAccount = "bound:exact"
	in.Ext = map[string]any{"structured_fields": map[string]any{}}
	in.CandidateName = "新附件资料"
	now := time.Now().UTC().Truncate(time.Microsecond)
	p := candidateReopenProof{DeletionSequence: ev[0].Sequence, InboundMessageHash: purgeDigest("inbound1"), InboundAt: now, AttachmentMessageHash: purgeDigest("attachment1"), AttachmentAt: now}
	reject := func(i CandidateProfileInput) {
		t.Helper()
		if _, err := s.SaveCandidateProfile(i); !errors.Is(err, errCandidateDeleted) {
			t.Fatalf("应拒绝旧任务，实际 %v", err)
		}
	}
	reject(in)
	in.ReopenProof = &p
	for _, bad := range []candidateReopenProof{
		{DeletionSequence: p.DeletionSequence, InboundAt: ev[0].DeletedAt, InboundMessageHash: p.InboundMessageHash, AttachmentAt: now, AttachmentMessageHash: p.AttachmentMessageHash},
		{DeletionSequence: p.DeletionSequence, InboundAt: now, InboundMessageHash: p.InboundMessageHash, AttachmentAt: ev[0].DeletedAt, AttachmentMessageHash: p.AttachmentMessageHash},
		{DeletionSequence: p.DeletionSequence, InboundAt: now.Add(time.Hour), InboundMessageHash: p.InboundMessageHash, AttachmentAt: now, AttachmentMessageHash: p.AttachmentMessageHash},
		{DeletionSequence: p.DeletionSequence + 1, InboundAt: now, InboundMessageHash: p.InboundMessageHash, AttachmentAt: now, AttachmentMessageHash: p.AttachmentMessageHash},
	} {
		v := in
		v.ReopenProof = &bad
		reject(v)
	}
	unrelated := in
	unrelated.PlatformCandidateID = "another-source"
	reject(unrelated)
	outsider := in
	outsider.UserEmail = foreign.UserEmail
	outsider.TenantID = foreign.TenantID
	outsider.AutoReplyPositionID = foreign.PositionID
	reject(outsider)
	var wg sync.WaitGroup
	saved := make([]PositionCandidate, 3)
	errs := make([]error, len(saved))
	for i := range saved {
		wg.Add(1)
		go func() { defer wg.Done(); saved[i], errs[i] = s.SaveCandidateProfile(in) }()
	}
	wg.Wait()
	for i, item := range saved {
		if errs[i] != nil || item.ID == "" || item.ID == old.ID || item.ID != saved[0].ID || item.RowVersion != saved[0].RowVersion {
			t.Fatal("并发入库不是同一新简历版本", i, item.ID, item.RowVersion, errs[i])
		}
	}
	ev, err = s.candidateDeletions(ctx, scope.TenantID, scope.UserEmail, false, 0)
	if err != nil || len(ev) != 1 || len(ev[0].BoundarySourceHashes) != 2 || len(ev[0].SourceHashes) != 1 || ev[0].SourceHashes[0] != purgeSourceHash(in.PlatformID, "second-known-alias") || len(ev[0].ContactHashes) != 0 {
		t.Fatal("迟到事件不能按旧来源或手机号清理新档", ev, err)
	}
	oldAlias := in
	oldAlias.PlatformCandidateID = "second-known-alias"
	oldAlias.Phone = "13800138009"
	reject(oldAlias)
	wrongAccount := in
	wrongAccount.ReopenPlatformAccount = "another-account"
	reject(wrongAccount)
	raw := in
	raw.ReopenProof = nil
	reject(raw)
	raw.PlatformCandidateID = "unknown-old-same-phone"
	reject(raw)
	// 不允许来源正确但手机号恰好属于同团队另一个人的新附件覆盖对方。
	separate := CandidateProfileInput{TenantID: scope.TenantID, UserEmail: scope.UserEmail, PlatformID: in.PlatformID, PlatformCandidateID: "independent-person", CandidateName: "独立资料", Phone: "13800138001"}
	peer, err := s.SaveCandidateProfile(separate)
	if err != nil {
		t.Fatal(err)
	}
	conflict := in
	conflict.Phone = separate.Phone
	different := p
	different.AttachmentMessageHash = purgeDigest("other-number")
	different.AttachmentAt = time.Now().UTC()
	conflict.ReopenProof = &different
	if _, err = s.SaveCandidateProfile(conflict); !errors.Is(err, ErrCandidateDuplicate) {
		t.Fatal("不同候选人的号码被合并", err)
	}
	retained, err := s.GetPositionCandidate(scope.TenantID, peer.ID, "", scope.UserEmail, false)
	if err != nil || retained.CandidateName != separate.CandidateName {
		t.Fatal("独立简历被改写", err)
	}
	if pg, ok := s.(*PostgresCandidateStore); ok {
		reports := NewPostgresAutoReplyStore(pg.db)
		report := CandidateAutoReplyReport{TenantID: scope.TenantID, PositionID: scope.PositionID, CandidateID: old.ID, InputHash: purgeDigest("report"), Report: CandidateRecommendationReport{GeneratedAt: time.Now().UTC()}}
		if _, err = reports.SaveAutoReplyReport(ctx, report); !errors.Is(err, errCandidateDeleted) {
			t.Fatal("旧简历可发布迟到报告", err)
		}
		report.CandidateID = saved[0].ID
		stored, err := reports.SaveAutoReplyReport(ctx, report)
		if err != nil || stored.CandidateID != saved[0].ID || stored.ShareEnabled {
			t.Fatal("新报告没有仅关联新档", err)
		}
	}
	// 同一代更晚的附件可增量更新，随后到达的旧附件不能覆盖。
	later := in
	newProof := p
	newProof.AttachmentAt = time.Now().UTC().Truncate(time.Microsecond)
	newProof.AttachmentMessageHash = purgeDigest("attachment2")
	later.ReopenProof = &newProof
	later.CandidateName = "更新附件"
	current, err := s.SaveCandidateProfile(later)
	if err != nil || current.ID != saved[0].ID || current.CandidateName != later.CandidateName {
		t.Fatal("新附件未正确更新", current.ID, err)
	}
	reject(in)
	if err = s.purgeCandidate(ctx, lib, current.ID, current.RowVersion); err != nil {
		t.Fatal(err)
	}
	reject(later)
	reject(oldAlias)
	ev, err = s.candidateDeletions(ctx, scope.TenantID, scope.UserEmail, false, p.DeletionSequence)
	if err != nil || len(ev) != 1 {
		t.Fatal(ev, err)
	}
	next := later
	proof2 := newProof
	proof2.DeletionSequence = ev[0].Sequence
	proof2.InboundAt = time.Now().UTC()
	proof2.AttachmentAt = proof2.InboundAt
	proof2.InboundMessageHash = purgeDigest("next-in")
	proof2.AttachmentMessageHash = purgeDigest("next-file")
	next.ReopenProof = &proof2
	newest, err := s.SaveCandidateProfile(next)
	if err != nil || newest.ID == current.ID || newest.ID == old.ID {
		t.Fatal("第二次删除阻碍新代次", newest.ID, err)
	}
	if _, err = s.GetPositionCandidate(scope.TenantID, old.ID, "", scope.UserEmail, false); !errors.Is(err, ErrNotFound) {
		t.Fatal("旧ID重新可见", err)
	}
}

// TestReopenLegacyCutoff 验证没有历史删除时间的旧事件采用迁移时刻作下限，不能将过去的消息解释为新消息。
func TestReopenLegacyCutoff(t *testing.T) {
	db := candidatePostgresTestDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DROP TABLE candidate_reopen_receipts; ALTER TABLE candidate_deletions DROP COLUMN deleted_at, DROP COLUMN cutoff_from_migration, DROP COLUMN contact_hashes"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`WITH t AS (INSERT INTO tenants DEFAULT VALUES RETURNING id) INSERT INTO candidate_deletions(tenant_id,candidate_id,row_version,owner_hash,audiences,source_hashes) SELECT id,gen_random_uuid(),1,'old','{}','[]' FROM t`); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	if err = tx.QueryRow("SELECT clock_timestamp()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0109_candidate_reopen.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	var cutoff time.Time
	var estimated bool
	if err = tx.QueryRow("SELECT deleted_at,cutoff_from_migration FROM candidate_deletions").Scan(&cutoff, &estimated); err != nil || !estimated || cutoff.Before(before) {
		t.Fatal("旧记录截止时间不可信", cutoff, before, estimated, err)
	}
}
