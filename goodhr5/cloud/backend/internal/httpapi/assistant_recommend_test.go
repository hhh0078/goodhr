// 本文件使用固定合成岗位、简历和模型响应验证推荐真实调用链与保守规则，不连接真实付费模型或生产数据。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recommendationSearchFixture 保存合成召回引用并记录实际调用的权限与白名单过滤。
type recommendationSearchFixture struct {
	hits    []resumeSearchHit
	filters []resumeSearchFilter
	scope   CandidateLibraryScope
}

// Ready 声明合成搜索已准备完成。
func (s *recommendationSearchFixture) Ready() bool { return true }

// Index 测试中不写入任何真实索引。
func (s *recommendationSearchFixture) Index(context.Context, resumeVectorRecord) error { return nil }

// Search 返回固定合成候选编号，供后端重新读取真实测试简历。
func (s *recommendationSearchFixture) Search(_ context.Context, scope CandidateLibraryScope, _ string, _ []float64) ([]resumeSearchHit, error) {
	s.scope = scope
	return s.hits, nil
}

// SearchFiltered 记录后端白名单过滤，验证优先条件不会成为硬过滤。
func (s *recommendationSearchFixture) SearchFiltered(ctx context.Context, scope CandidateLibraryScope, text string, vector []float64, filters []resumeSearchFilter) ([]resumeSearchHit, error) {
	s.filters = filters
	return s.Search(ctx, scope, text, vector)
}

// recommendationChatResponse 创建带可靠用量的合成JSON聊天响应。
func recommendationChatResponse(value any) *http.Response {
	content, _ := json.Marshal(value)
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150}})
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}
}

// TestAssistantRequirementsPreferredUnknownAndFilters 验证优先不变必要、无来源不变明确、受保护条件剔除与过滤白名单。
func TestAssistantRequirementsPreferredUnknownAndFilters(t *testing.T) {
	result := assistantRequirements{Requirements: []assistantRequirement{{Text: "检索经验", Type: "must_have", Source: "检索经验优先", Explicit: true, Filter: &assistantRequirementFilter{Field: "certificate", Op: "eq", Value: "检索"}}, {Text: "本科", Type: "must_have", Source: "本科及以上", Explicit: true, Filter: &assistantRequirementFilter{Field: "education_min", Op: "gte", Value: "本科"}}, {Text: "必须管理10人", Type: "must_have", Source: "模型猜测", Explicit: true}, {Text: "年龄30岁以下", Type: "must_have", Source: "年龄30岁以下", Explicit: true}}}
	if err := validateAssistantRequirements(&result, "本科及以上；检索经验优先；年龄30岁以下"); err != nil {
		t.Fatal(err)
	}
	if len(result.Requirements) != 3 || result.Requirements[0].Type != "preferred" || result.Requirements[2].Type != "unresolved" {
		t.Fatalf("requirements=%+v", result.Requirements)
	}
	filters, err := assistantReliableFilters(result.Requirements, HeadhunterJob{})
	if err != nil || len(filters) != 1 || filters[0].Value != "本科" {
		t.Fatalf("filters=%+v err=%v", filters, err)
	}
	result.Requirements[1].Filter.Value = "7"
	if _, err = assistantReliableFilters(result.Requirements, HeadhunterJob{}); err == nil {
		t.Fatal("numeric education bypass accepted")
	}
}

