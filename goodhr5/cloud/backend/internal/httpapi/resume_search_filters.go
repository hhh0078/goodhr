// 本文件将可靠且明确的必要条件转换为固定搜索过滤；未知事实保留，不开放模型任意 DSL。
package httpapi

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
)

// resumeSearchFilter 只承载经过需求来源核验的白名单字段和操作符。
type resumeSearchFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// filteredResumeSearch 在具备可靠字段时将必要条件推入关键词及向量预过滤。
type filteredResumeSearch interface {
	SearchFiltered(context.Context, CandidateLibraryScope, string, []float64, []resumeSearchFilter) ([]resumeSearchHit, error)
}

// resumeEducationRank 仅标准化明确学历名称；未知和含糊文本不推断等级。
func resumeEducationRank(value string) int {
	switch strings.TrimSpace(value) {
	case "初中":
		return 1
	case "高中":
		return 2
	case "中专", "中技":
		return 3
	case "大专", "专科":
		return 4
	case "本科", "学士":
		return 5
	case "硕士", "研究生", "硕士研究生":
		return 6
	case "博士", "博士研究生":
		return 7
	}
	return 0
}

// buildResumeVectorFacts 从原始结构化字段提取可安全过滤事实，不使用 AI 推断或敏感人口属性。
func buildResumeVectorFacts(candidate PositionCandidate) map[string]any {
	facts := map[string]any{}
	if rank := resumeEducationRank(candidate.EducationLevel); rank > 0 {
		facts["education_rank"] = rank
	}
	if candidate.ExpectedSalaryMin != nil && *candidate.ExpectedSalaryMin >= 0 {
		facts["expected_salary_min"] = *candidate.ExpectedSalaryMin
	}
	return facts
}

// resumeSearchPreFilters 只排除已知且明确不符的值，未填字段通过 unknown 分支进入待核实池。
func resumeSearchPreFilters(filters []resumeSearchFilter) ([]any, error) {
	if len(filters) > 8 {
		return nil, errors.New("可靠必要条件超过本轮过滤预算")
	}
	result := []any{}
	for _, filter := range filters {
		field := ""
		var condition any
		switch filter.Field {
		case "education_min":
			if filter.Op != "gte" {
				return nil, errors.New("学历条件只支持明确的最低要求")
			}
			rank := resumeEducationRank(filter.Value)
			if rank == 0 {
				return nil, errors.New("学历要求无法可靠标准化")
			}
			field = "education_rank"
			condition = map[string]any{"range": map[string]any{field: map[string]int{"gte": rank}}}
		case "expected_salary_max":
			if filter.Op != "lte" {
				return nil, errors.New("薪资条件只支持明确的岗位硬上限")
			}
			value, err := strconv.ParseFloat(filter.Value, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > 1000000 {
				return nil, errors.New("岗位薪资上限无效")
			}
			field = "expected_salary_min"
			condition = map[string]any{"range": map[string]any{field: map[string]float64{"lte": value}}}
		case "certificate":
			continue
		default:
			return nil, errors.New("该条件不能作为可靠的数据库过滤字段")
		}
		result = append(result, map[string]any{"bool": map[string]any{"should": []any{condition, map[string]any{"bool": map[string]any{"must_not": []any{map[string]any{"exists": map[string]string{"field": field}}}}}}, "minimum_should_match": 1}})
	}
	return result, nil
}
