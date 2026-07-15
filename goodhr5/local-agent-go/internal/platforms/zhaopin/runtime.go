// Package zhaopin 提供智联招聘平台的本地运行时实现。
package zhaopin

import (
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"regexp"
	"strings"
)

// Runtime 实现智联招聘平台运行时能力。
type Runtime struct{}

// NewRuntime 创建智联招聘平台运行时实例。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// ShouldSelectPositionDirectly 表示智联招聘每次直接切换任务岗位，不读取页面当前岗位。
func (r *Runtime) ShouldSelectPositionDirectly() bool {
	return true
}

// zhaopinCandidateVisiblePayload 返回智联候选人可见定位通用参数。
// cfg 为平台配置，candidate 为候选人。
func zhaopinCandidateVisiblePayload(cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) map[string]any {
	return map[string]any{
		"platform_id":             "zhaopin",
		"platform_config":         cfg,
		"candidate_name":          candidateName(candidate),
		"candidate_match_text":    candidateMatchText(candidate),
		"require_candidate_match": true,
		"card_index":              intFromMap(candidate, "card_index"),
		"element_ref":             stringFromMap(candidate, "element_ref"),
		"distance":                120,
		"wait_ms":                 260,
		"card_scroll_attempts":    18,
		"require_full":            true,
		"viewport_margin":         0,
	}
}

// candidateMatchText 返回智联动态列表重新定位候选人时使用的卡片文本。
// candidate 为任务最初提取的候选人数据。
func candidateMatchText(candidate platformcore.Candidate) string {
	fields := mapFromAny(candidate["fields"])
	return firstNonEmpty(stringFromMap(candidate, "raw_text"), stringFromMap(candidate, "filter_text"), stringFromMap(fields, "basic_info"))
}

// candidateAge 读取智联招聘候选人年龄。
func candidateAge(candidate platformcore.Candidate) string {
	fields := mapFromAny(candidate["fields"])
	age := firstNonEmpty(stringFromMap(candidate, "age"), stringFromMap(candidate, "candidate_age"), stringFromMap(fields, "age"), stringFromMap(fields, "candidate_age"))
	if age != "" {
		return age
	}
	text := firstNonEmpty(stringFromMap(candidate, "raw_text"), stringFromMap(candidate, "filter_text"), stringFromMap(fields, "basic_info"))
	match := regexp.MustCompile(`([1-9][0-9]?)\s*岁`).FindStringSubmatch(text)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

// normalizeCandidateIDPart 规范化智联招聘候选人 ID 组成部分。
func normalizeCandidateIDPart(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}
