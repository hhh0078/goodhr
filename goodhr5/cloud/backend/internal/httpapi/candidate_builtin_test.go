// 本文件通过真实HTTP路由验证共享示例认证、不可写、分页筛选和真实简历隔离，不连接生产数据库。
package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestBuiltinCandidateAuthenticationAndWrites 验证所有登录账号可读，但预留身份的任何写入都被拒绝。
func TestBuiltinCandidateAuthenticationAndWrites(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "builtin-owner@example.com")
	other := loginForTest(t, routes, "builtin-other@example.com")
	base := "/api/candidates/" + builtinCandidateID
	for _, path := range []string{"/api/candidates", base, base + "/notes", base + "/recommendations"} {
		status, _ := candidateTestRequest(t, routes, "", http.MethodGet, path, nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("anonymous %s status=%d", path, status)
		}
	}
	for _, account := range []string{token, other} {
		status, data := candidateTestRequest(t, routes, account, http.MethodGet, base+"?engagement_id=unrelated", nil)
		if status != http.StatusOK {
			t.Fatalf("read status=%d", status)
		}
		candidate := data["candidate"].(map[string]any)
		if candidate["id"] != builtinCandidateID || candidate["is_builtin"] != true || candidate["readonly"] != true || candidate["candidate_name"] != "瓜瓜" || candidate["phone"] != "17607080935" || candidate["email"] != "1224299352@qq.com" {
			t.Fatal("builtin contract mismatch")
		}
		if !strings.Contains(candidate["builtin_notice"].(string), "权限隔离") || candidate["user_email"] != "" {
			t.Fatal("builtin owner/notice mismatch")
		}
		for _, resource := range []string{"/notes", "/recommendations"} {
			status, _ = candidateTestRequest(t, routes, account, http.MethodGet, base+resource, nil)
			if status != http.StatusOK {
				t.Fatalf("read %s status=%d", resource, status)
			}
		}
		for _, resource := range []string{"", "/notes", "/recommendations", "/avatar", "/ai", "/associations"} {
			for _, method := range []string{http.MethodPatch, http.MethodPost, http.MethodDelete, http.MethodPut} {
				status, _ = candidateTestRequest(t, routes, account, method, base+resource, map[string]any{"row_version": 1, "ai_summary": "不能写", "content": "不能写", "position_id": "forged"})
				if status != http.StatusForbidden {
					t.Fatalf("write %s %s status=%d", method, resource, status)
				}
			}
		}
	}
	for _, forged := range []map[string]any{{"id": builtinCandidateID}, {"candidate_id": builtinCandidateID}, {"platform_candidate_id": builtinCandidateID}, {"is_builtin": true}, {"is_builtin": false}, {"readonly": false}, {"builtin_notice": "伪造"}, {"source_type": "builtin"}} {
		forged["candidate_name"], forged["phone"] = "伪造示例", "13800000001"
		status, _ := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", forged)
		if status != http.StatusForbidden {
			t.Fatalf("forged identity status=%d", status)
		}
	}
	status, positionData := candidateTestRequest(t, routes, token, http.MethodPost, "/api/positions", map[string]any{"name": "示例保护测试岗位"})
	if status != http.StatusOK {
		t.Fatalf("position status=%d", status)
	}
	positionID := positionData["position"].(map[string]any)["id"].(string)
	for _, payload := range []map[string]any{{"id": builtinCandidateID}, {"candidate_id": builtinCandidateID}, {"is_builtin": true}, {"readonly": false}, {"source_type": "builtin"}} {
		payload["candidate_name"], payload["phone"] = "示例不可回传", "13800000001"
		status, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/positions/"+positionID+"/candidates", payload)
		if status != http.StatusForbidden {
			t.Fatalf("old ingest accepted builtin status=%d", status)
		}
	}
	position, err := server.positions.store.PositionByID("", "builtin-owner@example.com", positionID, true)
	if err != nil || position.ScannedCount != 0 || position.DailyGreetedCount != 0 {
		t.Fatal("builtin ingest polluted runtime counts")
	}
	status, filtered := candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates?position_id="+positionID, nil)
	if status != http.StatusOK || filtered["total"] != float64(0) {
		t.Fatal("builtin associated to a real position")
	}
	status, data := candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates", nil)
	if status != http.StatusOK || data["real_total"] != float64(0) || data["builtin_total"] != float64(1) || data["total"] != float64(1) {
		t.Fatal("read/write created real candidates")
	}
}

