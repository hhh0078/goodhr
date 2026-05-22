// 本文件负责 Boss 平台候选人字段组装实现。
package httpapi

import (
	"regexp"
	"strconv"
	"strings"
)

func mapBossFieldsToCandidate(platformID string, fields map[string]any) Candidate {
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
	candidate := Candidate{
		PlatformID:          strings.TrimSpace(platformID),
		PlatformCandidateID: "",
		Name:                strings.TrimSpace(name),
		BasicInfo:           strings.TrimSpace(firstNonEmpty(basicInfo, education)),
		EducationLevel:      strings.TrimSpace(education),
		PersonalDescription: strings.TrimSpace(description),
		RawText:             strings.TrimSpace(raw),
		FilterText:          strings.TrimSpace(raw),
		BasicProfile: CandidateBasicProfile{
			PersonalDescription: strings.TrimSpace(description),
		},
		Runtime: CandidateRuntime{
			ElementRef:  strings.TrimSpace(elementRef),
			CardIndex:   index,
			Fingerprint: "",
		},
		Ext: map[string]any{"raw_fields": fields},
	}
	applyBossBasicProfile(&candidate, basicInfo, education, university, raw)
	return candidate
}

func applyBossBasicProfile(candidate *Candidate, basicInfo, education, university, raw string) {
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
		profile.Educations = []CandidateEducation{
			{
				SchoolName:     strings.TrimSpace(university),
				EducationLevel: strings.TrimSpace(firstNonEmpty(education, candidate.EducationLevel)),
			},
		}
	}
	candidate.BasicProfile = profile
}

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

func parseWorkStatus(text string) string {
	keywords := []string{"离职", "在职", "看机会"}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return keyword
		}
	}
	return ""
}

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
