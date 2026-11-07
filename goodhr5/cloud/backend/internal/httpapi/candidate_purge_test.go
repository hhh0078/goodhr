// 本文件从实际 HTTP 入口验证精确删除、版本、权限、内置示例及重复旧导入保护。
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestCandidatePurgeHTTP 验证详情和列表授权、仅删除确认版本，并检查旧任务和外部账号边界。
func TestCandidatePurgeHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "purge-owner@example.com")
	other := loginForTest(t, routes, "purge-foreign@example.com")
	body := map[string]any{"candidate_name": "合成人选", "phone": "13800138000", "source_type": "manual", "idempotency_key": "resume-import:synthetic"}
	status, out := candidateTestRequest(t, routes, token, "POST", "/api/candidates", body)
	if status != 200 {
		t.Fatal(out)
	}
	created := out["candidate"].(map[string]any)
	id := created["id"].(string)
	version := int64(created["row_version"].(float64))
	path := "/api/candidates/" + id
	check := func(verb, token, path string, status int) {
		t.Helper()
		got, r := candidateTestRequest(t, routes, token, verb, path, nil)
		if got != status {
			t.Fatalf("%s %s=%d want=%d %v", verb, path, got, status, r)
		}
	}
	_, detail := candidateTestRequest(t, routes, token, "GET", path, nil)
	if detail["candidate"].(map[string]any)["can_delete"] != true {
		t.Fatal(detail)
	}
	_, list := candidateTestRequest(t, routes, token, "GET", "/api/candidates", nil)
	for _, raw := range list["candidates"].([]any) {
		entry := raw.(map[string]any)
		if (entry["id"] == id) != (entry["can_delete"] == true) {
			t.Fatal("列表删除权限不符")
		}
	}
	query := "?row_version=" + strconv.FormatInt(version, 10)
	check("DELETE", token, "/api/candidates"+query, 400)
	check("DELETE", token, "/api/candidates?candidate_id="+id, 400)
	if status, _ := candidateTestRequest(t, routes, token, "DELETE", "/api/candidates", map[string]any{"id": id}); status != 400 {
		t.Fatal("误传单条请求体不应清全库", status)
	}
	check("DELETE", other, path+query, 404)
	check("DELETE", token, path, 400)
	check("DELETE", token, path+"?row_version=1000", 409)
	check("DELETE", token, "/api/candidates/"+builtinCandidateID+query, 403)
	check("DELETE", token, path+query, 200)
	check("DELETE", token, path+query, 200)
	check("GET", token, path, 404)
	status, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", body)
	if status != 410 {
		t.Fatalf("旧导入重放 status=%d", status)
	}
	status, checked := candidateTestRequest(t, routes, token, "POST", "/api/candidates/deletions/check", map[string]any{"candidate_ids": []string{id, "unknown"}, "source_hashes": []string{}})
	if status != 200 || len(checked["candidate_ids"].([]any)) != 1 {
		t.Fatal("删除查询失败", checked)
	}
	status, checked = candidateTestRequest(t, routes, other, "POST", "/api/candidates/deletions/check", map[string]any{"candidate_ids": []string{id}})
	if status != 200 || len(checked["candidate_ids"].([]any)) != 0 {
		t.Fatal("泄漏其他租户的删除事件", checked)
	}
	status, _ = candidateTestRequest(t, routes, other, http.MethodPost, "/api/candidates", body)
	if status != 200 {
		t.Fatalf("误拦其他租户 %d", status)
	}
	store := server.candidates.store.(*MemoryCandidateStore)
	tenant, _ := server.candidates.tenantStore.GetOrCreateTenant("purge-owner@example.com")
	events, err := store.candidateDeletions(context.Background(), tenant.ID, "purge-owner@example.com", false, 0)
	if err != nil || len(events) != 1 || len(events[0].RequestHashes) != 1 || events[0].RequestHashes[0] != purgeDigest("resume-import:synthetic") {
		t.Fatalf("事件=%v err=%v", events, err)
	}
	for _, tc := range []struct {
		actor string
		admin bool
		num   int
	}{{"ordinary", false, 0}, {"admin", true, 1}} {
		ev, e := store.candidateDeletions(context.Background(), tenant.ID, tc.actor, tc.admin, 0)
		if e != nil || len(ev) != tc.num || len(ev) > 0 && len(ev[0].RequestHashes) != 0 {
			t.Fatal("事件越权", tc, e, ev)
		}
	}
}

// TestCandidateDeletionAgentFeed 经过真实路由检查设备绑定、单页上限、游标翻页与跨租户隔离。
func TestCandidateDeletionAgentFeed(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "purge-device@example.com"
	token := loginForTest(t, routes, email)
	foreignEmail := "foreign-device@example.com"
	foreign := loginForTest(t, routes, foreignEmail)
	tenant, _ := server.candidates.tenantStore.GetOrCreateTenant(email)
	store := server.candidates.store.(*MemoryCandidateStore)
	for i := int64(1); i <= 101; i++ {
		store.deletions = append(store.deletions, memoryCandidateDeletion{tenant: tenant.ID, event: candidateDeletionEvent{Sequence: i, CandidateID: "item-" + strconv.FormatInt(i, 10), SourceHashes: []string{"source-hash"}}, recipients: map[string][]string{email: {"own-key"}}})
	}
	machine := stableAgentMachineIDPrefix + "purge-device"
	get := func(token, query string, status int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/auto-reply/agent/candidate-deletions"+query, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-GoodHR-Machine-ID", machine)
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("状态=%d 应为%d", w.Code, status)
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	get(token, "", 403)
	for _, who := range []string{email, foreignEmail} {
		if _, err := server.autoReply.agents.SaveBinding(AgentBinding{UserEmail: who, MachineID: machine}); err != nil {
			t.Fatal(err)
		}
	}
	get(token, "?after=-1", 400)
	page := get(token, "", 200)
	if len(page["events"].([]any)) != 100 || page["has_more"] != true || page["next_cursor"] != float64(100) {
		t.Fatal("分页不正确")
	}
	page = get(token, "?after=100", 200)
	if len(page["events"].([]any)) != 1 || page["has_more"] != false || page["next_cursor"] != float64(101) {
		t.Fatal("续拉错误")
	}
	page = get(foreign, "", 200)
	if len(page["events"].([]any)) != 0 || page["boundary_version"] != float64(1) {
		t.Fatal("跨团队事件泄露")
	}
}
