// 本文件用隔离PostgreSQL与合成搜索服务验证删除待办不会因服务故障丢失，也不会误删新版本。
package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// TestResumeSearchInvalidationPostgres 验证真实触发器、归属变更递增版本、失败保留待办及成功清理旧副本。
func TestResumeSearchInvalidationPostgres(t *testing.T) {
	db := candidatePostgresTestDB(t)
	for _, name := range []string{"0101_resume_vector_receipts.sql", "0102_resume_search_invalidation.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	var tenantID, id string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES('invalidate-owner@example.com',$1),('invalidate-member@example.com',$1)`, tenantID); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresCandidateStore(db)
	candidate, err := store.SaveCandidateProfile(CandidateProfileInput{UserEmail: "invalidate-owner@example.com", PlatformID: "manual", PlatformCandidateID: "invalidate", CandidateName: "合成清理简历"})
	if err != nil {
		t.Fatal(err)
	}
	id = candidate.ID
	scope := CandidateLibraryScope{TenantID: tenantID, UserEmail: "invalidate-owner@example.com"}
	vectors := newResumeVectorStore(db, store)
	if err = vectors.Save(context.Background(), scope, resumeVectorRecord{CandidateID: id, TenantID: tenantID, OwnerEmail: scope.UserEmail, ResumeVersion: candidate.RowVersion, Profile: resumeVectorProfile, Complete: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE candidate_profiles SET created_by_user_id=(SELECT id FROM users WHERE email='invalidate-member@example.com') WHERE tenant_id=$1 AND id=$2`, tenantID, id); err != nil {
		t.Fatal(err)
	}
	var version, through int64
	if err = db.QueryRow(`SELECT row_version FROM candidate_profiles WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&version); err != nil || version != candidate.RowVersion+1 {
		t.Fatal("归属变化没有使简历版本递增")
	}
	if err = db.QueryRow(`SELECT through_version FROM resume_search_invalidations WHERE tenant_id=$1 AND candidate_id=$2`, tenantID, id).Scan(&through); err != nil || through != candidate.RowVersion {
		t.Fatal("清理边界没有保留旧版本")
	}
	var fail atomic.Bool
	fail.Store(true)
	searchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/_delete_by_query") {
			t.Error("发出非清理请求")
		}
		if fail.Load() {
			_, _ = fmt.Fprint(w, `{"deleted":0,"timed_out":true,"failures":[]}`)
		} else {
			_, _ = fmt.Fprint(w, `{"deleted":2,"timed_out":false,"version_conflicts":0,"failures":[]}`)
		}
	}))
	defer searchServer.Close()
	worker := &resumeSearchInvalidator{db: db, search: &elasticResumeSearch{base: searchServer.URL, index: "test-index", client: searchServer.Client()}}
	if done, err := worker.once(context.Background()); err == nil || done {
		t.Fatal("搜索超时后仍删除了清理待办")
	}
	fail.Store(false)
	if done, err := worker.once(context.Background()); err != nil || !done {
		t.Fatalf("清理失败: %v", err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM resume_search_invalidations WHERE tenant_id=$1`, tenantID).Scan(&count); err != nil || count != 0 {
		t.Fatal("成功清理未确认待办")
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM candidate_resume_vectors WHERE tenant_id=$1 AND candidate_id=$2`, tenantID, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("旧持久向量未清理")
	}
	if _, err = db.Exec(`DELETE FROM candidate_profiles WHERE tenant_id=$1 AND id=$2`, tenantID, id); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT through_version FROM resume_search_invalidations WHERE tenant_id=$1 AND candidate_id=$2`, tenantID, id).Scan(&through); err != nil || through != version {
		t.Fatal("删除简历的清理意图丢失")
	}
}
