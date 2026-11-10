// 本文件定义在线简历入库协议，并按用户确认的完整条件执行唯一同人匹配；缺少信息不降级合并。
package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// OnlineCandidateProfile 仅接收页面明确可见的在线资料；未知年龄和开始年份必须留空。
type OnlineCandidateProfile struct {
	CandidateName       string                    `json:"candidate_name"`
	Gender              string                    `json:"gender,omitempty"`
	Age                 *int                      `json:"age,omitempty"`
	EducationLevel      string                    `json:"education_level,omitempty"`
	EarliestCompanyName string                    `json:"earliest_company_name,omitempty"`
	EarliestCompanyYear *int                      `json:"earliest_company_year,omitempty"`
	EarliestSchoolName  string                    `json:"earliest_school_name,omitempty"`
	EarliestSchoolYear  *int                      `json:"earliest_school_year,omitempty"`
	RawText             string                    `json:"raw_text"`
	SourceKey           string                    `json:"source_key"`
	WorkExperiences     []CandidateWorkExperience `json:"work_experiences,omitempty"`
	Educations          []CandidateEducation      `json:"educations,omitempty"`
}

// onlineProfileReceipt 保存当前平台的独立证据，不因同姓匹配覆盖原有来源快照。
type onlineProfileReceipt struct {
	Profile     OnlineCandidateProfile `json:"profile"`
	ProfileKey  string                 `json:"profile_key"`
	MatchStatus string                 `json:"match_status"`
	CapturedAt  time.Time              `json:"captured_at"`
}

// onlineProfileSurname 复用中文姓名规则并识别复姓；无法确认的非中文姓名不自动关联。
func onlineProfileSurname(name string) string {
	name = strings.TrimSpace(name)
	for _, surname := range []string{"欧阳", "司马", "上官", "诸葛", "夏侯", "东方", "皇甫", "尉迟", "公孙", "慕容", "令狐", "长孙", "宇文", "司徒", "司空", "独孤", "南宫", "闻人", "端木"} {
		if strings.HasPrefix(name, surname) {
			return surname
		}
	}
	for _, r := range name {
		if unicode.Is(unicode.Han, r) {
			return string(r)
		}
		return ""
	}
	return ""
}

// onlineProfileEducation 将明确等价的学历名称统一，不根据学校名称猜最高学历。
func onlineProfileEducation(value string) string {
	value = candidateDuplicateText(value)
	switch value {
	case "大学本科", "本科学历", "学士":
		return "本科"
	case "大学专科", "专科", "专科学历":
		return "大专"
	case "硕士研究生", "研究生硕士":
		return "硕士"
	case "博士研究生":
		return "博士"
	}
	return value
}

// onlineProfileMatchKey 只有姓氏、最高学历及最早公司学校年份均明确时才创建匹配组，年龄另作正负3岁核验。
func onlineProfileMatchKey(p OnlineCandidateProfile) string {
	if p.Age == nil || *p.Age < 1 || *p.Age > 120 || p.EarliestCompanyYear == nil || p.EarliestSchoolYear == nil {
		return ""
	}
	if *p.EarliestCompanyYear < 1900 || *p.EarliestSchoolYear < 1900 || *p.EarliestCompanyYear > time.Now().Year() || *p.EarliestSchoolYear > time.Now().Year() {
		return ""
	}
	parts := []string{onlineProfileSurname(p.CandidateName), onlineProfileEducation(p.EducationLevel), candidateDuplicateText(p.EarliestCompanyName), fmt.Sprint(*p.EarliestCompanyYear), candidateDuplicateText(p.EarliestSchoolName), fmt.Sprint(*p.EarliestSchoolYear)}
	for _, part := range parts {
		if part == "" {
			return ""
		}
	}
	return purgeDigest(parts...)
}

// onlineProfileFromCandidate 仅在线关联按完整经历的明确开始年月提取最早机构；人工查重继续使用原有容错方法。
func onlineProfileFromCandidate(item PositionCandidate) OnlineCandidateProfile {
	work, schools := make([][2]string, 0, len(item.WorkExperiences)), make([][2]string, 0, len(item.Educations))
	for _, entry := range item.WorkExperiences {
		work = append(work, [2]string{entry.CompanyName, entry.StartYM})
	}
	for _, entry := range item.Educations {
		schools = append(schools, [2]string{entry.SchoolName, entry.StartYM})
	}
	company, companyYear := onlineCanonicalEarliestExperience(work, time.Now().Year())
	school, schoolYear := onlineCanonicalEarliestExperience(schools, time.Now().Year())
	age := candidateDuplicateAge(0, item.BirthYM)
	if age == 0 {
		_, _ = fmt.Sscan(candidateString(item.LibraryFields, "age"), &age)
	}
	p := OnlineCandidateProfile{CandidateName: item.CandidateName, EducationLevel: item.EducationLevel, EarliestCompanyName: company, EarliestCompanyYear: companyYear, EarliestSchoolName: school, EarliestSchoolYear: schoolYear}
	if age > 0 {
		p.Age = &age
	}
	return p
}

// onlineProfilesMatch 同组全部字段必须一致，年龄在正负3岁以内；此规则只用于身份关联，不用于岗位淘汰。
func onlineProfilesMatch(a, b OnlineCandidateProfile) bool {
	key := onlineProfileMatchKey(a)
	return key != "" && key == onlineProfileMatchKey(b) && *a.Age >= *b.Age-3 && *a.Age <= *b.Age+3
}

// onlineProfileFingerprint 按实际资料内容生成服务端指纹，调用方自报的缓存编号不能作为人物身份。
func onlineProfileFingerprint(p OnlineCandidateProfile) string {
	p.SourceKey = ""
	raw, _ := json.Marshal(p)
	return purgeDigest(string(raw))
}
