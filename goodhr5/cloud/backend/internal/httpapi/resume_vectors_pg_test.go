// 本文件在独立回环测试库验证向量回执的租户隔离、行版本计数、迟到写入与删除级联。
package httpapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestResumeVectorsPostgresLifecycle 验证真实SQL不能把旧简历版本或别人的向量算成已完成。
func TestResumeVectorsPostgresLifecycle(t *testing.T) {
	db := candidatePostgresTestDB(t)
	migration, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0101_resume_vector_receipts.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	var tenantID, otherTenantID string
	if err = db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&otherTenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO users(email,tenant_id) VALUES('vector-owner@example.com',$1),('vector-member@example.com',$1),('vector-outsider@example.com',$2)`, tenantID, otherTenantID); err != nil {
		t.Fatal(err)
	}
	candidates := NewPostgresCandidateStore(db)
	owner := CandidateLibraryScope{TenantID: tenantID, UserEmail: "vector-owner@example.com"}
	candidate, err := candidates.SaveCandidateProfile(CandidateProfileInput{UserEmail: owner.UserEmail, PlatformID: "manual", PlatformCandidateID: "vector-pg", CandidateName: "合成向量候选人"})
	if err != nil {
		t.Fatal(err)
	}
	store := newResumeVectorStore(db, candidates)
	ctx := context.Background()
	record := resumeVectorRecord{CandidateID: candidate.ID, TenantID: tenantID, OwnerEmail: owner.UserEmail, ResumeVersion: candidate.RowVersion, Profile: resumeVectorProfile, Complete: true, Chunks: []resumeVectorChunk{}}
	if err = store.Save(ctx, owner, record); err != nil {
		t.Fatal(err)
	}
	total, indexed, err := store.Counts(ctx, owner)
	if err != nil || total != 1 || indexed != 1 {
		t.Fatalf("当前版本数量=%d/%d err=%v", total, indexed, err)
	}
	member := CandidateLibraryScope{TenantID: tenantID, UserEmail: "vector-member@example.com"}
	if _, err = store.Get(ctx, member, candidate.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("向量回执泄露给其他成员")
	}
	if _, err = store.Get(ctx, CandidateLibraryScope{TenantID: otherTenantID, UserEmail: "vector-outsider@example.com", IsAdmin: true}, candidate.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("跨团队泄露向量")
	}
	if _, err = db.Exec(`UPDATE candidate_profiles SET row_version=row_version+1 WHERE tenant_id=$1 AND id=$2`, tenantID, candidate.ID); err != nil {
		t.Fatal(err)
	}
	_, indexed, err = store.Counts(ctx, owner)
	if err != nil || indexed != 0 {
		t.Fatal("旧版本仍算完成")
	}
	pending, err := store.Pending(ctx, owner, 20)
	if err != nil || len(pending) != 1 || pending[0] != candidate.ID {
		t.Fatal("更新后没有进入待同步列表")
	}
	if err = store.Save(ctx, owner, record); !errors.Is(err, ErrCandidateVersion) {
		t.Fatal("迟到结果覆盖当前简历")
	}
	if _, err = db.Exec(`DELETE FROM candidate_profiles WHERE tenant_id=$1 AND id=$2`, tenantID, candidate.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM candidate_resume_vectors WHERE tenant_id=$1`, tenantID).Scan(&count); err != nil || count != 0 {
		t.Fatal("删除简历后仍残留数据库向量回执")
	}
}
