// Package boss 提供 Boss 直聘平台的本地运行时实现。
package boss

import (
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"regexp"
	"strings"
)

// Runtime 实现 Boss 平台运行时能力。
type Runtime struct{}

// NewRuntime 创建 Boss 平台运行时实例。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// bossCandidateVisiblePayload 返回 Boss 候选人可见定位通用参数。
// cfg 为平台配置，candidate 为候选人。
func bossCandidateVisiblePayload(cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) map[string]any {
	return map[string]any{
		"platform_config":           cfg,
		"card_index":                intFromMap(candidate, "card_index"),
		"element_ref":               stringFromMap(candidate, "element_ref"),
		"diagnostic_candidate_name": candidateName(candidate),
		"distance":                  120,
		"wait_ms":                   260,
		"card_scroll_attempts":      18,
		"require_full":              true,
		"viewport_margin":           0,
	}
}

// candidateAge 读取 Boss 候选人年龄。
// candidate 为候选人卡片数据，优先读取结构字段，缺失时从文本中提取“xx岁”。
func candidateAge(candidate platformcore.Candidate) string {
	fields := mapFromAny(candidate["fields"])
	age := firstNonEmpty(
		stringFromMap(candidate, "age"),
		stringFromMap(candidate, "candidate_age"),
		stringFromMap(fields, "age"),
		stringFromMap(fields, "candidate_age"),
	)
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

// normalizeCandidateIDPart 规范化 Boss 候选人 ID 组成部分。
// value 为姓名或年龄文本，返回去除空白后的文本。
func normalizeCandidateIDPart(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}
