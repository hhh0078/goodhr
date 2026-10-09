// 本文件验证简历新入口手机号、并发幂等、权限、人工字段保护和推荐历史，保护旧自动采集兼容性。
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestCandidateSourceFilteringLegacyAndExplicit 验证历史空来源、新自动来源和人工来源的筛选、分页及公共返回保持一致。
func TestCandidateSourceFilteringLegacyAndExplicit(t *testing.T) {
	store := NewMemoryCandidateStore()
	fields := []map[string]any{nil, {}, {"source_type": ""}, {"source_type": nil}, {"source_type": "  "}, {"source_type": "automatic"}, {"source_type": "manual"}, {"source_type": "ai_import"}, {"source_type": "browser_extension"}}
	for index, source := range fields {
		id := fmt.Sprintf("source-%d", index)
		store.profiles[id] = PositionCandidate{ID: id, TenantID: "team", UserEmail: "owner@example.com", LibraryFields: source}
	}
	store.profiles["other-member"] = PositionCandidate{ID: "other-member", TenantID: "team", UserEmail: "member@example.com"}
	store.profiles["other-team"] = PositionCandidate{ID: "other-team", TenantID: "other-team", UserEmail: "owner@example.com"}
	for _, item := range []struct {
		source string
		count  int
	}{{"", 9}, {"automatic", 6}, {"manual", 1}, {"ai_import", 1}, {"browser_extension", 1}} {
		result, err := store.ListPositionCandidates("team", PositionCandidateQuery{UserEmail: "owner@example.com", SourceType: item.source, PageSize: 100})
		if err != nil || result.Total != item.count || len(result.Items) != item.count {
			t.Fatalf("source=%s list=%+v err=%v", item.source, result, err)
		}
		for _, candidate := range result.Items {
			public := publicPositionCandidate(candidate)
			if item.source != "" && public["source_type"] != item.source {
				t.Fatalf("source filter and public response disagree: source=%s public=%v", item.source, public["source_type"])
			}
		}
	}
	page, err := store.ListPositionCandidates("team", PositionCandidateQuery{UserEmail: "owner@example.com", SourceType: "automatic", PageSize: 2, Page: 2})
	if err != nil || page.Total != 6 || len(page.Items) != 2 {
		t.Fatalf("automatic pagination=%+v err=%v", page, err)
	}
	for _, source := range []string{"manual", "ai_import", "browser_extension"} {
		if _, err := candidateMutationFields(map[string]any{"candidate_name": "来源测试", "phone": "13800138000", "source_type": source}, true); err != nil {
			t.Fatalf("new source %s rejected: %v", source, err)
		}
	}
	if _, err := candidateMutationFields(map[string]any{"candidate_name": "来源测试", "phone": "13800138000", "source_type": "automatic"}, true); err == nil {
		t.Fatal("new manual endpoint accepted automatic source")
	}
	where, args := buildCandidateWhere("team", PositionCandidateQuery{UserEmail: "owner@example.com", SourceType: "automatic"})
	if !strings.Contains(where, "COALESCE(NULLIF(BTRIM(cp.library_fields->>'source_type'),''),'automatic') ILIKE $3") || len(args) != 3 || args[2] != "%automatic%" {
		t.Fatalf("legacy-aware SQL predicate missing: where=%s args=%v", where, args)
	}
}

// candidateTestRequest 通过真实路由提交候选人请求，不绕过认证和字段校验。
func candidateTestRequest(t *testing.T, routes http.Handler, token, method, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	var result map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &result)
	return response.Code, result
}

