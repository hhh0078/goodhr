// 本文件仅在显式指定本机测试数据库时验证真实 PostgreSQL 迁移、事务、权限与历史兼容，禁止连接生产库。
package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// candidatePostgresTestDB 创建独立测试schema，必须是回环地址且数据库名以_test结尾。
func candidatePostgresTestDB(t *testing.T, beforeNotes ...bool) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOODHR_CANDIDATE_TEST_DSN")
	if dsn == "" {
		t.Skip("未指定本机候选人测试数据库")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !(parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost") || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("候选人数据库集成测试仅允许回环地址的_test库")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	schema := fmt.Sprintf("candidate_library_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	query := parsed.Query()
	query.Set("options", "-csearch_path="+schema+",public")
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	setup := `CREATE EXTENSION IF NOT EXISTS pgcrypto; CREATE TABLE tenants(id UUID PRIMARY KEY DEFAULT gen_random_uuid()); CREATE TABLE users(id UUID PRIMARY KEY DEFAULT gen_random_uuid(),email TEXT UNIQUE NOT NULL,tenant_id UUID REFERENCES tenants(id)); CREATE TABLE positions(id UUID PRIMARY KEY DEFAULT gen_random_uuid(),name TEXT NOT NULL DEFAULT ''); CREATE TABLE task_runs(id UUID PRIMARY KEY DEFAULT gen_random_uuid()); CREATE TABLE cookie_data(id UUID PRIMARY KEY DEFAULT gen_random_uuid());`
	if _, err = db.Exec(setup); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0028_candidate_profile_engagement_events.sql", "0050_simplify_candidate_resume_schema.sql", "0084_candidate_library_upgrade.sql", "0086_candidate_notes.sql"} {
		if name == "0086_candidate_notes.sql" && len(beforeNotes) > 0 && beforeNotes[0] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	return db
}

// TestCandidatePostgresSourceFilter 验证真实SQL将历史空来源与 automatic 一并筛选，并保留成员可见范围。
func TestCandidatePostgresSourceFilter(t *testing.T) {
	db := candidatePostgresTestDB(t)
	var tenantID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	email := "source-owner@example.com"
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES($1,$2),($3,$2)`, email, tenantID, "source-member@example.com"); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresCandidateStore(db)
	for index, fields := range []string{`{}`, `{"source_type":""}`, `{"source_type":null}`, `{"source_type":"  "}`, `{"source_type":"automatic"}`, `{"source_type":"manual"}`, `{"source_type":"ai_import"}`, `{"source_type":"browser_extension"}`} {
		candidate, err := store.SaveCandidateProfile(CandidateProfileInput{UserEmail: email, PlatformID: "liepin", PlatformCandidateID: fmt.Sprintf("source-%d", index), CandidateName: "来源测试"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE candidate_profiles SET library_fields=$1::jsonb WHERE id=$2::uuid`, fields, candidate.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.SaveCandidateProfile(CandidateProfileInput{UserEmail: "source-member@example.com", PlatformID: "liepin", PlatformCandidateID: "other-member", CandidateName: "其他成员"}); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		source string
		count  int
	}{{"", 8}, {"automatic", 5}, {"manual", 1}, {"ai_import", 1}, {"browser_extension", 1}} {
		result, err := store.ListPositionCandidates(tenantID, PositionCandidateQuery{UserEmail: email, SourceType: item.source, PageSize: 100})
		if err != nil || result.Total != item.count || len(result.Items) != item.count {
			t.Fatalf("source=%s list=%+v err=%v", item.source, result, err)
		}
		for _, candidate := range result.Items {
			if item.source != "" && publicPositionCandidate(candidate)["source_type"] != item.source {
				t.Fatalf("source filter and response disagree: %+v", candidate)
			}
		}
	}
	page, err := store.ListPositionCandidates(tenantID, PositionCandidateQuery{UserEmail: email, SourceType: "automatic", PageSize: 2, Page: 2})
	if err != nil || page.Total != 5 || len(page.Items) != 2 {
		t.Fatalf("source pagination=%+v err=%v", page, err)
	}
}

