// 本文件把插件真实生成的合成请求交给云端路由，验证字段映射、受限会话和独立备注推荐可联通。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCandidatePatchPreflightAllowsMethod 验证真实路由允许跨域简历修改预检，浏览器不会在发出 PATCH 前拦截。
func TestCandidatePatchPreflightAllowsMethod(t *testing.T) {
	routes := mustNewServer(t).Routes()
	request := httptest.NewRequest(http.MethodOptions, "/api/candidates/preflight-fixture", nil)
	request.Header.Set("Origin", "http://127.0.0.1:3000")
	request.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	request.Header.Set("Access-Control-Request-Headers", "content-type,authorization")
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("跨域预检未放行: status=%d headers=%v", response.Code, response.Header())
	}
	patchAllowed := false
	for _, method := range strings.Split(response.Header().Get("Access-Control-Allow-Methods"), ",") {
		patchAllowed = patchAllowed || strings.TrimSpace(method) == http.MethodPatch
	}
	if !patchAllowed {
		t.Fatalf("预检缺少 PATCH: %v", response.Header())
	}
	headers := strings.ToLower(response.Header().Get("Access-Control-Allow-Headers"))
	if !strings.Contains(headers, "content-type") || !strings.Contains(headers, "authorization") {
		t.Fatalf("预检缺少候选人请求所需头: %v", response.Header())
	}
}

