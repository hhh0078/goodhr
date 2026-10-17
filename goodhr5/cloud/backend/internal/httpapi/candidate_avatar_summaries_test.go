// 本文件验证推荐和招聘列表从现有简历读取头像，保持团队与参与者隔离，并避免逐条查询完整简历。
package httpapi

import (
	"net/http"
	"testing"
)

// candidateAvatarSummarySpy 记录列表批量读取的ID，确认未授权申请不进入头像查询。
type candidateAvatarSummarySpy struct {
	CandidateStore
	delegate CandidateAvatarSummaryStore
	calls    [][]string
}

// CandidateAvatarSummaries 在真实内存存储外记录查询范围，不替换业务数据或权限判断。
func (s *candidateAvatarSummarySpy) CandidateAvatarSummaries(tenantID string, ids []string) (map[string]string, error) {
	s.calls = append(s.calls, append([]string{}, ids...))
	return s.delegate.CandidateAvatarSummaries(tenantID, ids)
}

// TestRecruitmentListCandidateAvatarHTTP 验证旧流程实时读取头像、参与者只获授权头像、跨团队不可见且不扩大全简历权限。
func TestRecruitmentListCandidateAvatarHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	owner, participant, unrelated := "avatar-flow-owner@example.com", "avatar-flow-participant@example.com", "avatar-flow-unrelated@example.com"
	ownerToken := loginForTest(t, routes, owner)
	participantToken := loginForTest(t, routes, participant)
	unrelatedToken := loginForTest(t, routes, unrelated)
	foreignToken := loginForTest(t, routes, "avatar-flow-foreign@example.com")
	tenant, _ := server.tenants.store.GetOrCreateTenant(owner)
	for _, email := range []string{participant, unrelated} {
		invite, _, err := server.tenants.store.InviteMember(tenant.ID, email, "member", owner)
		if err != nil {
			t.Fatal(err)
		}
		if err = server.tenants.store.AcceptInvitation(invite.ID, email); err != nil {
			t.Fatal(err)
		}
	}
	status, response := candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "头像流程合成候选人", "phone": "13800000091"})
	if status != http.StatusOK {
		t.Fatal(response)
	}
	candidate := response["candidate"].(map[string]any)
	id := candidate["id"].(string)
	_, response = candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/positions", map[string]any{"name": "头像流程合成岗位"})
	positionID := response["position"].(map[string]any)["id"].(string)
	scope := CandidateLibraryScope{TenantID: tenant.ID, UserEmail: owner, IsAdmin: true}
	app, err := server.recruitment.StartApplication(scope, id, positionID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	app, err = server.recruitment.store.MutateApplication(tenant.ID, app.ID, app.Revision, func(item *RecruitmentApplication) error {
		item.Nodes[0].CCEmails = []string{participant}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 同人不同岗位复用一张头像，查询应去重，且流程存储本身不保存头像快照。
	second := app
	second.ID, second.PositionID = recruitmentID(), "second-avatar-position"
	if _, err = server.recruitment.store.CreateApplication(second); err != nil {
		t.Fatal(err)
	}
	_, hidden := candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "未共享头像候选人", "phone": "13800000092", "avatar_data_url": candidateAvatarTestImage(t, "jpeg", 32, 32, false)})
	hiddenID := hidden["candidate"].(map[string]any)["id"].(string)
	if _, err = server.recruitment.StartApplication(scope, hiddenID, positionID, "", ""); err != nil {
		t.Fatal(err)
	}
	status, response = candidateTestRequest(t, routes, ownerToken, http.MethodPatch, "/api/candidates/"+id, map[string]any{"row_version": candidate["row_version"], "avatar_data_url": candidateAvatarTestImage(t, "png", 32, 48, false)})
	if status != http.StatusOK {
		t.Fatal(response)
	}
	avatar := response["candidate"].(map[string]any)["avatar_data_url"].(string)
	spy := &candidateAvatarSummarySpy{CandidateStore: server.recruitment.candidates, delegate: server.recruitment.candidates.(CandidateAvatarSummaryStore)}
	server.recruitment.candidates = spy
	status, response = candidateTestRequest(t, routes, participantToken, http.MethodGet, "/api/recruitment/applications", nil)
	if status != http.StatusOK || len(response["applications"].([]any)) != 2 {
		t.Fatal("participant list permission", response)
	}
	for _, raw := range response["applications"].([]any) {
		item := raw.(map[string]any)
		if item["candidate_id"] != id || item["avatar_data_url"] != avatar {
			t.Fatal("list avatar missing or belongs to unauthorized candidate")
		}
	}
	if len(spy.calls) != 1 || len(spy.calls[0]) != 1 || spy.calls[0][0] != id {
		t.Fatal("avatar query must batch only deduplicated authorized candidates", spy.calls)
	}
	for _, token := range []string{unrelatedToken, foreignToken} {
		status, response = candidateTestRequest(t, routes, token, http.MethodGet, "/api/recruitment/applications", nil)
		if status != http.StatusOK || len(response["applications"].([]any)) != 0 || len(spy.calls) != 1 {
			t.Fatal("unrelated user caused avatar lookup or obtained application", response)
		}
	}
	status, response = candidateTestRequest(t, routes, participantToken, http.MethodGet, "/api/recruitment/applications/"+app.ID, nil)
	if status != http.StatusOK || response["candidate"].(map[string]any)["avatar_data_url"] != avatar || response["application"].(map[string]any)["permissions"].(map[string]any)["can_view_resume"] != false {
		t.Fatal("restricted detail must include avatar without granting full resume access")
	}
	status, _ = candidateTestRequest(t, routes, participantToken, http.MethodGet, "/api/candidates/"+id, nil)
	if status != http.StatusNotFound {
		t.Fatal("avatar lookup expanded full resume permission")
	}
	saved, err := server.recruitment.store.GetApplication(tenant.ID, app.ID)
	if err != nil || saved.AvatarDataURL != "" {
		t.Fatal("list projection changed persisted workflow snapshot", err)
	}
}

