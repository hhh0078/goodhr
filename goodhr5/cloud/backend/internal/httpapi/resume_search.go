// 本文件通过 Elasticsearch 进行受权限预过滤的 BM25 与两路向量检索，并在应用层融合名次。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// resumeSearchHit 是搜索副本的版本引用，展示前必须再次读取当前业务库权限与正文。
type resumeSearchHit struct {
	CandidateID   string              `json:"candidate_id"`
	ResumeVersion int64               `json:"resume_version"`
	Score         float64             `json:"score"`
	Evidence      []resumeVectorChunk `json:"evidence"`
}

// resumeSearchIndex 隔离可替换的检索实现，生产不允许静默降级为内存全库扫描。
type resumeSearchIndex interface {
	Ready() bool
	Index(context.Context, resumeVectorRecord) error
	Search(context.Context, CandidateLibraryScope, string, []float64) ([]resumeSearchHit, error)
}

// elasticResumeSearch 使用部署配置中的受信搜索地址，不接受模型提交的 URL 或搜索 DSL。
type elasticResumeSearch struct {
	base, key, index string
	client           *http.Client
	mu               sync.Mutex
	initialized      bool
}

// newResumeSearch 从独立环境变量读取搜索连接，不复用或暴露用户 AI Key。
func newResumeSearch() *elasticResumeSearch {
	return &elasticResumeSearch{base: strings.TrimRight(os.Getenv("GOODHR_SEARCH_URL"), "/"), key: os.Getenv("GOODHR_SEARCH_API_KEY"), index: "goodhr_resume_qwen37_v1", client: &http.Client{Timeout: 45 * time.Second}}
}

// Ready 只检查管理员配置是否完整，实际可达性由有时限的请求验证。
func (s *elasticResumeSearch) Ready() bool {
	u, err := url.Parse(s.base)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}

// request 有界读取搜索响应，错误只带状态码，不输出密钥或简历正文。
func (s *elasticResumeSearch) request(ctx context.Context, method, path string, input any) ([]byte, int, error) {
	if !s.Ready() {
		return nil, 0, errors.New("高级搜索服务尚未配置，请联系管理员")
	}
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			return nil, 0, err
		}
	}
	return s.rawRequest(ctx, method, path, body, "application/json")
}

// rawRequest 发送固定搜索操作，拒绝超出响应上限的数据。
func (s *elasticResumeSearch) rawRequest(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, method, s.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Content-Type", contentType)
	if s.key != "" {
		request.Header.Set("Authorization", "ApiKey "+s.key)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, 0, errors.New("高级搜索服务暂时没连上，请稍后再试")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
	if err != nil || len(data) > 16<<20 {
		return nil, response.StatusCode, errors.New("搜索响应过大或未读完整")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return data, response.StatusCode, fmt.Errorf("搜索服务返回状态 %d", response.StatusCode)
	}
	return data, response.StatusCode, nil
}

// ensureIndex 首次创建固定模型空间；模型与维度变化必须使用新的索引名。
func (s *elasticResumeSearch) ensureIndex(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return nil
	}
	properties := map[string]any{}
	for _, field := range []string{"tenant_id", "owner_email", "candidate_id", "kind", "path", "profile"} {
		properties[field] = map[string]string{"type": "keyword"}
	}
	properties["education_rank"] = map[string]string{"type": "integer"}
	properties["expected_salary_min"] = map[string]string{"type": "double"}
	properties["resume_version"] = map[string]string{"type": "long"}
	properties["text"] = map[string]string{"type": "text", "analyzer": "cjk"}
	properties["vector"] = map[string]any{"type": "dense_vector", "dims": 1024, "index": true, "similarity": "cosine"}
	body, status, err := s.request(ctx, http.MethodPut, "/"+s.index, map[string]any{"mappings": map[string]any{"dynamic": "strict", "_meta": map[string]string{"profile": resumeVectorProfile}, "properties": properties}})
	if err != nil && !(status == 400 && bytes.Contains(body, []byte("resource_already_exists_exception"))) {
		return err
	}
	mapping, _, err := s.request(ctx, http.MethodGet, "/"+s.index+"/_mapping", nil)
	if err != nil {
		return err
	}
	var result map[string]struct {
		Mappings struct {
			Meta struct {
				Profile string `json:"profile"`
			} `json:"_meta"`
			Properties map[string]struct {
				Dims int `json:"dims"`
			} `json:"properties"`
		} `json:"mappings"`
	}
	if json.Unmarshal(mapping, &result) != nil || result[s.index].Mappings.Meta.Profile != resumeVectorProfile || result[s.index].Mappings.Properties["vector"].Dims != 1024 {
		return errors.New("搜索索引的模型或向量维度不匹配，请建立新索引")
	}
	s.initialized = true
	return nil
}