// TestAssistantEvidenceQuotesAndUnknownClassification 验证引文必须逐字支持当前版本，必要条件未知单列而非排除。
func TestAssistantEvidenceQuotesAndUnknownClassification(t *testing.T) {
	requirements := []assistantRequirement{{ID: "r1", Text: "Go经验", Type: "must_have"}, {ID: "r2", Text: "搜索经验", Type: "preferred"}}
	chunks := []resumeVectorChunk{{ID: "chunk", Path: "work_experiences[0].content", Text: "参与Go检索服务开发"}}
	item := assistantEvaluation{CandidateID: "c1", ResumeVersion: 2, RequirementsVersion: "v1", RequirementChecks: []assistantRequirementCheck{{RequirementID: "r1", Status: "unknown", EvidenceRefs: []string{}}, {RequirementID: "r2", Status: "partial", EvidenceRefs: []string{"chunk"}}}, EvidenceRefs: []assistantEvidenceRef{{RequirementID: "r2", ChunkID: "chunk", Path: chunks[0].Path, Quote: "参与Go检索服务开发"}}}
	if err := validateAssistantEvaluation(&item, "c1", 2, "v1", requirements, chunks); err != nil {
		t.Fatal(err)
	}
	if status := classifyAssistantEvaluation(&item, requirements); status != "pending" {
		t.Fatalf("unknown classified as %s", status)
	}
	item.EvidenceRefs[0].Quote = "主导Go检索服务开发"
	if err := validateAssistantEvaluation(&item, "c1", 2, "v1", requirements, chunks); err == nil {
		t.Fatal("invented responsibility accepted")
	}
	item.EvidenceRefs[0].Quote = chunks[0].Text
	item.EvidenceRefs[0].ChunkID = "another-candidate"
	if err := validateAssistantEvaluation(&item, "c1", 2, "v1", requirements, chunks); err == nil {
		t.Fatal("foreign evidence accepted")
	}
	item.EvidenceRefs[0].ChunkID = "chunk"
	item.RequirementChecks[0].Status = "met"
	item.RequirementChecks[0].EvidenceRefs = []string{}
	if err := validateAssistantEvaluation(&item, "c1", 2, "v1", requirements, chunks); err == nil {
		t.Fatal("met without evidence accepted")
	}
}

// TestAssistantChunkSelectionIncludesLateExperience 验证上下文预算选择能找到后半段关键经历，不机械截取前N字。
func TestAssistantChunkSelectionIncludesLateExperience(t *testing.T) {
	chunks := []resumeVectorChunk{}
	for index := 0; index < 15; index++ {
		chunks = append(chunks, resumeVectorChunk{ID: strconvForTest(index), Path: "raw_text", Text: strings.Repeat("普通内容", 200)})
	}
	chunks = append(chunks, resumeVectorChunk{ID: "late", Path: "work_experiences[9].content", Text: "使用Go参与检索平台开发"})
	selected, truncated := selectAssistantChunks(chunks, []assistantRequirement{{Text: "Go 检索"}}, nil, 1600)
	found := false
	for _, chunk := range selected {
		if chunk.ID == "late" {
			found = true
		}
	}
	if !found || !truncated {
		t.Fatalf("late found=%v truncated=%v", found, truncated)
	}
}

// strconvForTest 返回短的测试编号，不依赖候选人真实标识。
func strconvForTest(value int) string { return string(rune('a' + value)) }

