// 本文件验证个人收藏夹隔离、团队权限、候选人数和删除收藏夹不删除简历等真实路由边界。
package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCandidateManagedFoldersHTTP 验证多人团队的真实收藏夹维护、筛选和受限插件入口。
func TestCandidateManagedFoldersHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	owner, member := "tag-owner@example.com", "tag-member@example.com"
	ownerToken := loginForTest(t, routes, owner)
	memberToken := loginForTest(t, routes, member)
	outsiderToken := loginForTest(t, routes, "tag-outsider@example.com")
	tenant, err := server.tenants.store.GetOrCreateTenant(owner)
	if err != nil {
		t.Fatal(err)
	}
	invite, _, err := server.tenants.store.InviteMember(tenant.ID, member, "member", owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.tenants.store.AcceptInvitation(invite.ID, member); err != nil {
		t.Fatal(err)
	}
	// 创建同名个人和团队收藏夹合法，但同一范围重复名称拒绝。
	status, teamResult := candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates/folders", map[string]any{"name": "重点人选", "scope": "team"})
	if status != 200 {
		t.Fatalf("team tag=%d %v", status, teamResult)
	}
	teamID := teamResult["folder"].(map[string]any)["id"].(string)
	status, personalResult := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/candidates/folders", map[string]any{"name": "私有秘密", "scope": "personal"})
	if status != 200 {
		t.Fatal(personalResult)
	}
	personalID := personalResult["folder"].(map[string]any)["id"].(string)
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates/folders", map[string]any{"name": "重点人选", "scope": "personal"})
	if status != 200 {
		t.Fatal("same name across scopes rejected")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates/folders", map[string]any{"name": "重点人选", "scope": "team"})
	if status != 409 {
		t.Fatalf("duplicate status=%d", status)
	}
	status, memberCandidate := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "收藏夹成员测试", "phone": "13800138901", "folder_ids": []string{personalID, teamID, teamID}})
	if status != 200 {
		t.Fatal(memberCandidate)
	}
	memberItem := memberCandidate["candidate"].(map[string]any)
	candidateID := memberItem["id"].(string)
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "收藏夹管理员测试", "phone": "13800138902", "folder_ids": []string{teamID}})
	if status != 200 {
		t.Fatal("owner candidate rejected")
	}
	for _, check := range []struct {
		token    string
		expected int
	}{{ownerToken, 2}, {memberToken, 1}} {
		status, listed := candidateTestRequest(t, routes, check.token, http.MethodGet, "/api/candidates/folders", nil)
		if status != 200 {
			t.Fatal(listed)
		}
		for _, raw := range listed["folders"].([]any) {
			tag := raw.(map[string]any)
			if tag["id"] == teamID && tag["candidate_count"] != float64(check.expected) {
				t.Fatalf("visible count=%v", tag)
			}
			if check.token == ownerToken && tag["id"] == personalID {
				t.Fatal("admin saw member personal tag")
			}
		}
	}
	status, detail := candidateTestRequest(t, routes, ownerToken, http.MethodGet, "/api/candidates/"+candidateID, nil)
	if status != 200 || strings.Contains(string(toJSONB(detail)), "私有秘密") || len(detail["candidate"].(map[string]any)["folder_ids"].([]any)) != 1 {
		t.Fatalf("personal tag leaked: %v", detail)
	}
	status, filtered := candidateTestRequest(t, routes, memberToken, http.MethodGet, "/api/candidates?folder_id="+teamID, nil)
	if status != 200 || filtered["real_total"] != 1.0 || filtered["builtin_total"] != 0.0 {
		t.Fatalf("tag filter=%v", filtered)
	}
	status, filtered = candidateTestRequest(t, routes, ownerToken, http.MethodGet, "/api/candidates?folder_id="+personalID, nil)
	if status != 200 || filtered["real_total"] != 0.0 {
		t.Fatal("admin filtered on private tag")
	}
	status, outsiders := candidateTestRequest(t, routes, outsiderToken, http.MethodGet, "/api/candidates/folders", nil)
	if status != 200 || len(outsiders["folders"].([]any)) != 0 {
		t.Fatal("tags crossed team")
	}
	status, _ = candidateTestRequest(t, routes, memberToken, http.MethodPatch, "/api/candidates/folders/"+teamID, map[string]any{"name": "越权修改", "scope": "team"})
	if status != 403 {
		t.Fatalf("member edit status=%d", status)
	}
	status, memberTeam := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/candidates/folders", map[string]any{"name": "成员团队收藏夹", "scope": "team"})
	if status != 200 {
		t.Fatal(memberTeam)
	}
	memberTeamID := memberTeam["folder"].(map[string]any)["id"].(string)
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPatch, "/api/candidates/folders/"+memberTeamID, map[string]any{"name": "管理员改名", "scope": "team"})
	if status != 200 {
		t.Fatal("administrator cannot rename team tag")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPatch, "/api/candidates/folders/"+memberTeamID, map[string]any{"name": "错误转个人", "scope": "personal"})
	if status != 403 {
		t.Fatal("administrator moved another member tag to personal")
	}
	status, _ = candidateTestRequest(t, routes, memberToken, http.MethodPatch, "/api/candidates/folders/"+memberTeamID, map[string]any{"name": "本人转个人", "scope": "personal"})
	if status != 200 {
		t.Fatal("creator cannot change own scope")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodDelete, "/api/candidates/folders/"+personalID, nil)
	if status != 404 {
		t.Fatalf("admin delete private status=%d", status)
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPatch, "/api/candidates/"+candidateID, map[string]any{"row_version": memberItem["row_version"], "folder_ids": []string{personalID}})
	if status != 400 {
		t.Fatalf("invisible tag accepted status=%d", status)
	}
	status, cleared := candidateTestRequest(t, routes, ownerToken, http.MethodPatch, "/api/candidates/"+candidateID, map[string]any{"row_version": memberItem["row_version"], "folder_ids": []string{}})
	if status != 200 || len(cleared["candidate"].(map[string]any)["folder_ids"].([]any)) != 0 {
		t.Fatal(cleared)
	}
	status, ownDetail := candidateTestRequest(t, routes, memberToken, http.MethodGet, "/api/candidates/"+candidateID, nil)
	if status != 200 || len(ownDetail["candidate"].(map[string]any)["folder_ids"].([]any)) != 1 {
		t.Fatal("clearing visible tags erased another member private relation")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "无效收藏夹", "phone": "13800138903", "folder_ids": []string{"unknown-tag"}})
	if status != 400 {
		t.Fatal("unknown tag accepted")
	}
	status, before := candidateTestRequest(t, routes, ownerToken, http.MethodGet, "/api/candidates", nil)
	if status != 200 || before["real_total"] != 2.0 {
		t.Fatal("failed tagging created candidate")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodDelete, "/api/candidates/folders/"+teamID, nil)
	if status != 200 {
		t.Fatal("tag delete failed")
	}
	_, after := candidateTestRequest(t, routes, ownerToken, http.MethodGet, "/api/candidates", nil)
	if after["real_total"] != before["real_total"] {
		t.Fatal("tag delete deleted resume")
	}
	status, exchange := candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/auth/extension-session", map[string]any{})
	if status != 200 {
		t.Fatal(exchange)
	}
	plugin := exchange["token"].(string)
	status, _ = candidateTestRequest(t, routes, plugin, http.MethodGet, "/api/candidates/folders", nil)
	if status != 200 {
		t.Fatal("plugin cannot list existing tags")
	}
	status, _ = candidateTestRequest(t, routes, plugin, http.MethodPost, "/api/candidates/folders", map[string]any{"name": "插件自由新增", "scope": "personal"})
	if status != 403 {
		t.Fatal("plugin can manage tags")
	}
	status, legacy := candidateTestRequest(t, routes, plugin, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "旧版插件原始技能", "phone": "13800138904", "tags": []string{"平台原始技能"}})
	if status != 200 || len(legacy["candidate"].(map[string]any)["folder_ids"].([]any)) != 0 || len(legacy["candidate"].(map[string]any)["tags"].([]any)) != 1 {
		t.Fatal("legacy plugin tags were blocked or converted into folders", legacy)
	}
}

