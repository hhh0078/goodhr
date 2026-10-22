// 本文件验证引文存在不等于支持结论、专用精排响应边界和预算终止，不调用真实付费服务。
package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestAssistantSemanticVerificationDowngradesOverclaim 验证原文只有参与时，第二次语义核验不支持主导结论会改为未知并删除夸大优势。
func TestAssistantSemanticVerificationDowngradesOverclaim(t *testing.T) {
	candidate := PositionCandidate{ID: "c1", UserEmail: "embedding@example.com", CandidateName: "合成人选", RowVersion: 1, RawText: "参与检索平台开发"}
	chunks := buildResumeVectorChunks(candidate)
	var evidence resumeVectorChunk
	for _, chunk := range chunks {
		if chunk.Path == "raw_text" {
			evidence = chunk
		}
	}
	requirements := []assistantRequirement{{ID: "r1", Text: "主导检索平台建设", Type: "must_have", Explicit: true}}
	wallet, _ := embeddingFixture(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(request.Body)
		if strings.Contains(string(raw), "独立证据复核") {
			return recommendationChatResponse(map[string]any{"checks": []map[string]any{{"requirement_id": "r1", "supported": false}}, "strengths_supported": []bool{false}, "gaps_supported": []bool{}}), nil
		}
		return recommendationChatResponse(assistantEvaluation{CandidateID: "c1", ResumeVersion: 1, RequirementsVersion: "v1", RequirementChecks: []assistantRequirementCheck{{RequirementID: "r1", Status: "met", EvidenceRefs: []string{evidence.ID}}}, EvidenceRefs: []assistantEvidenceRef{{RequirementID: "r1", ChunkID: evidence.ID, Path: evidence.Path, Quote: evidence.Text}}, Strengths: []string{"主导检索平台建设"}, Gaps: []string{}, QuestionsToVerify: []string{}}), nil
	}))
	tenants := NewMemoryTenantStore()
	tenant, err := tenants.GetOrCreateTenant(candidate.UserEmail)
	if err != nil {
		t.Fatal(err)
	}
	candidate.TenantID = tenant.ID
	candidates := NewMemoryCandidateStore()
	candidates.profiles[candidate.ID] = candidate
	service := &AssistantService{wallet: wallet, candidates: NewCandidateService(nil, candidates, tenants)}
	scope := CandidateLibraryScope{TenantID: tenant.ID, UserEmail: candidate.UserEmail, IsAdmin: true}
	result, err := service.evaluateAssistantCandidate(context.Background(), scope, requirements, "v1", assistantRecommendationCandidate{Candidate: candidate, Chunks: chunks}, &recommendationBudget{})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequirementChecks[0].Status != "unknown" || len(result.Strengths) != 0 || len(result.EvidenceRefs) != 0 || classifyAssistantEvaluation(&result, requirements) != "pending" {
		t.Fatalf("unsupported semantic claim accepted: %+v", result)
	}
}

// TestAssistantRecommendationBudgetIsFatal 验证总预算耗尽不再访问上游，且外层不能重新自动付费重跑。
func TestAssistantRecommendationBudgetIsFatal(t *testing.T) {
	service := &AssistantService{}
	var target struct {
		Value string `json:"value"`
	}
	err := service.recommendationJSON(context.Background(), CandidateLibraryScope{}, &recommendationBudget{Calls: 120}, "JSON", nil, &target, nil)
	var fatal assistantFatalError
	if !errors.Is(err, errRecommendationBudget) || !errors.As(err, &fatal) {
		t.Fatalf("budget did not stop outer retry: %v", err)
	}
}

// TestAssistantDedicatedRerankRejectsMissingIndex 验证专用精排结果必须有明确索引，缺失索引不能默认为第一个候选人。
func TestAssistantDedicatedRerankRejectsMissingIndex(t *testing.T) {
	service := &httpResumeReranker{endpoint: "https://rerank.example.com/v1/rerank", model: "fixed-rerank-version", client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[{"relevance_score":0.7}]}`)), Header: make(http.Header)}, nil
	})}}
	if _, err := service.Rerank(context.Background(), "岗位", []ResumeRerankDocument{{CandidateID: "c1", Text: "相关经历"}}); err == nil {
		t.Fatal("missing rerank index accepted")
	}
}

// TestAssistantClassificationRequiresPositiveEvidence 验证没有必要条件时，全未知不能冒充推荐；加分项有真实支持证据时才可推荐。
func TestAssistantClassificationRequiresPositiveEvidence(t *testing.T) {
	for _, sample := range []struct {
		name        string
		kind        string
		status      string
		quote       string
		referenceID string
		want        string
	}{
		{name: "只有加分项且全部未知", kind: "preferred", status: "unknown", want: "pending"},
		{name: "只有未明确要求且全部未知", kind: "unresolved", status: "unknown", want: "pending"},
		{name: "岗位条件尚未明确不能凭简历证据宣称匹配", kind: "unresolved", status: "met", quote: "参与搜索平台项目开发", referenceID: "chunk-current", want: "pending"},
		{name: "加分项明确符合且有原文支持", kind: "preferred", status: "met", quote: "参与搜索平台项目开发", referenceID: "chunk-current", want: "recommended"},
		{name: "加分项部分符合且有原文支持", kind: "preferred", status: "partial", quote: "参与搜索平台项目开发", referenceID: "chunk-current", want: "recommended"},
		{name: "加分项声称符合但没有原文", kind: "preferred", status: "met", referenceID: "chunk-current", want: "pending"},
		{name: "加分项引用编号不匹配", kind: "preferred", status: "met", quote: "参与搜索平台项目开发", referenceID: "other-chunk", want: "pending"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			requirements := []assistantRequirement{{ID: "r1", Text: "搜索平台项目经验", Type: sample.kind}}
			check := assistantRequirementCheck{RequirementID: "r1", Status: sample.status, EvidenceRefs: []string{}}
			if sample.referenceID != "" {
				check.EvidenceRefs = []string{sample.referenceID}
			}
			item := assistantEvaluation{RequirementChecks: []assistantRequirementCheck{check}, EvidenceRefs: []assistantEvidenceRef{}}
			if sample.quote != "" {
				item.EvidenceRefs = []assistantEvidenceRef{{RequirementID: "r1", ChunkID: "chunk-current", Path: "work_experiences[0].content", Quote: sample.quote}}
			}
			if got := classifyAssistantEvaluation(&item, requirements); got != sample.want {
				t.Fatalf("classification=%s want=%s item=%+v", got, sample.want, item)
			}
		})
	}
}
