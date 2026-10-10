// 本文件验证参考插件的结构化查重、严格与兜底分组、最早经历和隐藏记录隔离。
package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"
)

// candidateDuplicateFixtures 构造乱序经历和缺失字段，供内存与真实数据库共用相同查重证据。
func candidateDuplicateFixtures() (CandidateDuplicateQuery, []PositionCandidate) {
	query := CandidateDuplicateQuery{Name: "张**", Gender: "男", Age: 34,
		WorkExperiences: []CandidateWorkExperience{{CompanyName: "新公司", StartYM: "2023-01"}, {CompanyName: "第一公司", StartYM: "2015-06"}},
		Educations:      []CandidateEducation{{SchoolName: "后续学校", StartYM: "2013"}, {SchoolName: "最早大学", StartYM: "2009-09"}}}
	items := []PositionCandidate{}
	for _, id := range []string{"strict", "birth", "unknown-gender", "wrong-gender", "wrong-age", "missing-age", "wrong-year", "hidden", "foreign"} {
		item := PositionCandidate{ID: id, TenantID: "team", UserEmail: "owner@example.com", CandidateName: "张测试", BirthYM: fmt.Sprintf("%d-06", time.Now().Year()-34), LibraryFields: map[string]any{"gender": "male", "age": "34"}, WorkExperiences: append([]CandidateWorkExperience{}, query.WorkExperiences...), Educations: append([]CandidateEducation{}, query.Educations...), CreatedAt: time.Now().UTC()}
		switch id {
		case "birth":
			delete(item.LibraryFields, "age")
		case "unknown-gender":
			item.LibraryFields["gender"] = "未知"
		case "wrong-gender":
			item.LibraryFields["gender"] = "female"
		case "wrong-age":
			item.LibraryFields["age"] = "37"
		case "missing-age":
			item.LibraryFields["age"], item.BirthYM = "", ""
		case "wrong-year":
			item.Educations[1].StartYM = "2008"
		case "hidden":
			item.UserEmail = "member@example.com"
		case "foreign":
			item.TenantID = "other"
		}
		items = append(items, item)
	}
	return query, items
}

// candidateDuplicateIDs 归一化查重编号，避免同时间插入顺序影响断言。
func candidateDuplicateIDs(items []CandidateDuplicateMatch) string {
	ids := []string{}
	for _, item := range items {
		ids = append(ids, item.Candidate.ID)
	}
	sort.Strings(ids)
	return fmt.Sprint(ids)
}