// TestCandidateFolderRawTagIndependence 验证标签与收藏夹的保存、改名、清空、删除及自动入库始终互不影响。
func TestCandidateFolderRawTagIndependence(t *testing.T) {
	store := NewMemoryCandidateStore()
	scope := CandidateLibraryScope{TenantID: "team", UserEmail: "owner@example.com", IsAdmin: true}
	folder, err := store.SaveCandidateFolder(scope, "", "重点人选", "personal")
	if err != nil {
		t.Fatal(err)
	}
	fields, err := candidateMutationFields(map[string]any{"candidate_name": "独立字段测试", "phone": "13800138910", "tags": []string{"Go", "沟通能力"}, "folder_ids": []string{folder.ID}}, true)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.SaveLibraryCandidate(scope, "", fields, 0, "")
	if err != nil || len(item.FolderIDs) != 1 || string(toJSONB(item.LibraryFields["tags"])) != `["Go","沟通能力"]` {
		t.Fatal("raw tags replaced on create", item, err)
	}
	if _, err = store.SaveCandidateFolder(scope, folder.ID, "已改名收藏夹", "personal"); err != nil {
		t.Fatal(err)
	}
	item, err = store.GetPositionCandidate(scope.TenantID, item.ID, "", scope.UserEmail, true)
	if err != nil || item.FolderDetails[0].Name != "已改名收藏夹" || string(toJSONB(item.LibraryFields["tags"])) != `["Go","沟通能力"]` {
		t.Fatal("folder rename changed raw tags", item, err)
	}
	item, err = store.SaveLibraryCandidate(scope, item.ID, map[string]any{"tags": []string{}}, item.RowVersion, "")
	if err != nil || len(item.FolderIDs) != 1 || string(toJSONB(item.LibraryFields["tags"])) != `[]` {
		t.Fatal("raw tags clear changed folders", item, err)
	}
	item, err = store.SaveLibraryCandidate(scope, item.ID, map[string]any{"tags": []string{"React"}, "folder_ids": []string{}}, item.RowVersion, "")
	if err != nil || len(item.FolderIDs) != 0 || string(toJSONB(item.LibraryFields["tags"])) != `["React"]` {
		t.Fatal("folder clear changed raw tags", item, err)
	}
	item, err = store.SaveLibraryCandidate(scope, item.ID, map[string]any{"folder_ids": []string{folder.ID}}, item.RowVersion, "")
	if err != nil || string(toJSONB(item.LibraryFields["tags"])) != `["React"]` {
		t.Fatal("folder selection changed raw tags", item, err)
	}
	if err = store.DeleteCandidateFolder(scope, folder.ID); err != nil {
		t.Fatal(err)
	}
	item, err = store.GetPositionCandidate(scope.TenantID, item.ID, "", scope.UserEmail, true)
	if err != nil || len(item.FolderIDs) != 0 || string(toJSONB(item.LibraryFields["tags"])) != `["React"]` {
		t.Fatal("folder delete changed raw tags", item, err)
	}
	filtered, err := store.ListPositionCandidates(scope.TenantID, PositionCandidateQuery{Tag: "React", ActorEmail: scope.UserEmail})
	if err != nil || filtered.Total != 1 {
		t.Fatal("raw tag filter lost original behavior", filtered, err)
	}
	_, err = store.SaveLibraryCandidate(scope, item.ID, map[string]any{"tags": []string{"不应写入"}, "folder_ids": []string{"missing"}}, item.RowVersion, "")
	if !errors.Is(err, ErrCandidateFolderInvalid) {
		t.Fatal(err)
	}
	current, _ := store.GetPositionCandidate(scope.TenantID, item.ID, "", scope.UserEmail, true)
	if current.RowVersion != item.RowVersion || string(toJSONB(current.LibraryFields["tags"])) != `["React"]` {
		t.Fatal("failed folder validation mutated raw tags")
	}
	for _, raw := range []any{nil, "free text", []any{nil}, []string{""}, make([]string, 31)} {
		if _, err = candidateFolderIDs(raw); err == nil {
			t.Fatalf("invalid folder IDs accepted %#v", raw)
		}
	}
	aiFields, _ := candidateMutationFields(map[string]any{"candidate_name": "AI原始标签", "phone": "13800138911", "source_type": "ai_import", "tags": []string{"Python", "AI技能"}}, true)
	ai, err := store.SaveLibraryCandidate(scope, "", aiFields, 0, "")
	if err != nil || len(ai.FolderIDs) != 0 || string(toJSONB(ai.LibraryFields["tags"])) != `["Python","AI技能"]` {
		t.Fatal("AI labels converted to folders", ai, err)
	}
	automatic, err := store.SaveCandidateProfile(CandidateProfileInput{TenantID: scope.TenantID, UserEmail: scope.UserEmail, PlatformID: "liepin", PlatformCandidateID: "raw-skills", CandidateName: "平台标签", Tags: []string{"Excel", "薪酬"}})
	if err != nil || len(automatic.FolderIDs) != 0 || string(toJSONB(automatic.LibraryFields["tags"])) != `["Excel","薪酬"]` {
		t.Fatal("automatic labels converted or dropped", automatic, err)
	}
	folders, err := store.ListCandidateFolders(scope)
	if err != nil || len(folders) != 0 {
		t.Fatal("raw tags created folders", folders, err)
	}
	where, args := buildCandidateWhere("team", PositionCandidateQuery{Tag: "React", FolderID: "private-id", ActorEmail: scope.UserEmail})
	if !strings.Contains(where, "cp.library_fields->'tags'") || !strings.Contains(where, "t.scope='team' OR t.created_by_email=$") || len(args) != 4 {
		t.Fatal("raw and folder filters mixed", where, args)
	}
}