// TestCandidatePostgresLifecycle 验证新增和旧采集真实SQL、成员计数、推荐删除快照与备注幂等。
func TestCandidatePostgresLifecycle(t *testing.T) {
	db := candidatePostgresTestDB(t)
	var tenantID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	scope := CandidateLibraryScope{TenantID: tenantID, UserEmail: "pg-owner@example.com", AuthorName: "测试操作人"}
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES($1,$2),($3,$2)`, scope.UserEmail, tenantID, "pg-member@example.com"); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresCandidateStore(db)
	fields, err := candidateMutationFields(map[string]any{"candidate_name": "PG人工姓名", "phone": "13800138000", "platform_id": "liepin", "platform_candidate_id": "pg-source", "expected_salary_min": 10.5, "expected_salary_max": 20.75, "work_experiences": []map[string]any{{"company_name": "人工公司"}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := sync.Map{}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := store.SaveLibraryCandidate(scope, "", fields, 0, "pg-create")
			if err != nil {
				t.Errorf("PG concurrent create: %v", err)
				return
			}
			ids.Store(item.ID, true)
		}()
	}
	wg.Wait()
	count := 0
	var id string
	ids.Range(func(key, value any) bool { count++; id = key.(string); return true })
	if count != 1 {
		t.Fatalf("created %d IDs", count)
	}
	item, err := store.GetPositionCandidate(tenantID, id, "", scope.UserEmail, false)
	if err != nil || item.ExpectedSalaryMin == nil || *item.ExpectedSalaryMin != 10.5 || len(item.WorkExperiences) != 1 || len(item.Sources) != 1 {
		t.Fatalf("loaded=%+v err=%v", item, err)
	}
	member := CandidateLibraryScope{TenantID: tenantID, UserEmail: "pg-member@example.com"}
	matches, hidden, err := store.FindCandidateDuplicates(member, CandidateDuplicateQuery{Phone: "13800138000"})
	if err != nil || len(matches) != 0 || !hidden {
		t.Fatalf("hidden=%v matches=%v err=%v", hidden, matches, err)
	}
	list, err := store.ListPositionCandidates(tenantID, PositionCandidateQuery{UserEmail: scope.UserEmail, Keyword: "人工公司"})
	if err != nil || list.Total != 1 {
		t.Fatalf("member count/list=%+v err=%v", list, err)
	}
	updated, err := store.SaveLibraryCandidate(scope, id, map[string]any{"personal_description": "人工确认优势", "platform_id": "zhaopin", "platform_candidate_id": "pg-second-source"}, item.RowVersion, "pg-update")
	if err != nil || len(updated.Sources) != 2 {
		t.Fatalf("update=%+v err=%v", updated, err)
	}
	if _, err = store.SaveLibraryCandidate(scope, id, map[string]any{"personal_description": "旧内容"}, item.RowVersion, "pg-stale"); !errors.Is(err, ErrCandidateVersion) {
		t.Fatalf("stale=%v", err)
	}
	automatic, err := store.SaveCandidateProfile(CandidateProfileInput{UserEmail: scope.UserEmail, PlatformID: "zhaopin", PlatformCandidateID: "pg-second-source", CandidateName: "自动改名", Phone: "", PersonalDescription: "自动优势", WorkExperiences: []CandidateWorkExperience{}})
	if err != nil || automatic.ID != id || automatic.CandidateName != "PG人工姓名" || automatic.Phone != "13800138000" || automatic.PersonalDescription != "人工确认优势" || len(automatic.WorkExperiences) != 1 {
		t.Fatalf("automatic=%+v err=%v", automatic, err)
	}
	legacy, err := store.SaveCandidateProfile(CandidateProfileInput{UserEmail: scope.UserEmail, PlatformID: "hliepin", PlatformCandidateID: "no-phone", CandidateName: "无手机号历史"})
	if err != nil || legacy.Phone != "" {
		t.Fatalf("legacy=%+v err=%v", legacy, err)
	}
	note := CandidateEvent{CandidateID: id, EventType: "manual_note", MessageText: "第一行\n第二行", Metadata: map[string]any{"author_email": scope.UserEmail, "author_name": scope.AuthorName, "idempotency_key": "pg-note"}}
	first, err := store.SaveCandidateEvent(note)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := store.SaveCandidateEvent(note)
	if err != nil || retry.ID != first.ID {
		t.Fatalf("note retry=%+v err=%v", retry, err)
	}
	notes, err := store.ListCandidateNotes(tenantID, id)
	if err != nil || len(notes) != 1 || notes[0].AuthorName != scope.AuthorName {
		t.Fatalf("notes=%v err=%v", notes, err)
	}
	var positionID string
	if err = db.QueryRow(`INSERT INTO positions(name) VALUES('岗位快照') RETURNING id::text`).Scan(&positionID); err != nil {
		t.Fatal(err)
	}
	position := Position{ID: positionID, Name: "岗位快照"}
	rec, err := store.SaveCandidateRecommendation(scope, id, position, "recommended", "第一次推荐", "pg-rec")
	if err != nil {
		t.Fatal(err)
	}
	retryRec, err := store.SaveCandidateRecommendation(scope, id, position, "recommended", "第一次推荐", "pg-rec")
	if err != nil || retryRec.ID != rec.ID || len(retryRec.Events) != 1 {
		t.Fatalf("rec retry=%+v err=%v", retryRec, err)
	}
	list, err = store.ListPositionCandidates(tenantID, PositionCandidateQuery{UserEmail: scope.UserEmail, PositionID: positionID})
	if err != nil || list.Total != 1 {
		t.Fatalf("position list=%+v err=%v", list, err)
	}
	if _, err = db.Exec(`DELETE FROM positions WHERE id=$1`, positionID); err != nil {
		t.Fatal(err)
	}
	history, err := store.ListCandidateRecommendations(scope, id)
	if err != nil || len(history) != 1 || history[0].PositionName != "岗位快照" || history[0].PositionID != positionID {
		t.Fatalf("deleted position snapshot=%+v err=%v", history, err)
	}
	// 历史不同来源的同手机号保持独立，手动新增则提示冲突。
	for _, platform := range []string{"legacy-a", "legacy-b"} {
		if _, err = store.SaveCandidateProfile(CandidateProfileInput{UserEmail: scope.UserEmail, PlatformID: platform, PlatformCandidateID: "same-phone", CandidateName: platform, Phone: "13900139000"}); err != nil {
			t.Fatal(err)
		}
	}
	historical, _, err := store.FindCandidateDuplicates(scope, CandidateDuplicateQuery{Phone: "13900139000"})
	if err != nil || len(historical) != 2 {
		t.Fatalf("historical=%+v err=%v", historical, err)
	}
	newFields, _ := candidateMutationFields(map[string]any{"candidate_name": "重复新增", "phone": "13900139000"}, true)
	if _, err = store.SaveLibraryCandidate(scope, "", newFields, 0, "pg-duplicate"); !errors.Is(err, ErrCandidateDuplicate) {
		t.Fatalf("duplicate create err=%v", err)
	}
}