// TestCandidateAvatarSummariesMemory 验证仅投影请求ID且团队不符、缺失及无头像候选人安全回退。
func TestCandidateAvatarSummariesMemory(t *testing.T) {
	store := NewMemoryCandidateStore()
	store.profiles["own"] = PositionCandidate{ID: "own", TenantID: "own-team", LibraryFields: map[string]any{"avatar_data_url": "synthetic-avatar"}}
	store.profiles["empty"] = PositionCandidate{ID: "empty", TenantID: "own-team"}
	store.profiles["foreign"] = PositionCandidate{ID: "foreign", TenantID: "foreign-team", LibraryFields: map[string]any{"avatar_data_url": "foreign-avatar"}}
	items, err := store.CandidateAvatarSummaries("own-team", []string{"own", "empty", "missing", "foreign"})
	if err != nil || len(items) != 2 || items["own"] != "synthetic-avatar" || items["empty"] != "" {
		t.Fatal("memory avatar projection or tenant boundary", items, err)
	}
	items, err = store.CandidateAvatarSummaries("", []string{"own"})
	if err != nil || len(items) != 0 {
		t.Fatal("empty tenant read avatars", items, err)
	}
}

// TestCandidateAvatarSummariesPostgres 验证真实数据库只读取指定团队和ID，空头像与已删除ID保持安全回退。
func TestCandidateAvatarSummariesPostgres(t *testing.T) {
	db := candidatePostgresTestDB(t)
	var tenantID, foreignTenantID, ownID, emptyID, foreignID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&foreignTenantID); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		tenant, avatar string
		id             *string
	}{{tenantID, "own-avatar", &ownID}, {tenantID, "", &emptyID}, {foreignTenantID, "foreign-avatar", &foreignID}} {
		if err := db.QueryRow(`INSERT INTO candidate_profiles(tenant_id,source_platform_id,source_platform_candidate_id,candidate_name,library_fields) VALUES($1,'avatar-test',gen_random_uuid()::text,'合成头像测试',jsonb_build_object('avatar_data_url',$2::text)) RETURNING id::text`, row.tenant, row.avatar).Scan(row.id); err != nil {
			t.Fatal(err)
		}
	}
	store := NewPostgresCandidateStore(db)
	items, err := store.CandidateAvatarSummaries(tenantID, []string{ownID, emptyID, foreignID, "missing-non-uuid"})
	if err != nil || len(items) != 2 || items[ownID] != "own-avatar" || items[emptyID] != "" {
		t.Fatal("PostgreSQL avatar projection or tenant boundary", items, err)
	}
}