// TestCandidateFoldersPostgres 验证实际数据库收藏夹权限和原始技能标签的独立存储、清空、筛选与自动入库。
func TestCandidateFoldersPostgres(t *testing.T) {
	db := candidatePostgresTestDB(t)
	var tenantID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	owner := CandidateLibraryScope{TenantID: tenantID, UserEmail: "folders-pg-owner@example.com", IsAdmin: true}
	member := CandidateLibraryScope{TenantID: tenantID, UserEmail: "folders-pg-member@example.com"}
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES($1,$2),($3,$2)`, owner.UserEmail, tenantID, member.UserEmail); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresCandidateStore(db)
	personal, err := store.SaveCandidateFolder(member, "", "私人收藏夹", "personal")
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := candidateMutationFields(map[string]any{"candidate_name": "原始技能简历", "phone": "13800138912", "tags": []string{"Go", "薪酬"}, "folder_ids": []string{personal.ID}}, true)
	item, err := store.SaveLibraryCandidate(member, "", fields, 0, "")
	if err != nil || len(item.FolderIDs) != 1 || string(toJSONB(item.LibraryFields["tags"])) != `["Go","薪酬"]` {
		t.Fatal("raw create", item, err)
	}
	admin, err := store.GetPositionCandidate(tenantID, item.ID, "", owner.UserEmail, true)
	if err != nil || len(admin.FolderIDs) != 0 || string(toJSONB(admin.LibraryFields["tags"])) != `["Go","薪酬"]` {
		t.Fatal("admin folder isolation/raw tags", admin, err)
	}
	team, err := store.SaveCandidateFolder(owner, "", "团队收藏夹", "team")
	if err != nil {
		t.Fatal(err)
	}
	item, err = store.SaveLibraryCandidate(owner, item.ID, map[string]any{"folder_ids": []string{team.ID}}, admin.RowVersion, "")
	if err != nil || len(item.FolderIDs) != 1 || string(toJSONB(item.LibraryFields["tags"])) != `["Go","薪酬"]` {
		t.Fatal("team relation modified raw tags", item, err)
	}
	memberRead, err := store.GetPositionCandidate(tenantID, item.ID, "", member.UserEmail, false)
	if err != nil || len(memberRead.FolderIDs) != 2 {
		t.Fatal("admin cleared private folder", memberRead, err)
	}
	if _, err = store.SaveCandidateFolder(member, personal.ID, "私人收藏夹改名", "personal"); err != nil {
		t.Fatal(err)
	}
	item, err = store.SaveLibraryCandidate(member, item.ID, map[string]any{"tags": []string{}}, item.RowVersion, "")
	if err != nil || len(item.FolderIDs) != 2 || string(toJSONB(item.LibraryFields["tags"])) != `[]` {
		t.Fatal("raw clear affected folders", item, err)
	}
	item, err = store.SaveLibraryCandidate(member, item.ID, map[string]any{"tags": []string{"React"}, "folder_ids": []string{}}, item.RowVersion, "")
	if err != nil || len(item.FolderIDs) != 0 || string(toJSONB(item.LibraryFields["tags"])) != `["React"]` {
		t.Fatal("folder clear affected tags", item, err)
	}
	if err = store.DeleteCandidateFolder(owner, team.ID); err != nil {
		t.Fatal(err)
	}
	filtered, err := store.ListPositionCandidates(tenantID, PositionCandidateQuery{UserEmail: member.UserEmail, ActorEmail: member.UserEmail, Tag: "React"})
	if err != nil || filtered.Total != 1 || len(filtered.Items[0].FolderIDs) != 0 {
		t.Fatal("raw tag filter", filtered, err)
	}
	automatic, err := store.SaveCandidateProfile(CandidateProfileInput{TenantID: tenantID, UserEmail: member.UserEmail, PlatformID: "liepin", PlatformCandidateID: "pg-raw-skills", CandidateName: "平台技能", Tags: []string{"Excel", "绩效"}})
	if err != nil || len(automatic.FolderIDs) != 0 || string(toJSONB(automatic.LibraryFields["tags"])) != `["Excel","绩效"]` {
		t.Fatal("auto raw skills", automatic, err)
	}
	folders, err := store.ListCandidateFolders(member)
	if err != nil || len(folders) != 1 || folders[0].ID != personal.ID || folders[0].CandidateCount != 0 {
		t.Fatal("raw skills created folders", folders, err)
	}
}

// TestCandidateFolderMigrationPostgres 验证0096只清未经人工维护的已核实回填批次，保留手动、已编辑和后来关联的收藏夹。
func TestCandidateFolderMigrationPostgres(t *testing.T) {
	db := candidatePostgresTestDB(t)
	var tenantID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	email := "folders-migration@example.com"
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES($1,$2)`, email, tenantID); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresCandidateStore(db)
	labels := []string{"已确认生产回填", "新库回填", "回填已改名", "回填后手动加入", "手工同名"}
	candidate, err := store.SaveCandidateProfile(CandidateProfileInput{TenantID: tenantID, UserEmail: email, PlatformID: "liepin", PlatformCandidateID: "migration-skills", CandidateName: "迁移不改原文", Tags: labels})
	if err != nil {
		t.Fatal(err)
	}
	known := time.Date(2026, 10, 14, 17, 7, 42, 429804000, time.UTC)
	dynamic := known.Add(24 * time.Hour)
	// 独立schema账本模拟自动迁移SQL提交后几毫秒才记账的情况。
	if _, err = db.Exec(`CREATE TABLE schema_migrations(filename TEXT PRIMARY KEY,applied_at TIMESTAMPTZ NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO schema_migrations VALUES('0093_repair_builtin_ai_public_url.sql',$1),('0094_candidate_tags.sql',$2)`, dynamic.Add(-time.Second), dynamic.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, fixture := range []struct {
		name, scope              string
		created, updated, linked time.Time
	}{{labels[0], "personal", known, known, known}, {labels[1], "personal", dynamic, dynamic, dynamic}, {labels[2], "personal", known, known.Add(time.Minute), known}, {labels[3], "personal", known, known, known.Add(time.Minute)}, {labels[4], "personal", dynamic.Add(time.Second), dynamic.Add(time.Second), dynamic.Add(time.Second)}, {labels[1], "team", dynamic.Add(time.Second), dynamic.Add(time.Second), dynamic.Add(time.Second)}} {
		var id string
		if err = db.QueryRow(`INSERT INTO candidate_tags(tenant_id,name,scope,created_by_email,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, tenantID, fixture.name, fixture.scope, email, fixture.created, fixture.updated).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[fixture.name+"/"+fixture.scope] = id
		if _, err = db.Exec(`INSERT INTO candidate_tag_links(tenant_id,tag_id,candidate_id,created_at) VALUES($1,$2,$3,$4)`, tenantID, id, candidate.ID, fixture.linked); err != nil {
			t.Fatal(err)
		}
	}
	var before, after, preservedBefore, preservedAfter string
	if err = db.QueryRow(`SELECT row_to_json(cp)::text FROM candidate_profiles cp WHERE id=$1`, candidate.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT string_agg((to_jsonb(t)-'source_kind')::text,'' ORDER BY id) FROM candidate_tags t WHERE id NOT IN ($1,$2)`, ids[labels[0]+"/personal"], ids[labels[1]+"/personal"]).Scan(&preservedBefore); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0096_separate_resume_folders.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	if err = db.QueryRow(`SELECT row_to_json(cp)::text FROM candidate_profiles cp WHERE id=$1`, candidate.ID).Scan(&after); err != nil || before != after {
		t.Fatal("migration modified original candidate row", err)
	}
	if err = db.QueryRow(`SELECT string_agg((to_jsonb(t)-'source_kind')::text,'' ORDER BY id) FROM candidate_tags t`).Scan(&preservedAfter); err != nil || preservedBefore != preservedAfter {
		t.Fatal("manual or edited folders changed", err)
	}
	var count, links int
	if err = db.QueryRow(`SELECT count(*) FROM candidate_tags`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM candidate_tag_links`).Scan(&links); err != nil || count != 4 || links != 4 {
		t.Fatal("wrong migration removal", count, links, err)
	}
	loaded, err := store.GetPositionCandidate(tenantID, candidate.ID, "", email, false)
	if err != nil || string(toJSONB(loaded.LibraryFields["tags"])) != string(toJSONB(labels)) || len(loaded.FolderIDs) != 4 {
		t.Fatal("raw tags were changed or remaining folders lost", loaded, err)
	}
}
