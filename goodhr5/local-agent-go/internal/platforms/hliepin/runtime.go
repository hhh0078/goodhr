// Package hliepin 提供猎聘猎头端平台的本地运行时实现。
package hliepin

import (
	"goodhr5/local-agent-go/internal/platformcore"
	"regexp"
	"strings"
)

// Runtime 实现猎聘猎头端平台运行时能力。
type Runtime struct {
	platformID      string
	platformName    string
	currentPosition string
}

// NewRuntime 创建猎聘猎头端平台运行时实例。
func NewRuntime() *Runtime { return &Runtime{platformID: "hliepin", platformName: "猎聘猎头端"} }

// candidateRawText 组装候选人卡片原始文本。
func candidateRawText(fields map[string]any) string {
	parts := []string{}
	for _, key := range []string{"name", "basic_info", "education", "university", "description"} {
		if text := stringFromMap(fields, key); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

// candidateAge 读取猎聘猎头端候选人年龄。
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

// normalizeCandidateIDPart 规范化猎聘猎头端候选人 ID 组成部分。
func normalizeCandidateIDPart(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}