// TestCandidateLibraryHTTP 验证用户真正操作新增、修改、备注和推荐时的边界。
func TestCandidateLibraryHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "candidate-library@example.com")
	status, _ := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "测试同学"})
	if status != http.StatusBadRequest {
		t.Fatalf("missing phone status=%d", status)
	}
	input := map[string]any{"candidate_name": "测试同学", "phone": "+86 138-0013-8000", "expected_salary_min": 10.5, "expected_salary_max": 20.75, "source_type": "manual", "idempotency_key": "create-one", "work_experiences": []map[string]any{{"company_name": "测试科技", "position_name": "工程师"}}}
	status, response := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", input)
	if status != http.StatusOK {
		t.Fatalf("create status=%d result=%v", status, response)
	}
	candidate := response["candidate"].(map[string]any)
	id := candidate["id"].(string)
	if candidate["phone"] != "13800138000" || candidate["expected_salary_min"] != 10.5 {
		t.Fatalf("normalized=%v", candidate)
	}
	status, retry := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", input)
	if status != http.StatusOK || retry["candidate"].(map[string]any)["id"] != id {
		t.Fatalf("retry status=%d result=%v", status, retry)
	}
	input["idempotency_key"] = "create-two"
	status, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", input)
	if status != http.StatusConflict {
		t.Fatalf("duplicate status=%d", status)
	}
	status, check := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/duplicates", map[string]any{"phone": "13800138000"})
	if status != http.StatusOK || len(check["matches"].([]any)) != 1 {
		t.Fatalf("duplicate lookup=%v", check)
	}
	status, response = candidateTestRequest(t, routes, token, http.MethodPatch, "/api/candidates/"+id, map[string]any{"row_version": candidate["row_version"], "personal_description": "人工确认的优势", "idempotency_key": "update-one"})
	if status != http.StatusOK {
		t.Fatalf("patch status=%d result=%v", status, response)
	}
	status, _ = candidateTestRequest(t, routes, token, http.MethodPatch, "/api/candidates/"+id, map[string]any{"row_version": candidate["row_version"], "personal_description": "旧窗口修改"})
	if status != http.StatusConflict {
		t.Fatalf("stale patch status=%d", status)
	}
	note := map[string]any{"content": "第一行\n第二行", "idempotency_key": "note-one"}
	status, first := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/"+id+"/notes", note)
	if status != http.StatusOK {
		t.Fatalf("note=%v", first)
	}
	_, second := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/"+id+"/notes", note)
	if first["note"].(map[string]any)["id"] != second["note"].(map[string]any)["id"] {
		t.Fatal("note retry created second event")
	}
	status, positionResponse := candidateTestRequest(t, routes, token, http.MethodPost, "/api/positions", map[string]any{"name": "简历库测试岗位"})
	if status != http.StatusOK {
		t.Fatalf("position=%v", positionResponse)
	}
	positionID := positionResponse["position"].(map[string]any)["id"].(string)
	recBody := map[string]any{"position_id": positionID, "stage": "recommended", "content": "符合岗位要求", "idempotency_key": "recommend-one"}
	status, rec := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/"+id+"/recommendations", recBody)
	if status != http.StatusOK {
		t.Fatalf("recommend=%v", rec)
	}
	_, rec = candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/"+id+"/recommendations", recBody)
	if len(rec["recommendation"].(map[string]any)["events"].([]any)) != 1 {
		t.Fatal("recommend retry duplicated event")
	}
	position, _ := server.positions.store.PositionByID("", "candidate-library@example.com", positionID, true)
	if position.ScannedCount != 0 || position.DailyGreetedCount != 0 {
		t.Fatal("manual recommendation polluted runtime counts")
	}
	status, list := candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates?position_id="+positionID, nil)
	if status != http.StatusOK || len(list["candidates"].([]any)) != 1 {
		t.Fatalf("recommendation filter=%v", list)
	}
	other := loginForTest(t, routes, "other-candidate-library@example.com")
	status, _ = candidateTestRequest(t, routes, other, http.MethodGet, "/api/candidates/"+id, nil)
	if status != http.StatusNotFound {
		t.Fatalf("cross tenant status=%d", status)
	}
}

