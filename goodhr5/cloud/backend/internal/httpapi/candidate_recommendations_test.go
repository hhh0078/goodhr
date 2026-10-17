// 本文件验证猎头推荐服务端分页筛选、作者权限、关闭历史和删除仅解除推荐关系。
package httpapi

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// TestCandidateRecommendationsHTTP 验证团队推荐分页、筛选、关闭岗位历史及删除权限。
func TestCandidateRecommendationsHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	owner, member := "recommendations-owner@example.com", "recommendations-member@example.com"
	ownerToken := loginForTest(t, routes, owner)
	memberToken := loginForTest(t, routes, member)
	otherToken := loginForTest(t, routes, "recommendations-other@example.com")
	tenant, _ := server.tenants.store.GetOrCreateTenant(owner)
	invite, _, err := server.tenants.store.InviteMember(tenant.ID, member, "member", owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.tenants.store.AcceptInvitation(invite.ID, member); err != nil {
		t.Fatal(err)
	}
	_, ownerPosition := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/positions", map[string]any{"name": "管理岗位"})
	ownerPositionID := ownerPosition["position"].(map[string]any)["id"].(string)
	_, memberPosition := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/positions", map[string]any{"name": "推荐岗位"})
	memberPositionID := memberPosition["position"].(map[string]any)["id"].(string)
	_, first := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "张推荐测试", "phone": "13800138960", "current_position": "开发工程师", "avatar_data_url": candidateAvatarTestImage(t, "png", 32, 32, false)})
	firstID := first["candidate"].(map[string]any)["id"].(string)
	firstAvatar := first["candidate"].(map[string]any)["avatar_data_url"]
	_, second := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "李推荐测试", "phone": "13800138961"})
	secondID := second["candidate"].(map[string]any)["id"].(string)
	status, adminRec := candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates/"+firstID+"/recommendations", map[string]any{"position_id": ownerPositionID, "stage": "recommended", "content": "管理推进", "idempotency_key": "admin-rec"})
	if status != 200 {
		t.Fatal(adminRec)
	}
	adminID := adminRec["recommendation"].(map[string]any)["id"].(string)
	_, firstRec := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/candidates/"+firstID+"/recommendations", map[string]any{"position_id": memberPositionID, "stage": "draft", "idempotency_key": "own-rec"})
	firstRecID := firstRec["recommendation"].(map[string]any)["id"].(string)
	status, _ = candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/candidates/"+secondID+"/recommendations", map[string]any{"position_id": memberPositionID, "stage": "contacting", "content": "联系一下"})
	if status != 200 {
		t.Fatal("second recommendation failed")
	}
	for _, check := range []struct {
		query        string
		total, count int
	}{{"?page=2&page_size=1", 3, 1}, {"?q=张推荐", 2, 2}, {"?q=38961", 1, 1}, {"?q=推荐岗位", 2, 2}, {"?stage=contacting", 1, 1}, {"?position_id=" + ownerPositionID, 1, 1}, {"?author=" + owner, 1, 1}, {"?created_from=" + time.Now().In(chinaLocation()).Format(time.DateOnly), 3, 3}} {
		status, listed := candidateTestRequest(t, routes, memberToken, http.MethodGet, "/api/candidate-recommendations"+check.query, nil)
		if status != 200 || listed["total"] != float64(check.total) || len(listed["items"].([]any)) != check.count {
			t.Fatalf("filter=%s listed=%v", check.query, listed)
		}
	}
	status, listed := candidateTestRequest(t, routes, memberToken, http.MethodGet, "/api/candidate-recommendations", nil)
	if status != 200 {
		t.Fatal(listed)
	}
	for _, raw := range listed["items"].([]any) {
		item := raw.(map[string]any)
		if item["candidate_id"] == firstID && (firstAvatar == nil || item["avatar_data_url"] != firstAvatar) {
			t.Fatal("recommendation omitted persisted candidate avatar")
		}
		if item["candidate_id"] == secondID && item["avatar_data_url"] != nil {
			t.Fatal("candidate without avatar received another candidate's image")
		}
		if item["id"] == adminID && (item["can_delete"] != false || item["can_edit"] != true || len(item["events"].([]any)) != 1 || item["current_position"] != "开发工程师") {
			t.Fatal("record summary or permission missing", item)
		}
	}
	status, _ = candidateTestRequest(t, routes, memberToken, http.MethodDelete, "/api/candidate-recommendations/"+adminID, nil)
	if status != 403 {
		t.Fatal("non-author deleted recommendation")
	}
	status, hidden := candidateTestRequest(t, routes, otherToken, http.MethodGet, "/api/candidate-recommendations", nil)
	if status != 200 || hidden["total"] != 0.0 {
		t.Fatal("recommendation crossed team", hidden)
	}
	status, _ = candidateTestRequest(t, routes, otherToken, http.MethodDelete, "/api/candidate-recommendations/"+firstRecID, nil)
	if status != 404 {
		t.Fatal("foreign deletion exposed record")
	}
	status, _ = candidateTestRequest(t, routes, memberToken, http.MethodPatch, "/api/positions/"+memberPositionID, map[string]any{"is_open": false})
	if status != 200 {
		t.Fatal("close failed")
	}
	_, closed := candidateTestRequest(t, routes, memberToken, http.MethodGet, "/api/candidate-recommendations?position_id="+memberPositionID, nil)
	if closed["total"] != 2.0 {
		t.Fatal("closed history hidden")
	}
	for _, raw := range closed["items"].([]any) {
		item := raw.(map[string]any)
		if item["position_is_open"] != false || item["can_edit"] != false || item["can_delete"] != true {
			t.Fatal("closed action flags wrong", item)
		}
	}
	status, _ = candidateTestRequest(t, routes, memberToken, http.MethodDelete, "/api/candidate-recommendations/"+firstRecID, nil)
	if status != 200 {
		t.Fatal("author delete failed")
	}
	status, _ = candidateTestRequest(t, routes, ownerToken, http.MethodDelete, "/api/candidate-recommendations/"+adminID, nil)
	if status != 200 {
		t.Fatal("admin delete failed")
	}
	status, detail := candidateTestRequest(t, routes, memberToken, http.MethodGet, "/api/candidates/"+firstID, nil)
	if status != 200 || len(detail["candidate"].(map[string]any)["recommendations"].([]any)) != 0 {
		t.Fatal("delete removed candidate or stale recommendation", detail)
	}
	status, _ = candidateTestRequest(t, routes, memberToken, http.MethodGet, "/api/positions/"+memberPositionID, nil)
	if status != 200 {
		t.Fatal("delete removed position")
	}
	for _, query := range []string{"?stage=unknown", "?created_from=bad", "?created_from=2026-10-15&created_to=2026-10-14"} {
		status, _ = candidateTestRequest(t, routes, memberToken, http.MethodGet, "/api/candidate-recommendations"+query, nil)
		if status != 400 {
			t.Fatalf("invalid filter accepted %s", query)
		}
	}
	_, exchange := candidateTestRequest(t, routes, memberToken, http.MethodPost, "/api/auth/extension-session", map[string]any{})
	status, _ = candidateTestRequest(t, routes, exchange["token"].(string), http.MethodGet, "/api/candidate-recommendations", nil)
	if status != 403 {
		t.Fatal("plugin gained global management")
	}
}

