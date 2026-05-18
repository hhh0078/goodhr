// 本文件负责候选人筛选逻辑：关键词匹配和 AI 决策。
package httpapi

import (
	"math/rand"
	"strings"
)

// FilterResult 表示筛选结果。
type FilterResult struct {
	Passed          bool     `json:"passed"`
	Reason          string   `json:"reason"`
	MatchedKeywords []string `json:"matched_keywords,omitempty"`
}

// KeywordFilter 基于关键词列表对候选人进行文本匹配筛选。
type KeywordFilter struct {
	keywords        []string
	excludeKeywords []string
	isAndMode       bool
	clickFrequency  int
}

// NewKeywordFilter 创建关键词筛选器。
func NewKeywordFilter(keywords, excludeKeywords []string, isAndMode bool, clickFrequency int) *KeywordFilter {
	if clickFrequency <= 0 {
		clickFrequency = 7
	}
	return &KeywordFilter{
		keywords:        filterEmpty(keywords),
		excludeKeywords: filterEmpty(excludeKeywords),
		isAndMode:       isAndMode,
		clickFrequency:  clickFrequency,
	}
}

// Filter 对候选人文本执行关键词筛选。
func (f *KeywordFilter) Filter(text string) FilterResult {
	// 1. 先检查排除词
	for _, kw := range f.excludeKeywords {
		if kw != "" && strings.Contains(strings.ToLower(text), strings.ToLower(kw)) {
			return FilterResult{Passed: false, Reason: "命中排除词: " + kw}
		}
	}

	// 2. 无关键词时按概率通过
	if len(f.keywords) == 0 {
		if rand.Float64()*10 < float64(f.clickFrequency) {
			return FilterResult{Passed: true, Reason: "无条件概率通过"}
		}
		return FilterResult{Passed: false, Reason: "概率未通过"}
	}

	// 3. 关键词匹配
	matched := []string{}
	textLower := strings.ToLower(text)

	for _, kw := range f.keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(textLower, strings.ToLower(kw)) {
			matched = append(matched, kw)
		}
	}

	matchedAll := len(matched) == len(f.keywords)
	anyMatched := len(matched) > 0

	if f.isAndMode {
		if matchedAll {
			return FilterResult{Passed: true, Reason: "全部关键词匹配", MatchedKeywords: matched}
		}
		return FilterResult{Passed: false, Reason: "关键词未全部匹配", MatchedKeywords: matched}
	}

	if anyMatched {
		return FilterResult{Passed: true, Reason: "关键词部分匹配", MatchedKeywords: matched}
	}
	return FilterResult{Passed: false, Reason: "无关键词匹配"}
}

// filterEmpty 过滤空字符串。
func filterEmpty(items []string) []string {
	if items == nil {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			result = append(result, item)
		}
	}
	return result
}
