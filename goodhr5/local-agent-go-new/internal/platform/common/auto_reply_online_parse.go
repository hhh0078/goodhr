// Package common 本文件从平台DOM或服务器AI转录的明确文字提取基础字段，歧义与未知值保持空。
package common

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

var onlineProfileAge = regexp.MustCompile(`([1-9][0-9]?)\s*岁`)
var onlineProfileEducation = regexp.MustCompile(`(?:最高学历|学历)\s*[:：]\s*(博士|硕士|研究生|本科|大专|专科|高中|中专|职高|初中)`)
var onlineProfileHeaderEducation = regexp.MustCompile(`(博士|硕士|研究生|本科|大专|专科|高中|中专|职高|初中)`)
var onlineProfileGender = regexp.MustCompile(`(?:^|[\s|｜/，,])(?:性别\s*[:：]\s*)?(男|女)(?:$|[\s|｜/，,])`)
var onlineProfilePeriodStart = regexp.MustCompile(`^\s*((?:19|20)[0-9]{2})(?:[./年][0-9]{1,2}|\s*[-~—至])`)

// ParseAutoReplyOnlineProfileText 仅提取头部明示年龄学历与经历区间起始年份，不从职位、行业或工龄推测。
func ParseAutoReplyOnlineProfileText(text, name string) model.AutoReplyOnlineProfile {
	text = strings.TrimSpace(text)
	profile := model.AutoReplyOnlineProfile{CandidateName: strings.TrimSpace(name), RawText: text}
	if text == "" {
		return profile
	}
	lines := strings.Split(text, "\n")
	header := strings.Join(lines[:min(8, len(lines))], "\n")
	if age := uniqueOnlineMatch(onlineProfileAge, header); age != "" {
		if value, err := strconv.Atoi(age); err == nil && value >= 16 && value <= 100 {
			profile.Age = &value
		}
	}
	profile.EducationLevel = uniqueOnlineMatch(onlineProfileEducation, header)
	if profile.EducationLevel == "" {
		profile.EducationLevel = uniqueOnlineMatch(onlineProfileHeaderEducation, header)
	}
	profile.Gender = uniqueOnlineMatch(onlineProfileGender, header)
	profile.EarliestCompanyYear = onlineSectionEarliestYear(lines, "work")
	profile.EarliestSchoolYear = onlineSectionEarliestYear(lines, "school")
	profile.SourceKey = "online:" + HashText(profile.CandidateName+"|"+profile.RawText)
	return profile
}

// uniqueOnlineMatch 只有同一字段的所有显式值一致时才返回，冲突时不挑选其一。
func uniqueOnlineMatch(pattern *regexp.Regexp, text string) string {
	value := ""
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		if value != "" && value != match[1] {
			return ""
		}
		value = match[1]
	}
	return value
}

// onlineSectionEarliestYear 只统计明确工作或教育标题下以日期区间开头的行，项目背景年份不混入资料匹配。
func onlineSectionEarliestYear(lines []string, wanted string) *int {
	section, minimum := "", 0
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(raw), "#*- "))
		switch strings.TrimRight(line, ":：") {
		case "工作经历", "工作经验", "工作履历":
			section = "work"
			continue
		case "教育经历", "教育背景", "学习经历":
			section = "school"
			continue
		case "项目经历", "项目经验", "自我评价", "个人优势", "专业技能", "求职意向", "荣誉证书", "资格证书":
			section = ""
			continue
		}
		if section != wanted {
			continue
		}
		match := onlineProfilePeriodStart.FindStringSubmatch(line)
		if len(match) < 2 {
			continue
		}
		year, err := strconv.Atoi(match[1])
		if err == nil && year >= 1900 && year <= time.Now().Year() && (minimum == 0 || year < minimum) {
			minimum = year
		}
	}
	if minimum == 0 {
		return nil
	}
	return &minimum
}