// TestCandidateStructuredDuplicates 验证遮罩姓名按姓氏查重、年龄出生年、未知性别和最早经历全部条件。
func TestCandidateStructuredDuplicates(t *testing.T) {
	query, items := candidateDuplicateFixtures()
	scope := CandidateLibraryScope{TenantID: "team", UserEmail: "owner@example.com"}
	store := NewMemoryCandidateStore()
	for _, item := range items {
		store.profiles[item.ID] = item
	}
	matches, hidden, err := store.FindCandidateDuplicates(scope, query)
	if err != nil || hidden || candidateDuplicateIDs(matches) != "[birth strict unknown-gender]" {
		t.Fatalf("strict=%+v hidden=%v err=%v", matches, hidden, err)
	}
	for _, match := range matches {
		if match.MatchType != "possible" || fmt.Sprint(match.GroupKeys) != "[strict]" || len(match.Reasons) < 5 {
			t.Fatalf("missing reasons/group: %+v", match)
		}
	}
	criteria := candidateDuplicateCriteria(query)
	if criteria.Surname != "张" || criteria.AgeMin != 32 || criteria.AgeMax != 36 || criteria.EarliestCompanyYear != 2015 || criteria.EarliestSchoolYear != 2009 {
		t.Fatalf("criteria=%+v", criteria)
	}
	query.Keyword = "最早大学"
	matches, _, err = store.FindCandidateDuplicates(scope, query)
	if err != nil || candidateDuplicateIDs(matches) != "[birth strict unknown-gender]" || !slicesContainsString(matches[0].Reasons, "关键词相近：最早大学") {
		t.Fatalf("school keyword=%+v err=%v", matches, err)
	}
	query.Keyword = "不存在的职位"
	matches, _, err = store.FindCandidateDuplicates(scope, query)
	if err != nil || len(matches) != 0 {
		t.Fatalf("keyword did not narrow possible matches=%+v err=%v", matches, err)
	}
	exact := store.profiles["strict"]
	exact.PlatformID, exact.PlatformCandidateID = "liepin", "exact-source"
	store.profiles["strict"] = exact
	query.PlatformID, query.PlatformCandidateID = "liepin", "exact-source"
	matches, _, err = store.FindCandidateDuplicates(scope, query)
	if err != nil || candidateDuplicateIDs(matches) != "[strict]" || matches[0].MatchType != "exact" {
		t.Fatalf("keyword hid exact source=%+v err=%v", matches, err)
	}
	query.PlatformID, query.PlatformCandidateID, query.Keyword = "", "", ""
	query.WorkExperiences[1].CompanyName = "未出现的公司"
	matches, hidden, err = store.FindCandidateDuplicates(scope, query)
	if err != nil || hidden || candidateDuplicateIDs(matches) != "[birth strict unknown-gender]" {
		t.Fatalf("school fallback=%+v err=%v", matches, err)
	}
	for _, match := range matches {
		if fmt.Sprint(match.GroupKeys) != "[surname_gender_school]" {
			t.Fatalf("wrong fallback group: %+v", match)
		}
	}
	query.Educations[1].SchoolName = "未出现的学校"
	query.WorkExperiences[1].CompanyName = "第一公司"
	matches, _, err = store.FindCandidateDuplicates(scope, query)
	if err != nil || candidateDuplicateIDs(matches) != "[birth strict unknown-gender wrong-year]" {
		t.Fatalf("company fallback=%+v err=%v", matches, err)
	}
	query.Phone = "13800138000"
	hiddenItem := store.profiles["hidden"]
	hiddenItem.Phone = query.Phone
	store.profiles["hidden"] = hiddenItem
	matches, hidden, err = store.FindCandidateDuplicates(scope, query)
	if err != nil || !hidden || len(matches) != 0 {
		t.Fatalf("hidden exact leaked: %+v hidden=%v err=%v", matches, hidden, err)
	}
	if len(store.profiles) != len(items) {
		t.Fatal("fuzzy lookup mutated profiles")
	}
}

// TestCandidateStructuredDuplicateHTTP 验证插件平铺请求和分组响应经过真实认证路由。
func TestCandidateStructuredDuplicateHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "structured-duplicate@example.com")
	status, created := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "张测试", "phone": "13800138000", "gender": "male", "age": 34, "educations": []map[string]any{{"school_name": "最早大学", "start_ym": "2009"}}, "work_experiences": []map[string]any{{"company_name": "第一公司", "start_ym": "2015"}}})
	if status != http.StatusOK {
		t.Fatalf("create=%v", created)
	}
	status, result := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/duplicates", map[string]any{"candidate_name": "张**", "gender": "男", "age": time.Now().Year() - 34, "educations": []map[string]any{{"school_name": "最早大学", "start_ym": "2009"}}, "work_experiences": []map[string]any{{"company_name": "第一公司", "start_ym": "2015"}}})
	if status != http.StatusOK || len(result["matches"].([]any)) != 1 || len(result["groups"].([]any)) != 1 || result["criteria"].(map[string]any)["age"] != float64(34) {
		t.Fatalf("flat structured response=%v status=%d", result, status)
	}
	if result["matches"].([]any)[0].(map[string]any)["match_type"] != "possible" {
		t.Fatal("structured match treated as exact")
	}
	for _, input := range []map[string]any{{"age": 34.5}, {"age": 0}, {"work_experiences": "wrong"}} {
		status, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/duplicates", input)
		if status != http.StatusBadRequest {
			t.Fatalf("invalid query accepted=%v status=%d", input, status)
		}
	}
}
