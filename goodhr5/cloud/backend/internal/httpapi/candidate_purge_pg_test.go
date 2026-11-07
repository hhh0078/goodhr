// 本文件使用回环测试 PostgreSQL 独立 schema 验证真实删除、筛选快照、邮件与共享链接的事务边界。
package httpapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCandidatePurgePostgres 验证无外键数据手动清除、搜索失效触发器、已删来源拒绝旧上传及同名资料保留。
func TestCandidatePurgePostgres(t *testing.T) {
	db, screen, _ := screeningPostgresFixture(t)
	ctx := context.Background()
	for _, name := range []string{"0089_candidate_shares.sql", "0090_recruitment_workflows.sql", "0091_recruitment_mail.sql", "0101_resume_vector_receipts.sql", "0102_resume_search_invalidation.sql", "0105_auto_reply_result_summaries.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatal(name, err)
		}
	}
	store := NewPostgresCandidateStore(db)
	scope := CandidateLibraryScope{TenantID: screen.TenantID, UserEmail: screen.UserEmail}
	input := CandidateProfileInput{UserEmail: scope.UserEmail, PlatformID: "boss", PlatformCandidateID: "verified-source", Phone: "13800138000", CandidateName: "合成同名人选"}
	item, err := store.SaveCandidateProfile(input)
	if err != nil {
		t.Fatal(err)
	}
	otherInput := input
	otherInput.Phone = "13800138001"
	otherInput.PlatformCandidateID = "other"
	other, err := store.SaveCandidateProfile(otherInput)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(q, err)
		}
	}
	exec("INSERT INTO candidate_shares(token_hash,tenant_id,candidate_id,user_email) VALUES('test-link',$1,$2,$3)", scope.TenantID, item.ID, scope.UserEmail)
	exec("INSERT INTO candidate_auto_reply_reports(public_id,tenant_id,candidate_id,position_id,version,input_hash,report_data,share_enabled) VALUES('test-report',$1,$2,$3,1,'source','{}',true)", scope.TenantID, item.ID, screen.PositionID)
	exec("INSERT INTO candidate_resume_vectors(tenant_id,candidate_id,resume_version,profile) VALUES($1,$2,1,'synth')", scope.TenantID, item.ID)
	exec("INSERT INTO recruitment_applications(id,tenant_id,candidate_id,position_key,payload) VALUES('app',$1,$2,'p','{}')", scope.TenantID, item.ID)
	exec("INSERT INTO recruitment_invitations(token_hash,tenant_id,application_id,interview_id,interview_revision,expires_at) VALUES('link',$1,'app','interview',1,now()+interval '1 day')", scope.TenantID)
	exec("INSERT INTO recruitment_mail_deliveries(tenant_id,actor_email,application_id,idempotency_key,fingerprint,snapshot) VALUES($1,'actor','app','key','f','{}')", scope.TenantID)
	exec("INSERT INTO candidate_library_requests(tenant_id,actor_email,operation,request_key,fingerprint,result_id) VALUES($1,$2,'candidate:','opaque-import','f',$3)", scope.TenantID, scope.UserEmail, item.ID)
	screening := NewPostgresScreeningRecordStore(db)
	snapshot := screeningTestSnapshot(0)
	snapshot.PlatformCandidateID = input.PlatformCandidateID
	if _, err = screening.Save(ctx, screen, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = store.purgeCandidate(ctx, CandidateLibraryScope{TenantID: scope.TenantID, UserEmail: "other"}, item.ID, item.RowVersion); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err = store.purgeCandidate(ctx, scope, item.ID, item.RowVersion+1); !errors.Is(err, ErrCandidateVersion) {
		t.Fatal(err)
	}
	// 注入最后一条事件写入失败，必须回滚此前的筛选、面试邀请、邮件和主体删除。
	exec("ALTER TABLE candidate_deletions ADD CONSTRAINT test_rollback CHECK(false) NOT VALID")
	if err = store.purgeCandidate(ctx, scope, item.ID, item.RowVersion); err == nil {
		t.Fatal("模拟失败被吞掉")
	}
	for _, table := range []string{"candidate_shares", "recruitment_invitations", "recruitment_mail_deliveries", "position_screening_records"} {
		var n int
		if err = db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 1 {
			t.Fatal("事务未回滚", table, n, err)
		}
	}
	exec("ALTER TABLE candidate_deletions DROP CONSTRAINT test_rollback")
	for i := 0; i < 2; i++ {
		if err = store.purgeCandidate(ctx, scope, item.ID, item.RowVersion); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"candidate_shares", "candidate_auto_reply_reports", "candidate_resume_vectors", "recruitment_applications", "recruitment_invitations", "recruitment_mail_deliveries", "position_screening_records"} {
		var n int
		if err = db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM resume_search_invalidations WHERE candidate_id=$1", item.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("搜索失效待办遗漏", n, err)
	}
	ev, err := store.candidateDeletions(ctx, scope.TenantID, scope.UserEmail, false, 0)
	if err != nil || len(ev) != 1 || len(ev[0].SourceHashes) != 1 || ev[0].SourceHashes[0] != purgeSourceHash("boss", "verified-source") || len(ev[0].RequestHashes) != 1 {
		t.Fatal(ev, err)
	}
	admin, err := store.candidateDeletions(ctx, scope.TenantID, "different-admin", true, 0)
	if err != nil || len(admin) != 1 || len(admin[0].RequestHashes) != 0 {
		t.Fatal(admin, err)
	}
	if _, err = store.SaveCandidateProfile(input); !errors.Is(err, errCandidateDeleted) {
		t.Fatal("旧来源可重建", err)
	}
	input.PlatformCandidateID = "never-linked"
	if _, err = store.SaveCandidateProfile(input); !errors.Is(err, errCandidateDeleted) {
		t.Fatal("旧手机号可重建", err)
	}
	if _, err = store.SaveCandidateProfile(otherInput); err != nil {
		t.Fatal("误挡其他人", err)
	}
	if _, err = store.GetPositionCandidate(scope.TenantID, other.ID, "", scope.UserEmail, false); err != nil {
		t.Fatal("删除同名他人", err)
	}
	snapshot.PlatformCandidateID = ""
	if _, err = screening.Save(ctx, screen, snapshot); !errors.Is(err, errCandidateDeleted) {
		t.Fatal("旧版筛选重放", err)
	}
	if _, _, err = NewPostgresRecruitmentMailStore(db).ClaimDelivery(RecruitmentMailDelivery{TenantID: scope.TenantID, ApplicationID: "app"}); !errors.Is(err, errCandidateDeleted) {
		t.Fatal("面试旧发送仍可建账", err)
	}
	hash := purgeSourceHash("boss", "verified-source")
	check := candidateDeletionCheck{CandidateIDs: []string{item.ID, other.ID}, SourceHashes: []string{hash}}
	verified, err := store.checkCandidateDeletions(ctx, scope, check)
	if err != nil || len(verified.CandidateIDs) != 1 || len(verified.SourceHashes) != 1 {
		t.Fatal("应且仅应确认旧身份", verified, err)
	}
	newItem, err := store.SaveLibraryCandidate(scope, "", map[string]any{"candidate_name": "合成同名人选", "phone": "13800138000", "platform_id": "boss", "platform_candidate_id": "verified-source"}, 0, "new-manual-confirmation")
	if err != nil || newItem.ID == item.ID {
		t.Fatal("人工明确重建失败", newItem.ID, err)
	}
	verified, err = store.checkCandidateDeletions(ctx, scope, check)
	if err != nil || len(verified.CandidateIDs) != 1 || len(verified.SourceHashes) != 0 {
		t.Fatal("不能按旧来源清理人工新建的简历", verified, err)
	}
	ev, err = store.candidateDeletions(ctx, scope.TenantID, scope.UserEmail, false, 0)
	if err != nil || len(ev) != 1 || len(ev[0].SourceHashes) != 0 {
		t.Fatal("离线设备迟到的事件会按来源误删新简历", ev, err)
	}

}