// TestAssistantRecommendationJSONRetriesWithHistory 验证非法JSON会连同历史回馈模型，最多五次且每次保留真实用量扣费。
func TestAssistantRecommendationJSONRetriesWithHistory(t *testing.T) {
	calls := 0
	wallet, _ := embeddingFixture(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		var input struct {
			Messages []AIMsg           `json:"messages"`
			Format   map[string]string `json:"response_format"`
		}
		if json.NewDecoder(request.Body).Decode(&input) != nil || input.Format["type"] != "json_object" {
			t.Fatal("missing JSON response format")
		}
		if calls > 1 && (len(input.Messages) < 4 || !strings.Contains(input.Messages[len(input.Messages)-1].Content, "校验错误")) {
			t.Fatal("retry lost original response or error history")
		}
		raw := `{"choices":[{"message":{"content":"not json"}}],"usage":{"prompt_tokens":100,"completion_tokens":50}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Header: make(http.Header)}, nil
	}))
	s := &AssistantService{wallet: wallet}
	budget := &recommendationBudget{}
	var result struct {
		Value string `json:"value"`
	}
	err := s.recommendationJSON(context.Background(), CandidateLibraryScope{UserEmail: "embedding@example.com"}, budget, "只返回value", map[string]string{"input": "测试"}, &result, nil)
	if err == nil || calls != 5 || budget.Calls != 5 || budget.PromptTokens != 500 {
		t.Fatalf("calls=%d budget=%+v err=%v", calls, budget, err)
	}
	var fatal assistantFatalError
	if !errors.As(err, &fatal) {
		t.Fatal("inner five-attempt exhaustion could be retried by outer conversation")
	}
}

// TestAssistantRecommendationFlowUsesCurrentEvidence 验证完整JD、向量召回、精排、独立证据复核、过期跳过及推荐与待确认分池。
func TestAssistantRecommendationFlowUsesCurrentEvidence(t *testing.T) {
	t.Setenv("GOODHR_RERANK_URL", "")
	candidateStore := NewMemoryCandidateStore()
	tenants := NewMemoryTenantStore()
	tenant, err := tenants.GetOrCreateTenant("embedding@example.com")
	if err != nil {
		t.Fatal(err)
	}
	scope := CandidateLibraryScope{TenantID: tenant.ID, UserEmail: "embedding@example.com", IsAdmin: true}
	for _, id := range []string{"ready", "pending", "stale"} {
		candidateStore.profiles[id] = PositionCandidate{ID: id, TenantID: tenant.ID, UserEmail: scope.UserEmail, CandidateName: "合成姓名不许传模型", Phone: "13800138000", Email: "private@example.com", RowVersion: 1, RawText: "使用Go参与检索服务开发"}
	}
	stale := candidateStore.profiles["stale"]
	stale.RowVersion = 2
	candidateStore.profiles["stale"] = stale
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/embeddings") {
			return embeddingHTTPResponse(embeddingResultForTest(0)), nil
		}
		var input struct {
			Messages []AIMsg `json:"messages"`
		}
		if json.NewDecoder(request.Body).Decode(&input) != nil {
			t.Fatal("chat input invalid")
		}
		if strings.Contains(input.Messages[len(input.Messages)-1].Content, "合成姓名不许传模型") || strings.Contains(input.Messages[len(input.Messages)-1].Content, "13800138000") {
			t.Fatal("candidate PII reached model")
		}
		prompt := input.Messages[0].Content
		var data map[string]json.RawMessage
		json.Unmarshal([]byte(input.Messages[1].Content), &data)
		switch {
		case strings.Contains(prompt, "岗位条件解析"):
			return recommendationChatResponse(assistantRequirements{Requirements: []assistantRequirement{{Text: "熟悉Go", Type: "must_have", Source: "熟悉Go", Explicit: true}}}), nil
		case strings.Contains(prompt, "语义精排"):
			return recommendationChatResponse(map[string]any{"scores": []ResumeRerankScore{{CandidateID: "ready", Score: 90}, {CandidateID: "pending", Score: 80}}}), nil
		case strings.Contains(prompt, "逐条证据评估"):
			var id, version string
			var rowVersion int64
			var chunks []resumeVectorChunk
			json.Unmarshal(data["candidate_id"], &id)
			json.Unmarshal(data["requirements_version"], &version)
			json.Unmarshal(data["resume_version"], &rowVersion)
			json.Unmarshal(data["evidence_chunks"], &chunks)
			status := "met"
			if id == "pending" {
				status = "unknown"
			}
			var chunk resumeVectorChunk
			for _, item := range chunks {
				if item.Path == "raw_text" {
					chunk = item
				}
			}
			evaluation := assistantEvaluation{CandidateID: id, ResumeVersion: rowVersion, RequirementsVersion: version, RequirementChecks: []assistantRequirementCheck{{RequirementID: "r1", Status: status, EvidenceRefs: []string{}}}, Strengths: []string{}, Gaps: []string{}, QuestionsToVerify: []string{}, EvidenceRefs: []assistantEvidenceRef{}}
			if status == "met" {
				evaluation.RequirementChecks[0].EvidenceRefs = []string{chunk.ID}
				evaluation.EvidenceRefs = []assistantEvidenceRef{{RequirementID: "r1", ChunkID: chunk.ID, Path: chunk.Path, Quote: chunk.Text}}
			}
			return recommendationChatResponse(evaluation), nil
		case strings.Contains(prompt, "独立证据复核"):
			return recommendationChatResponse(map[string]any{"checks": []map[string]any{{"requirement_id": "r1", "supported": true}}, "strengths_supported": []bool{}, "gaps_supported": []bool{}}), nil
		default:
			t.Fatal("unexpected model operation")
			return nil, errors.New("unexpected")
		}
	})
	wallet, _ := embeddingFixture(t, transport)
	positions := NewMemoryPositionStore()
	auth := &AuthService{store: NewMemoryAuthStore()}
	headhunter := NewHeadhunterService(auth, NewMemoryHeadhunterStore(), tenants, positions)
	customer, err := headhunter.SaveCustomer(scope, HeadhunterCustomer{Name: "测试甲方"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := headhunter.SaveJob(scope, HeadhunterJob{Name: "Go岗位", CustomerID: customer.ID, Requirements: "熟悉Go"})
	if err != nil {
		t.Fatal(err)
	}
	vectors := newResumeVectorStore(nil, candidateStore)
	for _, id := range []string{"ready", "pending"} {
		candidate := candidateStore.profiles[id]
		if err = vectors.Save(context.Background(), scope, resumeVectorRecord{CandidateID: id, TenantID: tenant.ID, OwnerEmail: scope.UserEmail, ResumeVersion: 1, Profile: resumeVectorProfile, Complete: true, Chunks: buildResumeVectorChunks(candidate)}); err != nil {
			t.Fatal(err)
		}
	}
	search := &recommendationSearchFixture{hits: []resumeSearchHit{{CandidateID: "ready", ResumeVersion: 1}, {CandidateID: "ready", ResumeVersion: 1}, {CandidateID: "pending", ResumeVersion: 1}, {CandidateID: "stale", ResumeVersion: 1}}}
	service := NewAssistantService(NewCandidateService(auth, candidateStore, tenants, positions), wallet, headhunter, search, vectors)
	phaseCount := 0
	result, err := service.recommend(context.Background(), scope, job.ID, "", func(kind string, data map[string]any) error {
		if kind != "phase" || data["stage"] == "" || data["text"] == "" {
			t.Fatalf("frontend event contract mismatch kind=%s data=%v", kind, data)
		}
		phaseCount++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	recommended := result["recommendations"].([]assistantEvaluation)
	pending := result["pending_verification"].([]assistantEvaluation)
	counts := result["stage_counts"].(map[string]int)
	if len(recommended) != 1 || recommended[0].CandidateID != "ready" || len(pending) != 1 || pending[0].CandidateID != "pending" || counts["stale_or_inaccessible_skipped"] != 1 || counts["analyzed"] != 2 || result["reranker"] != "llm_reranker" {
		t.Fatalf("result=%+v", result)
	}
	if recommended[0].Name != "合成姓名不许传模型" || recommended[0].RequirementsVersion == "" {
		t.Fatal("server display identity or version missing")
	}
	if phaseCount < 6 {
		t.Fatalf("missing recommendation progress phases: %d", phaseCount)
	}
	if assistantRequirementsHash(job, "偏好变化", "first-model", nil) == assistantRequirementsHash(job, "", "first-model", nil) {
		t.Fatal("requirements change did not change version")
	}
	changed := false
	oldResult, oldErr := service.recommend(context.Background(), scope, job.ID, "", func(_ string, data map[string]any) error {
		if data["stage"] == "analyzing" && !changed {
			changed = true
			job.Description = "处理中更新了岗位职责"
			var saveErr error
			job, saveErr = headhunter.SaveJob(scope, job)
			return saveErr
		}
		return nil
	})
	if oldResult != nil || oldErr == nil || !strings.Contains(oldErr.Error(), "岗位资料已变化") {
		t.Fatalf("changed JD oldResult=%v err=%v", oldResult, oldErr)
	}
}

// TestAssistantRecommendationGuardsRejectStalePermissions 验证开始时的管理员标记不能在降权后读取同事简历，更新版本也不能沿用。
func TestAssistantRecommendationGuardsRejectStalePermissions(t *testing.T) {
	headhunter, _, member, _ := headhunterFixture(t)
	candidates := NewMemoryCandidateStore()
	candidates.profiles["colleague-candidate"] = PositionCandidate{ID: "colleague-candidate", TenantID: member.TenantID, UserEmail: "colleague@example.com", RowVersion: 2, CandidateName: "权限样本"}
	s := &AssistantService{candidates: NewCandidateService(headhunter.auth, candidates, headhunter.tenants, headhunter.positions)}
	staleAdmin := member
	staleAdmin.IsAdmin = true
	if _, err := s.checkRecommendationDocuments(staleAdmin, []ResumeRerankDocument{{CandidateID: "colleague-candidate", ResumeVersion: 2}}); err == nil {
		t.Fatal("stale admin permission exposed colleague resume")
	}
	own := candidates.profiles["colleague-candidate"]
	own.UserEmail = member.UserEmail
	candidates.profiles[own.ID] = own
	if _, err := s.checkRecommendationDocuments(member, []ResumeRerankDocument{{CandidateID: own.ID, ResumeVersion: 1}}); err == nil {
		t.Fatal("stale resume version accepted")
	}
	original := HeadhunterJob{ID: "job", Revision: 1, Requirements: "熟悉Go"}
	current := original
	current.Requirements = "熟悉Rust"
	if assistantJobUnchanged(original, current, "") {
		t.Fatal("changed JD accepted when legacy version was not incremented")
	}
}