// Index 用外部行版本写入分片，旧任务不能覆盖新简历；只有全部分片成功后业务回执才完成。
func (s *elasticResumeSearch) Index(ctx context.Context, item resumeVectorRecord) error {
	if err := s.ensureIndex(ctx); err != nil {
		return err
	}
	var body bytes.Buffer
	for _, chunk := range item.Chunks {
		meta := map[string]any{"index": map[string]any{"_index": s.index, "_id": item.CandidateID + ":" + chunk.ID, "version": item.ResumeVersion, "version_type": "external_gte"}}
		doc := map[string]any{"tenant_id": item.TenantID, "owner_email": item.OwnerEmail, "candidate_id": item.CandidateID, "resume_version": item.ResumeVersion, "kind": chunk.Kind, "path": chunk.Path, "profile": resumeVectorProfile, "text": chunk.Text, "vector": chunk.Vector}
		for _, field := range []string{"education_rank", "expected_salary_min"} {
			if value, ok := item.Facts[field]; ok {
				doc[field] = value
			}
		}
		if err := json.NewEncoder(&body).Encode(meta); err != nil {
			return err
		}
		if err := json.NewEncoder(&body).Encode(doc); err != nil {
			return err
		}
	}
	raw, _, err := s.rawRequest(ctx, http.MethodPost, "/_bulk?refresh=wait_for", body.Bytes(), "application/x-ndjson")
	if err != nil {
		return err
	}
	var result struct {
		Errors *bool `json:"errors"`
		Items  []map[string]struct {
			ID     string          `json:"_id"`
			Status int             `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Errors == nil || *result.Errors || len(result.Items) != len(item.Chunks) {
		return errors.New("部分简历片段未写入搜索索引，请重试同步")
	}
	for i, entry := range result.Items {
		value, ok := entry["index"]
		if !ok || len(entry) != 1 || value.ID != item.CandidateID+":"+item.Chunks[i].ID || value.Status < 200 || value.Status >= 300 || len(value.Error) > 0 && !bytes.Equal(bytes.TrimSpace(value.Error), []byte("null")) {
			return errors.New("搜索索引写入回执未核对，请重试同步")
		}
	}
	raw, _, err = s.request(ctx, http.MethodPost, "/"+s.index+"/_delete_by_query?conflicts=proceed&refresh=true", map[string]any{"query": map[string]any{"bool": map[string]any{"filter": []any{map[string]any{"term": map[string]string{"tenant_id": item.TenantID}}, map[string]any{"term": map[string]string{"candidate_id": item.CandidateID}}, map[string]any{"range": map[string]any{"resume_version": map[string]int64{"lt": item.ResumeVersion}}}}}}})
	if err != nil {
		return err
	}
	var cleanup struct {
		TimedOut *bool             `json:"timed_out"`
		Failures []json.RawMessage `json:"failures"`
	}
	if json.Unmarshal(raw, &cleanup) != nil || cleanup.TimedOut == nil || *cleanup.TimedOut || len(cleanup.Failures) > 0 {
		return errors.New("旧简历片段清理未完整完成，请重试同步")
	}
	return nil
}

// Search 同时执行关键词、职业概况向量和经历片段向量检索，权限过滤在召回前生效。
func (s *elasticResumeSearch) Search(ctx context.Context, scope CandidateLibraryScope, text string, vector []float64) ([]resumeSearchHit, error) {
	return s.SearchFiltered(ctx, scope, text, vector, nil)
}

// SearchFiltered 在三路召回开始前应用已经核验来源的必要条件。
func (s *elasticResumeSearch) SearchFiltered(ctx context.Context, scope CandidateLibraryScope, text string, vector []float64, conditions []resumeSearchFilter) ([]resumeSearchHit, error) {
	if err := s.ensureIndex(ctx); err != nil {
		return nil, err
	}
	filter := []any{map[string]any{"term": map[string]string{"tenant_id": scope.TenantID}}, map[string]any{"term": map[string]string{"profile": resumeVectorProfile}}}
	if !scope.IsAdmin {
		filter = append(filter, map[string]any{"term": map[string]string{"owner_email": scope.UserEmail}})
	}
	preFilters, err := resumeSearchPreFilters(conditions)
	if err != nil {
		return nil, err
	}
	filter = append(filter, preFilters...)
	queries := []map[string]any{{"size": 500, "_source": []string{"candidate_id", "resume_version", "kind", "path", "text"}, "collapse": map[string]string{"field": "candidate_id"}, "query": map[string]any{"bool": map[string]any{"filter": filter, "must": []any{map[string]any{"match": map[string]any{"text": map[string]any{"query": text, "operator": "or"}}}}}}}}
	for _, kind := range []string{"summary", "experience"} {
		filters := append(append([]any{}, filter...), map[string]any{"term": map[string]string{"kind": kind}})
		queries = append(queries, map[string]any{"size": 500, "_source": []string{"candidate_id", "resume_version", "kind", "path", "text"}, "collapse": map[string]string{"field": "candidate_id"}, "knn": map[string]any{"field": "vector", "query_vector": vector, "k": 2000, "num_candidates": 4000, "filter": map[string]any{"bool": map[string]any{"filter": filters}}}})
	}
	type routeResult struct {
		hits []resumeSearchHit
		err  error
	}
	results := make(chan routeResult, 3)
	for _, query := range queries {
		go func(input map[string]any) {
			raw, _, err := s.request(ctx, http.MethodPost, "/"+s.index+"/_search", input)
			if err != nil {
				results <- routeResult{err: err}
				return
			}
			var response struct {
				TimedOut bool `json:"timed_out"`
				Shards   struct {
					Failed int `json:"failed"`
				} `json:"_shards"`
				Hits struct {
					Hits []struct {
						Source struct {
							CandidateID string `json:"candidate_id"`
							Version     int64  `json:"resume_version"`
							Kind        string `json:"kind"`
							Path        string `json:"path"`
							Text        string `json:"text"`
						} `json:"_source"`
					} `json:"hits"`
				} `json:"hits"`
			}
			if json.Unmarshal(raw, &response) != nil || response.TimedOut || response.Shards.Failed > 0 {
				results <- routeResult{err: errors.New("检索未完整完成，请稍后再试")}
				return
			}
			hits := []resumeSearchHit{}
			for _, hit := range response.Hits.Hits {
				hits = append(hits, resumeSearchHit{CandidateID: hit.Source.CandidateID, ResumeVersion: hit.Source.Version, Evidence: []resumeVectorChunk{{Path: hit.Source.Path, Text: hit.Source.Text, Kind: hit.Source.Kind}}})
			}
			results <- routeResult{hits: hits}
		}(query)
	}
	routes := [][]resumeSearchHit{}
	for range queries {
		result := <-results
		if result.err != nil {
			return nil, result.err
		}
		routes = append(routes, result.hits)
	}
	return fuseResumeRanks(routes, 1000), nil
}

// fuseResumeRanks 使用固定 RRF 名次融合，先在每路按候选人去重，禁止混加不同量纲的原始分数。
func fuseResumeRanks(routes [][]resumeSearchHit, limit int) []resumeSearchHit {
	items := map[string]*resumeSearchHit{}
	latest := map[string]int64{}
	for _, route := range routes {
		for _, hit := range route {
			if hit.ResumeVersion > latest[hit.CandidateID] {
				latest[hit.CandidateID] = hit.ResumeVersion
			}
		}
	}
	for _, route := range routes {
		seen := map[string]bool{}
		rank := 0
		for _, hit := range route {
			if hit.CandidateID == "" || hit.ResumeVersion != latest[hit.CandidateID] || seen[hit.CandidateID] {
				continue
			}
			seen[hit.CandidateID] = true
			rank++
			old := items[hit.CandidateID]
			if old == nil {
				copy := hit
				copy.Score = 0
				copy.Evidence = nil
				old = &copy
				items[hit.CandidateID] = old
			}
			if hit.ResumeVersion != old.ResumeVersion {
				continue
			}
			old.Score += 1 / float64(60+rank)
			old.Evidence = append(old.Evidence, hit.Evidence...)
		}
	}
	result := []resumeSearchHit{}
	for _, item := range items {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].CandidateID < result[j].CandidateID
		}
		return result[i].Score > result[j].Score
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}
