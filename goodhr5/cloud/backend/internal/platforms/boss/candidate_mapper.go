// Package boss 负责 Boss 平台候选人字段组装实现。
package boss

import (
	"regexp"
	"strconv"
	"strings"

	"goodhr5/cloud/backend/internal/platformcore"
)

// MapFieldsToCandidate 将 Boss 抽取字段映射为统一候选人模型。
func MapFieldsToCandidate(platformID string, fields map[string]any) platformcore.Candidate {
	name, _ := fields["name"].(string)
	basicInfo, _ := fields["basic_info"].(string)
	education, _ := fields["education"].(string)
	university, _ := fields["university"].(string)
	description, _ := fields["description"].(string)
	raw := buildCandidateText(fields, nil)
	if strings.TrimSpace(raw) == "" {
		raw = strings.Join([]string{
			strings.TrimSpace(name),
			strings.TrimSpace(basicInfo),
			strings.TrimSpace(education),
			strings.TrimSpace(university),
			strings.TrimSpace(description),
		}, " ")
	}
	index, _ := fields["_index"].(int)
	elementRef, _ := fields["element_ref"].(string)
	candidate := platformcore.Candidate{
		PlatformID:          strings.TrimSpace(platformID),
		PlatformCandidateID: "",
		Name:                strings.TrimSpace(name),
		BasicInfo:           strings.TrimSpace(firstNonEmpty(basicInfo, education)),
		EducationLevel:      strings.TrimSpace(education),
		PersonalDescription: strings.TrimSpace(description),
		RawText:             strings.TrimSpace(raw),
		FilterText:          strings.TrimSpace(raw),
		BasicProfile: platformcore.CandidateBasicProfile{
			PersonalDescription: strings.TrimSpace(description),
		},
		Runtime: platformcore.CandidateRuntime{
			ElementRef:  strings.TrimSpace(elementRef),
			CardIndex:   index,
			Fingerprint: "",
		},
		Ext: map[string]any{"raw_fields": fields},
	}
	applyBasicProfile(&candidate, basicInfo, education, university, raw)
	return candidate
}

// applyBasicProfile 根据 Boss 原始文案补齐候选人基础档案字段。
func applyBasicProfile(candidate *platformcore.Candidate, basicInfo, education, university, raw string) {
	combined := strings.Join([]string{
		strings.TrimSpace(basicInfo),
		strings.TrimSpace(education),
		strings.TrimSpace(university),
		strings.TrimSpace(raw),
	}, " ")
	combined = strings.TrimSpace(combined)
	if combined == "" {
		return
	}
	profile := candidate.BasicProfile
	profile.WorkYears = strings.TrimSpace(firstNonEmpty(profile.WorkYears, parseWorkYears(combined)))
	profile.WorkStatus = strings.TrimSpace(firstNonEmpty(profile.WorkStatus, parseWorkStatus(combined)))
	profile.OnlineStatus = strings.TrimSpace(firstNonEmpty(profile.OnlineStatus, parseOnlineStatus(combined)))
	if profile.ExpectedSalary.Min == nil || profile.ExpectedSalary.Max == nil {
		minSalary, maxSalary := parseSalaryRange(combined)
		if minSalary != nil && profile.ExpectedSalary.Min == nil {
			profile.ExpectedSalary.Min = minSalary
		}
		if maxSalary != nil && profile.ExpectedSalary.Max == nil {
			profile.ExpectedSalary.Max = maxSalary
		}
	}
	if len(profile.Educations) == 0 && (strings.TrimSpace(university) != "" || strings.TrimSpace(education) != "") {
		profile.Educations = []platformcore.CandidateEducation{
			{
				SchoolName:     strings.TrimSpace(university),
				EducationLevel: strings.TrimSpace(firstNonEmpty(education, candidate.EducationLevel)),
			},
		}
	}
	candidate.BasicProfile = profile
}

// parseWorkYears 从文本中提取工作年限描述。
func parseWorkYears(text string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\d{1,2}\s*-\s*\d{1,2}\s*年`),
		regexp.MustCompile(`\d{1,2}\+?\s*年`),
		regexp.MustCompile(`应届`),
	}
	for _, pattern := range patterns {
		if value := strings.TrimSpace(pattern.FindString(text)); value != "" {
			return value
		}
	}
	return ""
}

// parseWorkStatus 从文本中提取候选人工作状态。
func parseWorkStatus(text string) string {
	keywords := []string{"离职", "在职", "看机会"}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return keyword
		}
	}
	return ""
}

// parseOnlineStatus 从文本中提取在线状态描述。
func parseOnlineStatus(text string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\d+\s*分钟前在线`),
		regexp.MustCompile(`\d+\s*小时前在线`),
		regexp.MustCompile(`在线`),
	}
	for _, pattern := range patterns {
		if value := strings.TrimSpace(pattern.FindString(text)); value != "" {
			return value
		}
	}
	return ""
}

// parseSalaryRange 从文本中提取期望薪资区间并转换为元/月。
func parseSalaryRange(text string) (*int, *int) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(\d{1,3})\s*[kKＫ]\s*-\s*(\d{1,3})\s*[kKＫ]`),
		regexp.MustCompile(`(\d{1,3})\s*-\s*(\d{1,3})\s*[kKＫ]`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(text)
		if len(matches) != 3 {
			continue
		}
		minValue, minErr := strconv.Atoi(strings.TrimSpace(matches[1]))
		maxValue, maxErr := strconv.Atoi(strings.TrimSpace(matches[2]))
		if minErr != nil || maxErr != nil {
			continue
		}
		minSalary := minValue * 1000
		maxSalary := maxValue * 1000
		return &minSalary, &maxSalary
	}
	return nil, nil
}

// firstNonEmpty 返回首个非空字符串值。
func firstNonEmpty(primary, fallback string) string {
	text := strings.TrimSpace(primary)
	if text != "" {
		return text
	}
	return strings.TrimSpace(fallback)
}

// buildCandidateText 按字段顺序拼接候选人文本；未传顺序时拼接全部字符串字段。
func buildCandidateText(candidate map[string]any, orderedKeys []string) string {
	if len(orderedKeys) == 0 {
		parts := make([]string, 0, len(candidate))
		for _, value := range candidate {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		return strings.Join(parts, " ")
	}
	parts := make([]string, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		value, _ := candidate[key].(string)
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}