// TestCandidateLibraryConcurrentAndVisibility 验证并发新增只建一份，成员查重不泄露他人资料。
func TestCandidateLibraryConcurrentAndVisibility(t *testing.T) {
	store := NewMemoryCandidateStore()
	scope := CandidateLibraryScope{TenantID: "team", UserEmail: "owner@example.com", AuthorName: "owner@example.com"}
	fields, _ := candidateMutationFields(map[string]any{"candidate_name": "隐私测试", "phone": "13900139000"}, true)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[string]bool{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := store.SaveLibraryCandidate(scope, "", fields, 0, "same-create")
			if err != nil {
				t.Errorf("concurrent create=%v", err)
				return
			}
			mu.Lock()
			ids[item.ID] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(ids) != 1 {
		t.Fatalf("created ids=%v", ids)
	}
	member := CandidateLibraryScope{TenantID: "team", UserEmail: "member@example.com"}
	matches, hidden, err := store.FindCandidateDuplicates(member, CandidateDuplicateQuery{Phone: "13900139000"})
	if err != nil || len(matches) != 0 || !hidden {
		t.Fatalf("hidden matches=%v hidden=%v err=%v", matches, hidden, err)
	}
	for id := range ids {
		if _, err := store.SaveLibraryCandidate(member, id, map[string]any{"personal_description": "越权修改"}, 1, ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross creator err=%v", err)
		}
	}
	unrelated := CandidateLibraryScope{TenantID: "other", UserEmail: "member@example.com"}
	matches, hidden, _ = store.FindCandidateDuplicates(unrelated, CandidateDuplicateQuery{Phone: "13900139000"})
	if len(matches) != 0 || hidden {
		t.Fatal("cross tenant duplicate leaked")
	}
}

// TestCandidateAutomaticProtectsManualFields 验证旧任务空手机号仍可入库，新来源复用人工简历时不覆盖已维护字段。
func TestCandidateAutomaticProtectsManualFields(t *testing.T) {
	store := NewMemoryCandidateStore()
	scope := CandidateLibraryScope{TenantID: "team", UserEmail: "owner@example.com"}
	fields, _ := candidateMutationFields(map[string]any{"candidate_name": "人工姓名", "phone": "13800138000", "platform_id": "liepin", "platform_candidate_id": "source-one", "work_experiences": []map[string]any{{"company_name": "人工公司"}}}, true)
	manual, err := store.SaveLibraryCandidate(scope, "", fields, 0, "manual")
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := store.SaveCandidateProfile(CandidateProfileInput{TenantID: "team", UserEmail: scope.UserEmail, PlatformID: "liepin", PlatformCandidateID: "source-one", CandidateName: "自动姓名", Phone: "", WorkExperiences: []CandidateWorkExperience{}})
	if err != nil || automatic.ID != manual.ID || automatic.CandidateName != "人工姓名" || automatic.Phone != "13800138000" || len(automatic.WorkExperiences) != 1 {
		t.Fatalf("protected=%+v err=%v", automatic, err)
	}
	legacy, err := store.SaveCandidateProfile(CandidateProfileInput{TenantID: "team", UserEmail: scope.UserEmail, PlatformID: "zhaopin", PlatformCandidateID: "blank-phone", CandidateName: "隐藏手机号"})
	if err != nil || legacy.ID == "" || legacy.Phone != "" {
		t.Fatalf("legacy=%+v err=%v", legacy, err)
	}
}

// TestCandidateMutationRejectsInvalidPhoneAndKeepsDecimals 验证掩码号码被拒绝、小数K完整保留。
func TestCandidateMutationRejectsInvalidPhoneAndKeepsDecimals(t *testing.T) {
	for _, phone := range []string{"138****8000", "123", "not-phone"} {
		if _, err := candidateMutationFields(map[string]any{"candidate_name": "测试", "phone": phone}, true); err == nil {
			t.Fatalf("invalid phone accepted=%s", phone)
		}
	}
	fields, err := candidateMutationFields(map[string]any{"candidate_name": "测试", "phone": "+1 202-555-0198", "expected_salary_min": 10.125}, true)
	if err != nil || fields["phone"] != "+12025550198" || fields["expected_salary_min"] != 10.125 {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
}