// TestBuiltinCandidatePaginationFiltersAndPrivacy 验证示例占位不遗漏真实记录，并保持筛选、查重和跨账号权限。
func TestBuiltinCandidatePaginationFiltersAndPrivacy(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "builtin-pages@example.com")
	other := loginForTest(t, routes, "builtin-pages-other@example.com")
	status, duplicates := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/duplicates", map[string]any{"phone": "17607080935", "candidate_name": "瓜瓜"})
	if status != http.StatusOK || len(duplicates["matches"].([]any)) != 0 || duplicates["hidden_match"] != false {
		t.Fatal("builtin entered duplicate checks")
	}
	ids := map[string]bool{builtinCandidateID: true}
	var firstID string
	for i := 0; i < 4; i++ {
		phone := fmt.Sprintf("1380000000%d", i)
		if i == 0 {
			phone = "17607080935"
		}
		status, data := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": fmt.Sprintf("私有测试%d", i), "phone": phone})
		if status != http.StatusOK {
			t.Fatalf("create status=%d", status)
		}
		id := data["candidate"].(map[string]any)["id"].(string)
		ids[id] = true
		if firstID == "" {
			firstID = id
		}
	}
	for _, size := range []int{1, 2, 3, 100} {
		seen := map[string]bool{}
		for page := 1; page <= 7; page++ {
			status, data := candidateTestRequest(t, routes, token, http.MethodGet, fmt.Sprintf("/api/candidates?page=%d&page_size=%d", page, size), nil)
			if status != http.StatusOK || data["total"] != float64(5) || data["real_total"] != float64(4) || data["builtin_total"] != float64(1) {
				t.Fatal("pagination counts mismatch")
			}
			for _, value := range data["candidates"].([]any) {
				id := value.(map[string]any)["id"].(string)
				if seen[id] || !ids[id] {
					t.Fatalf("duplicate/unknown page row %s", id)
				}
				seen[id] = true
			}
		}
		if len(seen) != len(ids) {
			t.Fatalf("pagination omitted candidates size=%d", size)
		}
	}
	for _, query := range []string{"q=瓜瓜", "q=示例计算机学院", "phone=0935", "region=成都", "education_level=本科", "work_years=8年", "work_status=在职", "tag=内置示例", "creator=管理员", "source_type=builtin", "created_from=2026-10-10&created_to=2026-10-10"} {
		status, data := candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates?"+query, nil)
		if status != http.StatusOK || data["builtin_total"] != float64(1) {
			t.Fatalf("builtin missing matching filter %s", query)
		}
	}
	for _, query := range []string{"q=" + url.QueryEscape("完全不匹配"), "position_id=real-position", "source_type=manual", "creator=builtin-pages@example.com", "created_to=2026-10-09"} {
		status, data := candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates?"+query, nil)
		if status != http.StatusOK || data["builtin_total"] != float64(0) {
			t.Fatalf("builtin ignored filter %s", query)
		}
	}
	status, data := candidateTestRequest(t, routes, other, http.MethodGet, "/api/candidates", nil)
	if status != http.StatusOK || data["real_total"] != float64(0) || len(data["candidates"].([]any)) != 1 {
		t.Fatal("shared example exposed another account's real records")
	}
	status, _ = candidateTestRequest(t, routes, other, http.MethodGet, "/api/candidates/"+firstID, nil)
	if status != http.StatusNotFound {
		t.Fatal("real detail escaped tenant scope")
	}
	status, duplicates = candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/duplicates", map[string]any{"phone": "17607080935"})
	if status != http.StatusOK || len(duplicates["matches"].([]any)) != 1 || duplicates["matches"].([]any)[0].(map[string]any)["candidate"].(map[string]any)["id"] == builtinCandidateID {
		t.Fatal("virtual contact affected real duplicate match")
	}
	// 同团队普通成员仍只看自己的真实简历；共享示例不放宽其他候选人的作者权限。
	tenant, err := server.tenants.store.GetOrCreateTenant("builtin-pages@example.com")
	if err != nil {
		t.Fatal(err)
	}
	invite, _, err := server.tenants.store.InviteMember(tenant.ID, "builtin-pages-other@example.com", "member", "builtin-pages@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err = server.tenants.store.AcceptInvitation(invite.ID, "builtin-pages-other@example.com"); err != nil {
		t.Fatal(err)
	}
	status, data = candidateTestRequest(t, routes, other, http.MethodGet, "/api/candidates", nil)
	if status != http.StatusOK || data["real_total"] != float64(0) || data["total"] != float64(1) {
		t.Fatal("example bypassed real creator visibility")
	}
	for _, suffix := range []string{"", "/notes", "/recommendations"} {
		status, _ = candidateTestRequest(t, routes, other, http.MethodGet, "/api/candidates/"+firstID+suffix, nil)
		if status != http.StatusNotFound {
			t.Fatalf("member exposed another creator resource %s", suffix)
		}
	}
	status, _ = candidateTestRequest(t, routes, other, http.MethodPatch, "/api/candidates/"+firstID, map[string]any{"row_version": 1, "personal_description": "越权修改"})
	if status != http.StatusNotFound {
		t.Fatal("example bypassed real creator write scope")
	}
	status, duplicates = candidateTestRequest(t, routes, other, http.MethodPost, "/api/candidates/duplicates", map[string]any{"phone": "17607080935"})
	if status != http.StatusOK || len(duplicates["matches"].([]any)) != 0 || duplicates["hidden_match"] != true {
		t.Fatal("example bypassed hidden real duplicate privacy")
	}
	status, cleared := candidateTestRequest(t, routes, token, http.MethodDelete, "/api/candidates", nil)
	if status != http.StatusOK || cleared["deleted"] != float64(4) {
		t.Fatalf("clear real candidates status=%d", status)
	}
	_, data = candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates", nil)
	if data["real_total"] != float64(0) || data["total"] != float64(1) {
		t.Fatal("clear removed example or count is polluted")
	}
}
