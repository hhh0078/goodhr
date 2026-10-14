// 本文件验证个人标签隔离、团队权限、候选人数和删除标签不删除简历等真实路由边界。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCandidateManagedTagsHTTP 验证多人团队的真实标签维护、筛选和受限插件入口。
func TestCandidateManagedTagsHTTP(t *testing.T) {
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
	// 创建同名个人和团队标签合法，但同一范围重复名称拒绝。
	status, teamResult := candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates/tags", map[string]any{"name": "重点人选", "scope": "team"})
	if status != 200 {
		t.Fatalf("team tag=%d %v", status, teamResult)
	}
	teamID := teamResult["tag"].(map[string]any)["id"].(string)
	status, personalResult := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/candidates/tags", map[string]any{"name": "私有秘密", "scope": "personal"})
	if status != 200 {
		t.Fatal(personalResult)
	}
	personalID := personalResult["tag"].(map[string]any)["id"].(string)
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates/tags", map[string]any{"name": "重点人选", "scope": "personal"})
	if status != 200 {
		t.Fatal("same name across scopes rejected")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates/tags", map[string]any{"name": "重点人选", "scope": "team"})
	if status != 409 {
		t.Fatalf("duplicate status=%d", status)
	}
	status, memberCandidate := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "标签成员测试", "phone": "13800138901", "tag_ids": []string{personalID, teamID, teamID}})
	if status != 200 {
		t.Fatal(memberCandidate)
	}
	memberItem := memberCandidate["candidate"].(map[string]any)
	candidateID := memberItem["id"].(string)
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "标签管理员测试", "phone": "13800138902", "tag_ids": []string{teamID}})
	if status != 200 {
		t.Fatal("owner candidate rejected")
	}
	for _, check := range []struct {
		token    string
		expected int
	}{{ownerToken, 2}, {memberToken, 1}} {
		status, listed := candidateTestRequest(t, routes, check.token, http.MethodGet, "/api/candidates/tags", nil)
		if status != 200 {
			t.Fatal(listed)
		}
		for _, raw := range listed["tags"].([]any) {
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
	if status != 200 || strings.Contains(string(toJSONB(detail)), "私有秘密") || len(detail["candidate"].(map[string]any)["tag_ids"].([]any)) != 1 {
		t.Fatalf("personal tag leaked: %v", detail)
	}
	status, filtered := candidateTestRequest(t, routes, memberToken, http.MethodGet, "/api/candidates?tag_id="+teamID, nil)
	if status != 200 || filtered["real_total"] != 1.0 || filtered["builtin_total"] != 0.0 {
		t.Fatalf("tag filter=%v", filtered)
	}
	status, filtered = candidateTestRequest(t, routes, ownerToken, http.MethodGet, "/api/candidates?tag_id="+personalID, nil)
	if status != 200 || filtered["real_total"] != 0.0 {
		t.Fatal("admin filtered on private tag")
	}
	status, outsiders := candidateTestRequest(t, routes, outsiderToken, http.MethodGet, "/api/candidates/tags", nil)
	if status != 200 || len(outsiders["tags"].([]any)) != 0 {
		t.Fatal("tags crossed team")
	}
	status, _ = candidateTestRequest(t, routes, memberToken, http.MethodPatch, "/api/candidates/tags/"+teamID, map[string]any{"name": "越权修改", "scope": "team"})
	if status != 403 {
		t.Fatalf("member edit status=%d", status)
	}
	status, memberTeam := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/candidates/tags", map[string]any{"name": "成员团队标签", "scope": "team"})
	if status != 200 {
		t.Fatal(memberTeam)
	}
	memberTeamID := memberTeam["tag"].(map[string]any)["id"].(string)
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPatch, "/api/candidates/tags/"+memberTeamID, map[string]any{"name": "管理员改名", "scope": "team"})
	if status != 200 {
		t.Fatal("administrator cannot rename team tag")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPatch, "/api/candidates/tags/"+memberTeamID, map[string]any{"name": "错误转个人", "scope": "personal"})
	if status != 403 {
		t.Fatal("administrator moved another member tag to personal")
	}
	status, _ = candidateTestRequest(t, routes, memberToken, http.MethodPatch, "/api/candidates/tags/"+memberTeamID, map[string]any{"name": "本人转个人", "scope": "personal"})
	if status != 200 {
		t.Fatal("creator cannot change own scope")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodDelete, "/api/candidates/tags/"+personalID, nil)
	if status != 404 {
		t.Fatalf("admin delete private status=%d", status)
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPatch, "/api/candidates/"+candidateID, map[string]any{"row_version": memberItem["row_version"], "tag_ids": []string{personalID}})
	if status != 400 {
		t.Fatalf("invisible tag accepted status=%d", status)
	}
	status, cleared := candidateTestRequest(t, routes, ownerToken, http.MethodPatch, "/api/candidates/"+candidateID, map[string]any{"row_version": memberItem["row_version"], "tag_ids": []string{}})
	if status != 200 || len(cleared["candidate"].(map[string]any)["tag_ids"].([]any)) != 0 {
		t.Fatal(cleared)
	}
	status, ownDetail := candidateTestRequest(t, routes, memberToken, http.MethodGet, "/api/candidates/"+candidateID, nil)
	if status != 200 || len(ownDetail["candidate"].(map[string]any)["tag_ids"].([]any)) != 1 {
		t.Fatal("clearing visible tags erased another member private relation")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "无效标签", "phone": "13800138903", "tag_ids": []string{"unknown-tag"}})
	if status != 400 {
		t.Fatal("unknown tag accepted")
	}
	status, before := candidateTestRequest(t, routes, ownerToken, http.MethodGet, "/api/candidates", nil)
	if status != 200 || before["real_total"] != 2.0 {
		t.Fatal("failed tagging created candidate")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodDelete, "/api/candidates/tags/"+teamID, nil)
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
	status, _ = candidateTestRequest(t, routes, plugin, http.MethodGet, "/api/candidates/tags", nil)
	if status != 200 {
		t.Fatal("plugin cannot list existing tags")
	}
	status, _ = candidateTestRequest(t, routes, plugin, http.MethodPost, "/api/candidates/tags", map[string]any{"name": "插件自由新增", "scope": "personal"})
	if status != 403 {
		t.Fatal("plugin can manage tags")
	}
	status, _ = candidateTestRequest(t, routes, plugin, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "插件未知标签", "phone": "13800138904", "tags": []string{"插件随便写的标签"}})
	if status != 400 {
		t.Fatal("plugin created unknown legacy tag")
	}
}

