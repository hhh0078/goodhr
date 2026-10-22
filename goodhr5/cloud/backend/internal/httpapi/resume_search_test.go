// 本文件使用本机HTTP桩验证搜索三路权限预过滤、未知保留、模型空间、版本写入回执与RRF去重，不连接外部ES。
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// searchIndexForTest 提供可控模型空间；具体搜索和写入路径由测试处理，避免将未知响应默认为成功。
func searchIndexForTest(t *testing.T, profile string, dimensions int, handle http.HandlerFunc) *elasticResumeSearch {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut && r.URL.Path == "/test_index" {
			_, _ = io.WriteString(w, `{"acknowledged":true}`)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/test_index/_mapping" {
			_ = json.NewEncoder(w).Encode(map[string]any{"test_index": map[string]any{"mappings": map[string]any{"_meta": map[string]any{"profile": profile}, "properties": map[string]any{"vector": map[string]any{"dims": dimensions}}}}})
			return
		}
		handle(w, r)
	}))
	t.Cleanup(server.Close)
	return &elasticResumeSearch{base: server.URL, index: "test_index", client: server.Client()}
}

// TestResumeSearchPreFiltersAllRoutes 检查BM25及两路KNN在召回前都带租户、模型、本人范围和未知保留分支。
func TestResumeSearchPreFiltersAllRoutes(t *testing.T) {
	var mu sync.Mutex
	queries := []map[string]any{}
	index := searchIndexForTest(t, resumeVectorProfile, 1024, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/test_index/_search" {
			http.NotFound(w, r)
			return
		}
		var query map[string]any
		if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		queries = append(queries, query)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"timed_out":false,"_shards":{"failed":0},"hits":{"hits":[]}}`)
	})
	conditions := []resumeSearchFilter{{Field: "education_min", Op: "gte", Value: "本科"}, {Field: "expected_salary_max", Op: "lte", Value: "30000"}}
	scope := CandidateLibraryScope{TenantID: "tenant-a", UserEmail: "member@example.test", IsAdmin: false}
	if _, err := index.SearchFiltered(context.Background(), scope, "Go 工程师", []float64{1}, conditions); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(queries) != 3 {
		t.Fatalf("三路召回请求数=%d", len(queries))
	}
	kinds := map[string]bool{}
	for _, query := range queries {
		var filters []any
		if knn, ok := query["knn"].(map[string]any); ok {
			filters = knn["filter"].(map[string]any)["bool"].(map[string]any)["filter"].([]any)
			kinds[filters[len(filters)-1].(map[string]any)["term"].(map[string]any)["kind"].(string)] = true
		} else {
			filters = query["query"].(map[string]any)["bool"].(map[string]any)["filter"].([]any)
			kinds["keyword"] = true
		}
		raw, _ := json.Marshal(filters)
		for _, part := range []string{`"tenant_id":"tenant-a"`, `"profile":"` + resumeVectorProfile + `"`, `"owner_email":"member@example.test"`, `"education_rank":{"gte":5}`, `"expected_salary_min":{"lte":30000}`, `"minimum_should_match":1`, `"must_not":[{"exists":{"field":"education_rank"}}]`, `"must_not":[{"exists":{"field":"expected_salary_min"}}]`} {
			if !strings.Contains(string(raw), part) {
				t.Fatalf("召回遗漏过滤 %s: %s", part, raw)
			}
		}
		if query["collapse"].(map[string]any)["field"] != "candidate_id" {
			t.Fatal("多片段没有在召回中按候选人去重")
		}
	}
	if !kinds["keyword"] || !kinds["summary"] || !kinds["experience"] {
		t.Fatal("没有覆盖三种召回")
	}
}

// TestResumeSearchAdminAndInvalidFilters 验证管理员仍限定租户；非法字段不产生任何搜索请求。
func TestResumeSearchAdminAndInvalidFilters(t *testing.T) {
	var calls atomic.Int32
	index := searchIndexForTest(t, resumeVectorProfile, 1024, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "owner_email") || !strings.Contains(string(raw), `"tenant_id":"team"`) {
			t.Error("管理员范围不正确")
		}
		_, _ = io.WriteString(w, `{"hits":{"hits":[]}}`)
	})
	if _, err := index.Search(context.Background(), CandidateLibraryScope{TenantID: "team", IsAdmin: true}, "query", []float64{1}); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []resumeSearchFilter{{Field: "gender", Op: "eq", Value: "男"}, {Field: "education_min", Op: "eq", Value: "本科"}, {Field: "expected_salary_max", Op: "lte", Value: "-1"}, {Field: "expected_salary_max", Op: "lte", Value: "NaN"}, {Field: "expected_salary_max", Op: "lte", Value: "+Inf"}} {
		if _, err := index.SearchFiltered(context.Background(), CandidateLibraryScope{TenantID: "team"}, "query", nil, []resumeSearchFilter{filter}); err == nil {
			t.Fatal("非法预过滤被执行")
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("非法条件仍发送了搜索请求: %d", calls.Load())
	}
}

// TestResumeSearchModelSpace 拒绝同名索引使用旧模型或错误向量维度。
func TestResumeSearchModelSpace(t *testing.T) {
	for _, item := range []struct {
		profile string
		dims    int
	}{{"old-profile", 1024}, {resumeVectorProfile, 768}} {
		t.Run(fmt.Sprintf("%s-%d", item.profile, item.dims), func(t *testing.T) {
			index := searchIndexForTest(t, item.profile, item.dims, func(w http.ResponseWriter, r *http.Request) {
				t.Error("错误模型空间不应执行检索")
				http.NotFound(w, r)
			})
			if _, err := index.Search(context.Background(), CandidateLibraryScope{}, "query", nil); err == nil {
				t.Fatal("错误模型空间仍被使用")
			}
		})
	}
}

// TestResumeSearchIndexReceipts 验证每片段外部版本、数据范围及完整bulk回执，异常回执禁止标记成功。
func TestResumeSearchIndexReceipts(t *testing.T) {
	validBulk := `{"errors":false,"items":[{"index":{"_id":"candidate:chunk","status":201}}]}`
	for _, item := range []struct {
		name, bulk, cleanup string
		wantErr             bool
	}{
		{"valid", validBulk, `{"timed_out":false,"failures":[]}`, false},
		{"empty_bulk", `{}`, ``, true},
		{"missing_items", `{"errors":false,"items":[]}`, ``, true},
		{"wrong_id", `{"errors":false,"items":[{"index":{"_id":"other","status":201}}]}`, ``, true},
		{"failed_item", `{"errors":false,"items":[{"index":{"_id":"candidate:chunk","status":409}}]}`, ``, true},
		{"hidden_item_error", `{"errors":false,"items":[{"index":{"_id":"candidate:chunk","status":201,"error":{"type":"failure"}}}]}`, ``, true},
		{"timed_out_cleanup", validBulk, `{"timed_out":true,"failures":[]}`, true},
		{"failed_cleanup", validBulk, `{"timed_out":false,"failures":[{"reason":"failed"}]}`, true},
		{"missing_cleanup", validBulk, `{}`, true},
	} {
		t.Run(item.name, func(t *testing.T) {
			index := searchIndexForTest(t, resumeVectorProfile, 1024, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				switch r.URL.Path {
				case "/_bulk":
					lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
					if len(lines) != 2 {
						t.Error("bulk不包含完整片段")
					}
					for _, part := range []string{`"_id":"candidate:chunk"`, `"version":7`, `"version_type":"external_gte"`, `"tenant_id":"tenant"`, `"owner_email":"owner"`, `"resume_version":7`, `"education_rank":5`, `"profile":"` + resumeVectorProfile + `"`} {
						if !strings.Contains(string(raw), part) {
							t.Errorf("bulk缺少%s", part)
						}
					}
					_, _ = io.WriteString(w, item.bulk)
				case "/test_index/_delete_by_query":
					for _, part := range []string{`"candidate_id":"candidate"`, `"tenant_id":"tenant"`, `"resume_version":{"lt":7}`} {
						if !strings.Contains(string(raw), part) {
							t.Errorf("旧版本清理缺少范围%s", part)
						}
					}
					_, _ = io.WriteString(w, item.cleanup)
				default:
					http.NotFound(w, r)
				}
			})
			err := index.Index(context.Background(), resumeVectorRecord{CandidateID: "candidate", TenantID: "tenant", OwnerEmail: "owner", ResumeVersion: 7, Facts: map[string]any{"education_rank": 5}, Chunks: []resumeVectorChunk{{ID: "chunk", Kind: "summary", Text: "工程师", Vector: []float64{1}}}})
			if (err != nil) != item.wantErr {
				t.Fatalf("写入回执错误=%v wantErr=%v", err, item.wantErr)
			}
		})
	}
}

// TestResumeSearchRRFVersionAndDedup 验证相同候选人只按每路计一次，旧版本不覆盖新版本，返回顺序与路由完成顺序无关。
func TestResumeSearchRRFVersionAndDedup(t *testing.T) {
	hit := func(id string, version int64, text string) resumeSearchHit {
		return resumeSearchHit{CandidateID: id, ResumeVersion: version, Score: 99999, Evidence: []resumeVectorChunk{{Text: text}}}
	}
	routes := [][]resumeSearchHit{{hit("a", 1, "old"), hit("a", 1, "old duplicate"), hit("b", 1, "b")}, {hit("a", 2, "new"), hit("a", 2, "duplicate"), hit("b", 1, "b2")}, {hit("a", 2, "new2")}}
	got := fuseResumeRanks(routes, 100)
	reversed := fuseResumeRanks([][]resumeSearchHit{routes[2], routes[1], routes[0]}, 100)
	if len(got) != 2 || got[0].CandidateID != "a" || got[0].ResumeVersion != 2 || math.Abs(got[0].Score-2.0/61) > 1e-10 {
		t.Fatalf("RRF名次异常=%+v", got)
	}
	if got[0].CandidateID != reversed[0].CandidateID || got[0].ResumeVersion != reversed[0].ResumeVersion || got[0].Score != reversed[0].Score {
		t.Fatal("路由完成顺序影响候选版本或分数")
	}
	texts := []string{}
	for _, chunk := range got[0].Evidence {
		texts = append(texts, chunk.Text)
	}
	if !reflect.DeepEqual(texts, []string{"new", "new2"}) {
		t.Fatalf("旧版本或重复片段进入证据: %v", texts)
	}
	if limited := fuseResumeRanks(routes, 1); len(limited) != 1 {
		t.Fatal("融合忽略结果预算")
	}
}
