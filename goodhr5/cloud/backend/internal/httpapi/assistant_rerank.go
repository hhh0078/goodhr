// 本文件提供可替换的岗位候选人精排接口，专用服务缺失时明确使用有界聊天模型精排。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ResumeRerankDocument 保存不带姓名和联系方式的候选人证据文本。
type ResumeRerankDocument struct {
	CandidateID   string `json:"candidate_id"`
	ResumeVersion int64  `json:"resume_version"`
	Text          string `json:"text"`
}

// ResumeRerankScore 表示仅用于本次排序的相关度分数，不是录用概率。
type ResumeRerankScore struct {
	CandidateID string  `json:"candidate_id"`
	Score       float64 `json:"score"`
}

// ResumeReranker 隔离专用精排渠道，query为岗位条件，documents为有界脱敏证据。
type ResumeReranker interface {
	Rerank(context.Context, string, []ResumeRerankDocument) ([]ResumeRerankScore, error)
}

// httpResumeReranker 只读取管理员配置的固定地址，模型与用户不能覆盖服务地址或密钥。
type httpResumeReranker struct {
	endpoint, key, model string
	client               *http.Client
}

// newHTTPResumeReranker 使用独立环境配置选择精排服务，未配置时交由调用方明确使用LLM方案。
func newHTTPResumeReranker() (ResumeReranker, error) {
	endpoint := strings.TrimSpace(os.Getenv("GOODHR_RERANK_URL"))
	if endpoint == "" {
		return nil, nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
		return nil, errors.New("精排服务地址未配置完整")
	}
	model := strings.TrimSpace(os.Getenv("GOODHR_RERANK_MODEL"))
	if model == "" {
		return nil, errors.New("请为独立精排服务配置固定模型版本")
	}
	return &httpResumeReranker{endpoint: endpoint, key: os.Getenv("GOODHR_RERANK_API_KEY"), model: model, client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Rerank 兼容常见rerank服务的query/documents合同，严格核对返回索引和有限数字。
func (s *httpResumeReranker) Rerank(ctx context.Context, query string, documents []ResumeRerankDocument) ([]ResumeRerankScore, error) {
	texts := []string{}
	for _, document := range documents {
		texts = append(texts, document.Text)
	}
	body, err := json.Marshal(map[string]any{"model": s.model, "query": query, "documents": texts, "top_n": len(texts), "return_documents": false})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if s.key != "" {
		request.Header.Set("Authorization", "Bearer "+s.key)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, errors.New("独立精排服务连接失败")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 || response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("独立精排响应未完整完成")
	}
	var result struct {
		Results []struct {
			Index *int     `json:"index"`
			Score *float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Results) != len(documents) {
		return nil, errors.New("独立精排结果数量不完整")
	}
	seen := map[int]bool{}
	scores := []ResumeRerankScore{}
	for _, item := range result.Results {
		if item.Index == nil || *item.Index < 0 || *item.Index >= len(documents) || seen[*item.Index] || item.Score == nil || math.IsNaN(*item.Score) || math.IsInf(*item.Score, 0) {
			return nil, errors.New("独立精排返回了无效索引或分数")
		}
		seen[*item.Index] = true
		scores = append(scores, ResumeRerankScore{CandidateID: documents[*item.Index].CandidateID, Score: *item.Score})
	}
	return scores, nil
}

// assistantChunkTerms 生成轻量原文片段选择词，只用于上下文组织，不把词频当成最终匹配分数。
func assistantChunkTerms(requirements []assistantRequirement) []string {
	terms := []string{}
	seen := map[string]bool{}
	for _, requirement := range requirements {
		for _, word := range strings.FieldsFunc(strings.ToLower(requirement.Text), func(value rune) bool { return unicode.IsSpace(value) || unicode.IsPunct(value) }) {
			runes := []rune(word)
			if len(runes) > 12 {
				for start := 0; start+2 <= len(runes); start += 2 {
					term := string(runes[start : start+2])
					if !seen[term] {
						terms = append(terms, term)
						seen[term] = true
					}
				}
			} else if len(runes) > 1 && !seen[word] {
				terms = append(terms, word)
				seen[word] = true
			}
		}
	}
	return terms
}

// selectAssistantChunks 联合检索命中和全简历条件词选择证据，后半段经历同样参与，超预算只标记截断而不判不具备能力。
func selectAssistantChunks(chunks []resumeVectorChunk, requirements []assistantRequirement, hits []resumeVectorChunk, budget int) ([]resumeVectorChunk, bool) {
	type scored struct {
		chunk resumeVectorChunk
		score int
		index int
	}
	terms := assistantChunkTerms(requirements)
	ranked := []scored{}
	for index, chunk := range chunks {
		score := 0
		if chunk.Kind == "summary" {
			score += 2
		}
		for _, term := range terms {
			if strings.Contains(strings.ToLower(chunk.Text), term) {
				score++
			}
		}
		for _, hit := range hits {
			if hit.Path == chunk.Path && (hit.Text == chunk.Text || strings.Contains(chunk.Text, hit.Text) || strings.Contains(hit.Text, chunk.Text)) {
				score += 10
			}
		}
		ranked = append(ranked, scored{chunk: chunk, score: score, index: index})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	selected := []scored{}
	used := 0
	for _, item := range ranked {
		length := utf8.RuneCountInString(item.chunk.Text)
		if used+length <= budget {
			copy := item
			copy.chunk.Vector = nil
			selected = append(selected, copy)
			used += length
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].index < selected[j].index })
	result := []resumeVectorChunk{}
	for _, item := range selected {
		result = append(result, item.chunk)
	}
	return result, len(result) < len(chunks)
}

// rerankAssistantDocuments 在独立服务和明确标记的LLM精排间选择，结果必须覆盖当前全部输入候选人。
func (s *AssistantService) rerankAssistantDocuments(ctx context.Context, scope CandidateLibraryScope, requirements []assistantRequirement, documents []ResumeRerankDocument, budget *recommendationBudget) ([]ResumeRerankScore, string, error) {
	reranker, err := newHTTPResumeReranker()
	if err != nil {
		return nil, "", err
	}
	queryRaw, _ := json.Marshal(requirements)
	if reranker != nil {
		if _, err = s.checkRecommendationDocuments(scope, documents); err != nil {
			return nil, "dedicated_reranker", err
		}
		scores, err := reranker.Rerank(ctx, string(queryRaw), documents)
		if err == nil {
			sort.SliceStable(scores, func(i, j int) bool { return scores[i].Score > scores[j].Score })
			for index := range scores {
				scores[index].Score = 100 * float64(len(scores)-index) / float64(max(1, len(scores)))
			}
		}
		return scores, "dedicated_reranker", err
	}
	all := []ResumeRerankScore{}
	for start := 0; start < len(documents); start += 10 {
		batch := documents[start:min(start+10, len(documents))]
		if scope, err = s.checkRecommendationDocuments(scope, batch); err != nil {
			return nil, "llm_reranker", err
		}
		var result struct {
			Scores []ResumeRerankScore `json:"scores"`
		}
		err = s.recommendationJSON(ctx, scope, budget, "你现在仅做岗位与职业经历的语义精排。输出{\"scores\":[{\"candidate_id\":\"输入编号\",\"score\":0到100的相关度}]}，每个输入恰好一条。比较职责深度、项目场景和真实经历，不因关键词重复多而加分；同义经历可以相关，优先条件不作为强制淘汰。这个分数不是录用概率。", map[string]any{"requirements": requirements, "documents": batch}, &result, func() error {
			if len(result.Scores) != len(batch) {
				return errors.New("scores必须覆盖本批全部候选人")
			}
			ids := map[string]bool{}
			for _, item := range batch {
				ids[item.CandidateID] = true
			}
			for _, item := range result.Scores {
				if !ids[item.CandidateID] || math.IsNaN(item.Score) || math.IsInf(item.Score, 0) || item.Score < 0 || item.Score > 100 {
					return errors.New("精排候选编号重复或分数无效")
				}
				delete(ids, item.CandidateID)
			}
			return nil
		})
		if err != nil {
			return nil, "llm_reranker", err
		}
		all = append(all, result.Scores...)
	}
	return all, "llm_reranker", nil
}
