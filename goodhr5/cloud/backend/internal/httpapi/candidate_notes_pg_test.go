// 本文件仅使用隔离的本机测试库，验证旧人工备注升级不丢数据、独立备注事务以及推荐发生时间兼容。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestCandidatePostgresNotesMigration 验证旧备注保留编号正文时间、重复迁移、重复历史键和推荐事件回填。
func TestCandidatePostgresNotesMigration(t *testing.T) {
	db := candidatePostgresTestDB(t, true)
	var tenantID, userID, candidateID, positionID, recommendationID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	scope := CandidateLibraryScope{TenantID: tenantID, UserEmail: "migration-owner@example.com", AuthorName: "迁移作者"}
	if err := db.QueryRow(`INSERT INTO users(email,tenant_id) VALUES($1,$2) RETURNING id::text`, scope.UserEmail, tenantID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES('migration-member@example.com',$1)`, tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO candidate_profiles(tenant_id,created_by_user_id,source_platform_id,source_platform_candidate_id,candidate_name) VALUES($1,$2,'liepin','old-migration','旧备注简历') RETURNING id::text`, tenantID, userID).Scan(&candidateID); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	legacyTimes := []time.Time{base.Add(-48 * time.Hour), base.Add(-24 * time.Hour)}
	legacyIDs := []string{}
	for index, created := range legacyTimes {
		metadata, _ := json.Marshal(map[string]any{"author_email": scope.UserEmail, "author_name": scope.AuthorName, "idempotency_key": "legacy-one"})
		var id string
		if err := db.QueryRow(`INSERT INTO candidate_events(tenant_id,candidate_id,event_type,message_text,metadata,created_at) VALUES($1,$2,'manual_note',$3,$4::jsonb,$5) RETURNING id::text`, tenantID, candidateID, fmt.Sprintf("历史第%d条\n保留换行", index+1), string(metadata), created).Scan(&id); err != nil {
			t.Fatal(err)
		}
		legacyIDs = append(legacyIDs, id)
	}
	if _, err := db.Exec(`INSERT INTO candidate_events(tenant_id,candidate_id,event_type,message_text) VALUES($1,$2,'detail_analysis','自动分析')`, tenantID, candidateID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO positions(name) VALUES('旧推荐岗位') RETURNING id::text`).Scan(&positionID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO candidate_recommendations(tenant_id,candidate_id,position_id,position_key,position_name,stage,author_email) VALUES($1,$2,$3::uuid,$3::text,'旧推荐岗位','contacting',$4) RETURNING id::text`, tenantID, candidateID, positionID, scope.UserEmail).Scan(&recommendationID); err != nil {
		t.Fatal(err)
	}
	for index, created := range legacyTimes {
		if _, err := db.Exec(`INSERT INTO candidate_recommendation_events(recommendation_id,to_stage,content,author_email,created_at) VALUES($1,'contacting',$2,$3,$4)`, recommendationID, fmt.Sprintf("旧推荐%d", index), scope.UserEmail, created); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0086_candidate_notes.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatalf("migration run=%d err=%v", index, err)
		}
	}
	store := NewPostgresCandidateStore(db)
	notes, err := store.ListCandidateNotes(tenantID, candidateID)
	if err != nil || len(notes) != 2 {
		t.Fatalf("migrated notes=%+v err=%v", notes, err)
	}
	for index, id := range legacyIDs {
		var content, author, name, key string
		var created, occurred time.Time
		if err := db.QueryRow(`SELECT content,author_email,author_name,idempotency_key,created_at,occurred_at FROM candidate_notes WHERE id=$1`, id).Scan(&content, &author, &name, &key, &created, &occurred); err != nil {
			t.Fatal(err)
		}
		if content != fmt.Sprintf("历史第%d条\n保留换行", index+1) || author != scope.UserEmail || name != scope.AuthorName || !created.Equal(legacyTimes[index]) || !occurred.Equal(created) {
			t.Fatalf("legacy changed id=%s content=%q time=%s/%s author=%s/%s", id, content, created, occurred, author, name)
		}
		if index == 0 && key != "legacy-one" || index == 1 && key != "migrated:"+id {
			t.Fatalf("legacy request key=%s", key)
		}
	}
	retry, err := store.SaveCandidateNote(scope, CandidateNote{CandidateID: candidateID, Content: "历史第1条\n保留换行", OccurredAt: legacyTimes[0], IdempotencyKey: "legacy-one"})
	if err != nil || retry.ID != legacyIDs[0] {
		t.Fatalf("legacy retry=%+v err=%v", retry, err)
	}
	if _, err = store.SaveCandidateNote(scope, CandidateNote{CandidateID: candidateID, Content: "覆盖历史", IdempotencyKey: "legacy-one"}); !errors.Is(err, ErrCandidateRequest) {
		t.Fatalf("legacy conflict=%v", err)
	}
	var migratedMismatch int
	if err = db.QueryRow(`SELECT count(*) FROM candidate_recommendation_events WHERE occurred_at<>created_at`).Scan(&migratedMismatch); err != nil || migratedMismatch != 0 {
		t.Fatalf("event backfill mismatch=%d err=%v", migratedMismatch, err)
	}
	var missingComments int
	if err = db.QueryRow(`SELECT count(*) FROM pg_attribute WHERE attrelid='candidate_notes'::regclass AND attnum>0 AND NOT attisdropped AND COALESCE(col_description(attrelid,attnum),'')=''`).Scan(&missingComments); err != nil || missingComments != 0 {
		t.Fatalf("missing Chinese comments=%d err=%v", missingComments, err)
	}
	var wg sync.WaitGroup
	newIDs := sync.Map{}
	for index := 0; index < 5; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			note, err := store.SaveCandidateNote(scope, CandidateNote{CandidateID: candidateID, Content: "新独立备注", IdempotencyKey: "concurrent-note"})
			if err != nil {
				t.Errorf("concurrent note=%v", err)
				return
			}
			newIDs.Store(note.ID, true)
		}()
	}
	wg.Wait()
	newCount := 0
	newID := ""
	newIDs.Range(func(key, value any) bool { newCount++; newID = key.(string); return true })
	if newCount != 1 {
		t.Fatalf("note concurrency created=%d", newCount)
	}
	for _, forbidden := range []CandidateLibraryScope{{TenantID: tenantID, UserEmail: "migration-member@example.com"}, {TenantID: "00000000-0000-0000-0000-000000000099", UserEmail: scope.UserEmail, IsAdmin: true}} {
		if _, err = store.SaveCandidateNote(forbidden, CandidateNote{CandidateID: candidateID, Content: "越权"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unauthorized note err=%v", err)
		}
	}
	item, err := store.GetPositionCandidate(tenantID, candidateID, "", scope.UserEmail, false)
	if err != nil || len(item.Notes) != 2 || item.Notes[0].ID != newID || len(item.Events) != 1 || item.Events[0].EventType != "detail_analysis" {
		t.Fatalf("detail note/event separation=%+v err=%v", item, err)
	}
	list, err := store.ListPositionCandidates(tenantID, PositionCandidateQuery{UserEmail: scope.UserEmail})
	if err != nil || len(list.Items) != 1 || list.Items[0].Notes[0].ID != newID {
		t.Fatalf("list latest dedicated note=%+v err=%v", list, err)
	}
	position := Position{ID: positionID, Name: "旧推荐岗位"}
	backdated := base.Add(-72 * time.Hour)
	rec, err := store.SaveCandidateRecommendation(scope, candidateID, position, "interview", "迟录的沟通", "backdated-rec", backdated)
	if err != nil || len(rec.Events) != 3 || !rec.Events[0].OccurredAt.Equal(legacyTimes[1]) || !rec.Events[2].OccurredAt.Equal(backdated) {
		t.Fatalf("rec occurrence order=%+v err=%v", rec, err)
	}
	rec, err = store.SaveCandidateRecommendation(scope, candidateID, position, "interview", "迟录的沟通", "backdated-rec", backdated)
	if err != nil || len(rec.Events) != 3 {
		t.Fatalf("rec time retry=%+v err=%v", rec, err)
	}
	if _, err = store.SaveCandidateRecommendation(scope, candidateID, position, "interview", "迟录的沟通", "backdated-rec", base); !errors.Is(err, ErrCandidateRequest) {
		t.Fatalf("rec changed time conflict=%v", err)
	}
	var noteCount, automaticCount int
	if err = db.QueryRow(`SELECT count(*) FROM candidate_notes WHERE candidate_id=$1`, candidateID).Scan(&noteCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM candidate_events WHERE candidate_id=$1`, candidateID).Scan(&automaticCount); err != nil || noteCount != 3 || automaticCount != 3 {
		t.Fatalf("dedicated count=%d old events=%d err=%v", noteCount, automaticCount, err)
	}
}

// TestCandidatePostgresStructuredDuplicates 验证真实参数化SQL与内存使用相同的手机号分流、最早经历、年龄、分组和隐藏规则。
func TestCandidatePostgresStructuredDuplicates(t *testing.T) {
	db := candidatePostgresTestDB(t)
	var tenantID, otherTenantID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&otherTenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES('owner@example.com',$1),('member@example.com',$1),('foreign@example.com',$2)`, tenantID, otherTenantID); err != nil {
		t.Fatal(err)
	}
	scope := CandidateLibraryScope{TenantID: tenantID, UserEmail: "owner@example.com"}
	query, fixtures := candidateDuplicateFixtures()
	store := NewPostgresCandidateStore(db)
	ids := map[string]string{}
	for _, fixture := range fixtures {
		tenant, email := tenantID, fixture.UserEmail
		if fixture.TenantID == "other" {
			tenant, email = otherTenantID, "foreign@example.com"
		}
		fields, _ := json.Marshal(fixture.LibraryFields)
		work, _ := json.Marshal(fixture.WorkExperiences)
		schools, _ := json.Marshal(fixture.Educations)
		var id string
		if err := db.QueryRow(`INSERT INTO candidate_profiles(tenant_id,created_by_user_id,source_platform_id,source_platform_candidate_id,candidate_name,birth_ym,library_fields,work_experiences,educations) VALUES($1,(SELECT id FROM users WHERE email=$2),'liepin',$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb) RETURNING id::text`, tenant, email, fixture.ID, fixture.CandidateName, fixture.BirthYM, string(fields), string(work), string(schools)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[fixture.ID] = id
	}
	assertMatch := func(expected []string, group string) {
		t.Helper()
		matches, hidden, err := store.FindCandidateDuplicates(scope, query)
		actual := []CandidateDuplicateMatch{}
		for _, name := range expected {
			actual = append(actual, CandidateDuplicateMatch{Candidate: PositionCandidate{ID: ids[name]}})
		}
		if err != nil || hidden || candidateDuplicateIDs(matches) != candidateDuplicateIDs(actual) {
			t.Fatalf("SQL matches=%+v expected=%v hidden=%v err=%v", matches, expected, hidden, err)
		}
		for _, match := range matches {
			if group != "" && fmt.Sprint(match.GroupKeys) != "["+group+"]" {
				t.Fatalf("SQL group=%+v", match)
			}
		}
	}
	assertMatch([]string{"strict", "birth", "unknown-gender"}, "strict")
	query.Keyword = "最早大学"
	assertMatch([]string{"strict", "birth", "unknown-gender"}, "strict")
	query.Keyword = "不存在的职位"
	assertMatch(nil, "")
	query.PlatformID, query.PlatformCandidateID = "liepin", "strict"
	assertMatch([]string{"strict"}, "")
	query.PlatformID, query.PlatformCandidateID, query.Keyword = "", "", ""
	query.WorkExperiences[1].CompanyName = "不存在的公司"
	assertMatch([]string{"strict", "birth", "unknown-gender"}, "surname_gender_school")
	query.WorkExperiences[1].CompanyName = "第一公司"
	query.Educations[1].SchoolName = "不存在的学校"
	assertMatch([]string{"strict", "birth", "unknown-gender", "wrong-year"}, "surname_gender_company")
	query.PlatformID, query.PlatformCandidateID = "liepin", "hidden"
	query.Keyword = "不存在的职位"
	matches, hidden, err := store.FindCandidateDuplicates(scope, query)
	if err != nil || len(matches) != 0 || !hidden {
		t.Fatalf("SQL hidden exact=%+v hidden=%v err=%v", matches, hidden, err)
	}
	if _, err = db.Exec(`UPDATE candidate_profiles SET phone=CASE source_platform_candidate_id WHEN 'strict' THEN '13800138000' WHEN 'birth' THEN '13900139000' ELSE phone END, normalized_phone=CASE source_platform_candidate_id WHEN 'strict' THEN '13800138000' WHEN 'birth' THEN '13900139000' ELSE normalized_phone END WHERE source_platform_candidate_id IN ('strict','birth')`); err != nil {
		t.Fatal(err)
	}
	query = CandidateDuplicateQuery{Phone: "+86 13800138000", Name: "张测试", Keyword: "最早大学"}
	matches, hidden, err = store.FindCandidateDuplicates(scope, query)
	if err != nil || hidden || len(matches) != 1 || matches[0].Candidate.ID != ids["strict"] || matches[0].MatchType != "exact" {
		t.Fatalf("SQL phone exact mixed fuzzy results=%+v hidden=%v err=%v", matches, hidden, err)
	}
	query, _ = candidateDuplicateFixtures()
	query.PlatformID, query.PlatformCandidateID, query.Keyword = "", "", ""
	query.Age = 0
	query.BirthYM = fmt.Sprintf("%d-01", time.Now().Year()-34)
	// 出生年月替代年龄后仍保留正确学校条件，必须优先严格组；显式缺失学校时才降级到公司组。
	assertMatch([]string{"strict", "birth", "unknown-gender"}, "strict")
	query.Educations[1].SchoolName = "不存在的学校"
	assertMatch([]string{"strict", "birth", "unknown-gender", "wrong-year"}, "surname_gender_company")
}
