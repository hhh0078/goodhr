// 本文件仅在显式本机_test数据库隔离schema中验证招聘迁移、持久化、行锁和邀请版本，禁止连接生产。
package httpapi

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// recruitmentPostgresFixture 复用已有回环地址和_test库护栏，并在每个测试的独立schema执行招聘迁移。
func recruitmentPostgresFixture(t *testing.T) (*sql.DB, *PostgresRecruitmentStore, string, string) {
	t.Helper()
	db := candidatePostgresTestDB(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0090_recruitment_workflows.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatalf("recruitment migration: %v", err)
		}
	}
	var tenantID, candidateID string
	if err = db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO candidate_profiles(tenant_id,candidate_name) VALUES($1,'PG测试同学') RETURNING id::text`, tenantID).Scan(&candidateID); err != nil {
		t.Fatal(err)
	}
	return db, NewPostgresRecruitmentStore(db), tenantID, candidateID
}

// recruitmentPostgresApplication 创建满足外键的最小独立流程快照。
func recruitmentPostgresApplication(tenantID, candidateID string, workflow RecruitmentWorkflow) RecruitmentApplication {
	now := time.Now().UTC()
	nodes := []RecruitmentNodeInstance{}
	for i, node := range workflow.Nodes {
		status := "pending"
		if i == 0 {
			status = "current"
		}
		nodes = append(nodes, RecruitmentNodeInstance{RecruitmentNode: node, Status: status, Attempt: 1, Approvals: []RecruitmentDecision{}})
	}
	return RecruitmentApplication{ID: recruitmentID(), TenantID: tenantID, CandidateID: candidateID, CandidateName: "PG测试同学", CandidateEmail: "candidate@example.com", PositionID: "position", PositionName: "PG测试岗位", OwnerEmail: "owner@example.com", WorkflowID: workflow.ID, WorkflowName: workflow.Name, WorkflowVersion: workflow.Version, Status: "active", Stage: "contacting", Revision: 1, Nodes: nodes, Interviews: []RecruitmentInterview{}, Events: []RecruitmentEvent{}, CreatedAt: now, UpdatedAt: now}
}

// TestRecruitmentPostgresDefaultSnapshotAndComments 验证迁移中文备注、默认幂等、模板版本和实例快照持久不漂移。
func TestRecruitmentPostgresDefaultSnapshotAndComments(t *testing.T) {
	db, store, tenantID, candidateID := recruitmentPostgresFixture(t)
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := store.EnsureDefault(tenantID, defaultRecruitmentWorkflow(tenantID)); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	items, err := store.ListWorkflows(tenantID)
	if err != nil || len(items) != 1 {
		t.Fatalf("templates %+v err=%v", items, err)
	}
	workflow := items[0]
	app, err := store.CreateApplication(recruitmentPostgresApplication(tenantID, candidateID, workflow))
	if err != nil {
		t.Fatal(err)
	}
	workflow.Name = "新版本"
	workflow.Nodes[0].Name = "新节点"
	workflow, err = store.SaveWorkflow(tenantID, workflow, workflow.Revision)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetApplication(tenantID, app.ID)
	if err != nil || loaded.WorkflowVersion != 1 || loaded.Nodes[0].Name == "新节点" {
		t.Fatalf("snapshot %+v err=%v", loaded, err)
	}
	if _, err = store.SaveWorkflow(tenantID, workflow, 1); !errors.Is(err, ErrRecruitmentVersion) {
		t.Fatalf("stale template=%v", err)
	}
	var missing int
	err = db.QueryRow(`SELECT count(*) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname IN ('recruitment_workflows','recruitment_applications','recruitment_invitations') AND a.attnum>0 AND NOT a.attisdropped AND col_description(c.oid,a.attnum) IS NULL`).Scan(&missing)
	if err != nil || missing != 0 {
		t.Fatalf("missing comments=%d err=%v", missing, err)
	}
	if err = store.DeleteWorkflow(tenantID, workflow.ID, workflow.Revision); !errors.Is(err, ErrRecruitmentInvalid) {
		t.Fatalf("default delete=%v", err)
	}
	other := defaultRecruitmentWorkflow(tenantID)
	other.IsDefault = false
	other, err = store.SaveWorkflow(tenantID, other, 0)
	if err != nil {
		t.Fatal(err)
	}
	other.IsDefault = true
	other, err = store.SaveWorkflow(tenantID, other, other.Revision)
	if err != nil {
		t.Fatal(err)
	}
	items, err = store.ListWorkflows(tenantID)
	defaults := 0
	for _, item := range items {
		if item.IsDefault {
			defaults++
		}
	}
	if err != nil || defaults != 1 {
		t.Fatalf("default switch defaults=%d err=%v", defaults, err)
	}
}

// TestRecruitmentPostgresAtomicRevisionAndInvitation 验证真实行锁只允许一个并发动作、隔离团队、结束后再次启动及旧邀请失效。
func TestRecruitmentPostgresAtomicRevisionAndInvitation(t *testing.T) {
	_, store, tenantID, candidateID := recruitmentPostgresFixture(t)
	workflow := defaultRecruitmentWorkflow(tenantID)
	if err := store.EnsureDefault(tenantID, workflow); err != nil {
		t.Fatal(err)
	}
	app, err := store.CreateApplication(recruitmentPostgresApplication(tenantID, candidateID, workflow))
	if err != nil {
		t.Fatal(err)
	}
	duplicate := app
	duplicate.ID = recruitmentID()
	if _, err = store.CreateApplication(duplicate); !errors.Is(err, ErrRecruitmentDuplicate) {
		t.Fatalf("duplicate=%v", err)
	}
	if _, err = store.GetApplication("00000000-0000-4000-8000-000000000099", app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross team=%v", err)
	}
	var wait sync.WaitGroup
	var mutex sync.Mutex
	success := 0
	conflict := 0
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := store.MutateApplication(tenantID, app.ID, 1, func(item *RecruitmentApplication) error {
				recruitmentAppendEvent(item, "owner@example.com", "concurrent", "一次动作", "")
				return nil
			})
			mutex.Lock()
			defer mutex.Unlock()
			if err == nil {
				success++
			} else if errors.Is(err, ErrRecruitmentVersion) {
				conflict++
			} else {
				t.Errorf("concurrent err=%v", err)
			}
		}()
	}
	wait.Wait()
	if success != 1 || conflict != 7 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	app, _ = store.GetApplication(tenantID, app.ID)
	if len(app.Events) != 1 {
		t.Fatal("multiple persisted concurrent events")
	}
	app, err = store.MutateApplication(tenantID, app.ID, app.Revision, func(item *RecruitmentApplication) error {
		item.Interviews = append(item.Interviews, RecruitmentInterview{ID: "interview", NodeID: workflow.Nodes[1].ID, Attempt: 1, StartsAt: time.Now().Add(time.Hour), DurationMinutes: 30, Status: "scheduled", Confirmation: "pending", Revision: 1})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	inv := RecruitmentInvitation{TokenHash: "hash-only", TenantID: tenantID, ApplicationID: app.ID, InterviewID: "interview", InterviewRevision: 1, ExpiresAt: time.Now().Add(time.Hour)}
	if err = store.SaveInvitation(inv); err != nil {
		t.Fatal(err)
	}
	persisted, err := store.FindInvitation(inv.TokenHash)
	if err != nil || persisted.InterviewRevision != 1 {
		t.Fatalf("inv %+v err=%v", persisted, err)
	}
	app, err = store.MutateApplication(tenantID, app.ID, app.Revision, func(item *RecruitmentApplication) error { item.Interviews[0].Revision++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	inv.TokenHash = "another-hash"
	if err = store.SaveInvitation(inv); !errors.Is(err, ErrRecruitmentVersion) {
		t.Fatalf("outdated invitation=%v", err)
	}
	app, err = store.MutateApplication(tenantID, app.ID, app.Revision, func(item *RecruitmentApplication) error { item.Status = "withdrawn"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	duplicate.ID = recruitmentID()
	if _, err = store.CreateApplication(duplicate); err != nil {
		t.Fatalf("new application after closed=%v", err)
	}
}

// TestRecruitmentPostgresCandidateDeletionPreservesHistory 验证原简历删除行为不被新增外键阻塞，招聘历史独立保留。
func TestRecruitmentPostgresCandidateDeletionPreservesHistory(t *testing.T) {
	db, store, tenantID, candidateID := recruitmentPostgresFixture(t)
	workflow := defaultRecruitmentWorkflow(tenantID)
	app, err := store.CreateApplication(recruitmentPostgresApplication(tenantID, candidateID, workflow))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DELETE FROM candidate_profiles WHERE id=$1`, candidateID); err != nil {
		t.Fatalf("original candidate deletion blocked: %v", err)
	}
	read, err := store.GetApplication(tenantID, app.ID)
	if err != nil || read.CandidateID != candidateID || read.CandidateName != "PG测试同学" {
		t.Fatalf("history lost %+v err=%v", read, err)
	}
}