// TestCandidateRecommendationsPostgres 验证数据库分页、隐藏记录隔离和删除外键仅级联推荐历史。
func TestCandidateRecommendationsPostgres(t *testing.T) {
	db := candidatePostgresTestDB(t)
	var tenantID, otherTenantID, positionID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&otherTenantID); err != nil {
		t.Fatal(err)
	}
	owner := CandidateLibraryScope{TenantID: tenantID, UserEmail: "rec-pg-owner@example.com", IsAdmin: true}
	member := CandidateLibraryScope{TenantID: tenantID, UserEmail: "rec-pg-member@example.com"}
	foreign := CandidateLibraryScope{TenantID: otherTenantID, UserEmail: "rec-pg-foreign@example.com", IsAdmin: true}
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES($1,$2),($3,$2),($4,$5)`, owner.UserEmail, tenantID, member.UserEmail, foreign.UserEmail, otherTenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO positions(name) VALUES('推荐工程师') RETURNING id::text`).Scan(&positionID); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresCandidateStore(db)
	position := Position{ID: positionID, Name: "推荐工程师", IsOpen: true}
	ids := []string{}
	candidateIDs := []string{}
	rawAvatar := candidateAvatarTestImage(t, "png", 32, 32, false)
	avatar, err := normalizeCandidateAvatar(rawAvatar)
	if err != nil {
		t.Fatal(err)
	}
	for i, input := range []struct {
		scope       CandidateLibraryScope
		name, phone string
	}{{member, "张数据库推荐", "13800138970"}, {owner, "李数据库推荐", "13800138971"}} {
		// 和实际入库请求一致，原图只经一次字段校验；透明PNG再次重编码可能产生不同字节。
		fields, err := candidateMutationFields(map[string]any{"candidate_name": input.name, "phone": input.phone, "current_position": "工程师", "avatar_data_url": rawAvatar}, true)
		if err != nil || candidateString(fields, "avatar_data_url") != avatar {
			t.Fatal("candidate avatar validation differs from normalized source", err)
		}
		candidate, err := store.SaveLibraryCandidate(input.scope, "", fields, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		var storedAvatar string
		if err = db.QueryRow(`SELECT library_fields->>'avatar_data_url' FROM candidate_profiles WHERE id=$1::uuid`, candidate.ID).Scan(&storedAvatar); err != nil || storedAvatar != avatar || candidateString(candidate.LibraryFields, "avatar_data_url") != storedAvatar {
			t.Fatal("saved candidate avatar must equal validated fields and persisted JSONB", err)
		}
		rec, err := store.SaveCandidateRecommendation(owner, candidate.ID, position, "draft", "首次推荐", "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.ID)
		candidateIDs = append(candidateIDs, candidate.ID)
		if _, err = db.Exec(`UPDATE candidate_recommendations SET created_at=$1 WHERE id=$2`, time.Date(2026, 10, 14+i, 0, 30, 0, 0, chinaLocation()), rec.ID); err != nil {
			t.Fatal(err)
		}
	}
	memberList, err := store.ListCandidateRecommendationRecords(member, CandidateRecommendationQuery{PageSize: 1})
	if err != nil || memberList.Total != 1 || len(memberList.Items) != 1 || memberList.Items[0].CanDelete || !memberList.Items[0].CanEdit || len(memberList.Items[0].Events) != 1 {
		t.Fatal("member page boundary", memberList, err)
	}
	if memberList.Items[0].AvatarDataURL != avatar {
		t.Fatal("PostgreSQL recommendation omitted JSONB avatar")
	}
	all, err := store.ListCandidateRecommendationRecords(owner, CandidateRecommendationQuery{Page: 2, PageSize: 1})
	if err != nil || all.Total != 2 || len(all.Items) != 1 {
		t.Fatal("PG pagination", all, err)
	}
	for _, check := range []struct {
		query CandidateRecommendationQuery
		count int
	}{{CandidateRecommendationQuery{Keyword: "张数据库"}, 1}, {CandidateRecommendationQuery{Keyword: "38971"}, 1}, {CandidateRecommendationQuery{PositionID: positionID, Stage: "draft", Author: owner.UserEmail}, 2}, {CandidateRecommendationQuery{CreatedFrom: "2026-10-14", CreatedTo: "2026-10-14"}, 1}} {
		result, err := store.ListCandidateRecommendationRecords(owner, check.query)
		if err != nil || result.Total != check.count {
			t.Fatal("PG filter", check.query, result, err)
		}
	}
	if err = store.DeleteCandidateRecommendation(member, ids[0]); !errors.Is(err, ErrCandidateRecommendationForbidden) {
		t.Fatal("member non-author delete", err)
	}
	if err = store.DeleteCandidateRecommendation(foreign, ids[0]); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-team delete", err)
	}
	if _, err = db.Exec(`UPDATE positions SET is_open=false WHERE id=$1`, positionID); err != nil {
		t.Fatal(err)
	}
	closed, err := store.ListCandidateRecommendationRecords(owner, CandidateRecommendationQuery{})
	if err != nil || closed.Total != 2 || closed.Items[0].CanEdit || closed.Items[0].PositionIsOpen {
		t.Fatal("closed history", closed, err)
	}
	if err = store.DeleteCandidateRecommendation(owner, ids[0]); err != nil {
		t.Fatal(err)
	}
	var events, candidates, positions int
	if err = db.QueryRow(`SELECT count(*) FROM candidate_recommendation_events WHERE recommendation_id=$1`, ids[0]).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM candidate_profiles WHERE id=$1`, candidateIDs[0]).Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM positions WHERE id=$1`, positionID).Scan(&positions); err != nil || events != 0 || candidates != 1 || positions != 1 {
		t.Fatal("deletion cascaded into candidate or position", events, candidates, positions, err)
	}
}