// TestCandidateTagValidationAndLegacy 验证旧文本兼容、管理后的改名以及不合法标签输入。
func TestCandidateTagValidationAndLegacy(t *testing.T) {
	store := NewMemoryCandidateStore()
	scope := CandidateLibraryScope{TenantID: "team", UserEmail: "owner@example.com", IsAdmin: true}
	fields, err := candidateMutationFields(map[string]any{"candidate_name": "兼容标签", "phone": "13800138910", "tags": []string{"旧标签"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.SaveLibraryCandidate(scope, "", fields, 0, "legacy")
	if err != nil || len(item.TagIDs) != 1 {
		t.Fatalf("legacy item=%+v err=%v", item, err)
	}
	if _, err = store.SaveCandidateTag(scope, item.TagIDs[0], "新名字", "personal"); err != nil {
		t.Fatal(err)
	}
	item, err = store.GetPositionCandidate(scope.TenantID, item.ID, "", scope.UserEmail, true)
	if err != nil || !strings.Contains(string(toJSONB(item.LibraryFields["tags"])), "新名字") {
		t.Fatal("rename not reflected")
	}
	for _, raw := range []any{nil, "free text", []any{nil}, []string{""}, make([]string, 31)} {
		if _, err = candidateTagIDs(raw); err == nil {
			t.Fatalf("invalid accepted: %#v", raw)
		}
	}
	_, err = store.SaveLibraryCandidate(scope, item.ID, map[string]any{"tag_ids": []string{"missing"}}, item.RowVersion, "unknown")
	if !errors.Is(err, ErrCandidateTagInvalid) {
		t.Fatalf("unknown error=%v", err)
	}
	current, _ := store.GetPositionCandidate(scope.TenantID, item.ID, "", scope.UserEmail, true)
	if current.RowVersion != item.RowVersion || len(current.TagIDs) != 1 {
		t.Fatal("failed write mutated resume")
	}
	// 同一平台资料被另一成员自动同步时，返回内容也不能泄露原作者个人标签。
	_, err = store.SaveLibraryCandidate(scope, item.ID, map[string]any{"platform_id": "liepin", "platform_candidate_id": "shared-source"}, current.RowVersion, "")
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := store.SaveCandidateProfile(CandidateProfileInput{TenantID: scope.TenantID, UserEmail: "member@example.com", PlatformID: "liepin", PlatformCandidateID: "shared-source", CandidateName: "自动更新"})
	if err != nil || len(automatic.TagIDs) != 0 || strings.Contains(string(toJSONB(publicPositionCandidate(automatic))), "新名字") {
		t.Fatal("automatic response leaked private tags", err)
	}
	where, args := buildCandidateWhere("team", PositionCandidateQuery{TagID: "private-id", ActorEmail: "admin@example.com"})
	if !strings.Contains(where, "t.scope='team' OR t.created_by_email=$") || len(args) != 3 {
		t.Fatalf("unsafe tag predicate=%s args=%v", where, args)
	}
}

// TestCandidateTagsPostgres 验证实际数据库的回填、不改旧简历行、个人隔离和删除关联。
func TestCandidateTagsPostgres(t *testing.T) {
	db := candidatePostgresTestDB(t)
	var tenantID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	owner := CandidateLibraryScope{TenantID: tenantID, UserEmail: "tags-pg-owner@example.com", IsAdmin: true}
	member := CandidateLibraryScope{TenantID: tenantID, UserEmail: "tags-pg-member@example.com"}
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES($1,$2),($3,$2)`, owner.UserEmail, tenantID, member.UserEmail); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresCandidateStore(db)
	item, err := store.SaveCandidateProfile(CandidateProfileInput{TenantID: tenantID, UserEmail: member.UserEmail, PlatformID: "liepin", PlatformCandidateID: "tag-legacy", CandidateName: "历史标签"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE candidate_profiles SET library_fields='{"tags":["旧标签","旧标签","Other",null,123]}'::jsonb WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	var before, after string
	if err = db.QueryRow(`SELECT row_to_json(cp)::text FROM candidate_profiles cp WHERE id=$1`, item.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0094_candidate_tags.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	if err = db.QueryRow(`SELECT row_to_json(cp)::text FROM candidate_profiles cp WHERE id=$1`, item.ID).Scan(&after); err != nil || before != after {
		t.Fatal("migration changed original candidate row", err)
	}
	memberTags, err := store.ListCandidateTags(member)
	if err != nil || len(memberTags) != 2 {
		t.Fatalf("legacy tags=%v err=%v", memberTags, err)
	}
	for _, tag := range memberTags {
		if tag.Scope != "personal" || tag.CandidateCount != 1 {
			t.Fatal("unsafe legacy migration", tag)
		}
	}
	adminTags, err := store.ListCandidateTags(owner)
	if err != nil || len(adminTags) != 0 {
		t.Fatal("administrator saw another member tags", adminTags, err)
	}
	adminRead, err := store.GetPositionCandidate(tenantID, item.ID, "", owner.UserEmail, true)
	if err != nil || len(adminRead.TagIDs) != 0 || strings.Contains(string(toJSONB(publicPositionCandidate(adminRead))), "旧标签") {
		t.Fatal("private tag leaked", err)
	}
	teamTag, err := store.SaveCandidateTag(owner, "", "团队测试", "team")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.SaveLibraryCandidate(owner, item.ID, map[string]any{"tag_ids": []string{teamTag.ID}}, adminRead.RowVersion, "")
	if err != nil || len(updated.TagIDs) != 1 {
		t.Fatal("admin tag association failed", err)
	}
	memberRead, err := store.GetPositionCandidate(tenantID, item.ID, "", member.UserEmail, false)
	if err != nil || len(memberRead.TagIDs) != 3 {
		t.Fatal("admin erased private associations", err)
	}
	if _, err = store.SaveLibraryCandidate(owner, item.ID, map[string]any{"tag_ids": []string{memberTags[0].ID}}, updated.RowVersion, ""); !errors.Is(err, ErrCandidateTagInvalid) {
		t.Fatal("invisible ID accepted", err)
	}
	if err = store.DeleteCandidateTag(owner, teamTag.ID); err != nil {
		t.Fatal(err)
	}
	memberRead, err = store.GetPositionCandidate(tenantID, item.ID, "", member.UserEmail, false)
	if err != nil || len(memberRead.TagIDs) != 2 {
		t.Fatal("tag deletion removed candidate", err)
	}
	encoded, _ := json.Marshal(memberRead.LibraryFields["tags"])
	if strings.Contains(string(encoded), "团队测试") {
		t.Fatal("deleted label remained")
	}
}