// TestPluginCandidateRequestContract 使用真实字段校验保存插件草稿，防止各模块单测通过但互相不兼容。
func TestPluginCandidateRequestContract(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	fixture := filepath.Join(filepath.Dir(source), "../../../..", "browser-extension", "tests", "fixtures", "plugin-candidate-create.json")
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if json.Unmarshal(raw, &input) != nil {
		t.Fatal("插件请求样本格式不正确")
	}
	if candidateString(input, "avatar_data_url") == "" {
		t.Fatal("插件联调样本缺少实际整理的合成头像")
	}
	server := mustNewServer(t)
	routes := server.Routes()
	webToken := loginForTest(t, routes, "plugin-contract@example.com")
	status, exchange := candidateTestRequest(t, routes, webToken, http.MethodPost, "/api/auth/extension-session", map[string]any{})
	if status != 200 {
		t.Fatalf("会话交换: %v", exchange)
	}
	pluginToken, _ := exchange["token"].(string)
	status, me := candidateTestRequest(t, routes, pluginToken, http.MethodGet, "/api/auth/me", nil)
	if status != 200 || me["session"].(map[string]any)["kind"] != extensionSessionKind {
		t.Fatalf("插件权限类型未公开: %v", me)
	}
	status, response := candidateTestRequest(t, routes, pluginToken, http.MethodPost, "/api/candidates", input)
	if status != 200 {
		t.Fatalf("插件真实请求被拒绝: %v", response)
	}
	candidate := response["candidate"].(map[string]any)
	id := candidate["id"].(string)
	if candidate["expected_region"] != "期望测试城市" || candidate["current_region"] != "合同测试城市" || len(candidate["tag_ids"].([]any)) != 0 || candidate["expected_salary_min"] != 20.0 {
		t.Fatalf("插件字段未正确保存: %v", candidate)
	}
	avatar := candidateString(candidate, "avatar_data_url")
	if !strings.HasPrefix(avatar, "data:image/png;base64,") {
		t.Fatal("插件实际整理的头像未被云端保存")
	}
	status, detail := candidateTestRequest(t, routes, pluginToken, http.MethodGet, "/api/candidates/"+id, nil)
	if status != 200 || candidateString(detail["candidate"].(map[string]any), "avatar_data_url") != avatar {
		t.Fatal("保存后的头像未通过受保护的简历详情返回")
	}
	status, again := candidateTestRequest(t, routes, pluginToken, http.MethodPost, "/api/candidates", input)
	if status != 200 || again["candidate"].(map[string]any)["id"] != id {
		t.Fatal("插件重试重复创建")
	}
	structured := map[string]any{}
	for _, key := range []string{"candidate_name", "gender", "age", "birth_ym", "work_experiences", "educations"} {
		structured[key] = input[key]
	}
	status, duplicates := candidateTestRequest(t, routes, pluginToken, http.MethodPost, "/api/candidates/duplicates", structured)
	if status != 200 {
		t.Fatalf("缺号结构化查重不联通: %v", duplicates)
	}
	matches, ok := duplicates["matches"].([]any)
	if !ok || len(matches) != 1 || matches[0].(map[string]any)["match_type"] != "possible" || matches[0].(map[string]any)["candidate"].(map[string]any)["id"] != id {
		t.Fatalf("同一插件履历缺号时应提示可能重复: %v", duplicates)
	}
	criteria := duplicates["criteria"].(map[string]any)
	if criteria["earliest_company"] != "合成测试公司" || criteria["earliest_company_year"] != 2020.0 || criteria["earliest_school_year"] != 2010.0 {
		t.Fatalf("查重依据丢失插件经历字段: %v", criteria)
	}
	status, position := candidateTestRequest(t, routes, webToken, http.MethodPost, "/api/positions", map[string]any{"name": "插件联调岗位"})
	if status != 200 {
		t.Fatal("无法创建隔离测试岗位")
	}
	positionID := position["position"].(map[string]any)["id"].(string)
	occurred := time.Now().Add(-time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339)
	noteInput := map[string]any{"content": "合成备注\n下一步跟进", "idempotency_key": "plugin-note", "occurred_at": occurred}
	status, _ = candidateTestRequest(t, routes, pluginToken, http.MethodPost, "/api/candidates/"+id+"/notes", noteInput)
	if status != 200 {
		t.Fatal("插件备注不联通")
	}
	status, _ = candidateTestRequest(t, routes, pluginToken, http.MethodPost, "/api/candidates/"+id+"/notes", noteInput)
	if status != 200 {
		t.Fatal("同一插件备注重试失败")
	}
	noteInput["idempotency_key"] = "plugin-note-next"
	status, _ = candidateTestRequest(t, routes, pluginToken, http.MethodPost, "/api/candidates/"+id+"/notes", noteInput)
	if status != 200 {
		t.Fatal("同样文字的新备注不能独立追加")
	}
	status, notes := candidateTestRequest(t, routes, pluginToken, http.MethodGet, "/api/candidates/"+id+"/notes", nil)
	items, ok := notes["notes"].([]any)
	if status != 200 || !ok || len(items) != 2 {
		t.Fatalf("备注应逐条保存且重试不重复: %v", notes)
	}
	for _, item := range items {
		note := item.(map[string]any)
		if note["content"] != "合成备注\n下一步跟进" || note["occurred_at"] != occurred || note["author_email"] != "plugin-contract@example.com" {
			t.Fatalf("备注正文/作者/发生时间不完整: %v", note)
		}
	}
	status, rec := candidateTestRequest(t, routes, pluginToken, http.MethodPost, "/api/candidates/"+id+"/recommendations", map[string]any{"position_id": positionID, "stage": "draft", "idempotency_key": "plugin-recommend"})
	if status != 200 {
		t.Fatalf("插件岗位关联不联通: %v", rec)
	}
	recommendationInput := map[string]any{"position_id": positionID, "stage": "recommended", "content": "合成推荐专属备注", "occurred_at": occurred, "idempotency_key": "plugin-recommend-next"}
	status, rec = candidateTestRequest(t, routes, pluginToken, http.MethodPost, "/api/candidates/"+id+"/recommendations", recommendationInput)
	if status != 200 {
		t.Fatalf("插件状态/推荐备注/时间不联通: %v", rec)
	}
	status, _ = candidateTestRequest(t, routes, pluginToken, http.MethodPost, "/api/candidates/"+id+"/recommendations", recommendationInput)
	if status != 200 {
		t.Fatal("推荐重试失败")
	}
	status, recs := candidateTestRequest(t, routes, pluginToken, http.MethodGet, "/api/candidates/"+id+"/recommendations", nil)
	recommendations, ok := recs["recommendations"].([]any)
	if status != 200 || !ok || len(recommendations) != 1 {
		t.Fatalf("同一岗位重复建立关系: %v", recs)
	}
	item := recommendations[0].(map[string]any)
	events := item["events"].([]any)
	if item["stage"] != "recommended" || len(events) != 2 {
		t.Fatalf("推荐阶段和幂等历史有误: %v", item)
	}
	foundRecommended := false
	for _, rawEvent := range events {
		event := rawEvent.(map[string]any)
		if event["to_stage"] != "recommended" {
			continue
		}
		foundRecommended = true
		if event["occurred_at"] != occurred || event["content"] != "合成推荐专属备注" || event["author_email"] != "plugin-contract@example.com" || event["created_at"] == "" {
			t.Fatalf("推荐事件细节有误: %v", event)
		}
	}
	if !foundRecommended {
		t.Fatal("推荐状态历史缺失")
	}
}
