// 本文件将完整简历原文与各段经历拆为可回溯向量片段，等长遮盖身份及联系信息，保留字段路径和字符偏移。
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var resumeContactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9-]+(?:\.[a-z0-9-]+)+`),
	regexp.MustCompile(`(?:\+?86[-\s]?)?1[3-9][0-9][-\s]?[0-9]{4}[-\s]?[0-9]{4}`),
	regexp.MustCompile(`(?i)(?:电话|手机|微信|QQ|联系方式|联系地址|家庭住址|身份证)\s*[:：]\s*[^\n，,；;|｜]{1,80}`),
	regexp.MustCompile(`(?i)(?:性别|gender|sex)\s*[:：]?\s*(?:男|女|male|female)`),
	regexp.MustCompile(`(?:年龄)\s*[:：]?\s*[0-9]{1,3}(?:岁)?`),
	regexp.MustCompile(`(?:出生日期|出生年月|生日)\s*[:：]?\s*[0-9年/月日.\-]{4,16}`),
	regexp.MustCompile(`(?:婚姻|婚育|民族|宗教)\s*[:：]?\s*[^\n，,；;|｜]{1,30}`),
	regexp.MustCompile(`(?:已婚|未婚|离异|已育|未育)`),
	regexp.MustCompile(`(?:^|[\s，,|｜])(?:男|女)(?:性)?(?:$|[\s，,|｜])`),
	regexp.MustCompile(`[0-9]{1,3}\s*岁`),
}

// redactResumeVectorText 等长遮盖敏感内容，偏移以原字段Unicode字符计数；姓名与联系方式不进入模型或搜索副本。
func redactResumeVectorText(text string, candidate PositionCandidate) string {
	for _, value := range []string{candidate.CandidateName, candidate.Phone, candidate.Email} {
		if strings.TrimSpace(value) != "" {
			text = strings.ReplaceAll(text, value, strings.Repeat("█", utf8.RuneCountInString(value)))
		}
	}
	for _, pattern := range resumeContactPatterns {
		text = pattern.ReplaceAllStringFunc(text, func(value string) string { return strings.Repeat("█", utf8.RuneCountInString(value)) })
	}
	return text
}

// appendResumeVectorChunks 分段覆盖整个字段而非只保留开头，每段最多1500字符，重叠150字符保存跨段上下文。
func appendResumeVectorChunks(items []resumeVectorChunk, kind, path, text string, candidate PositionCandidate) []resumeVectorChunk {
	runes := []rune(redactResumeVectorText(text, candidate))
	for start := 0; start < len(runes); {
		end := min(start+1500, len(runes))
		part := string(runes[start:end])
		if strings.Trim(part, " █\t\r\n") != "" {
			digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%s", path, start, end, part)))
			items = append(items, resumeVectorChunk{ID: hex.EncodeToString(digest[:12]), Kind: kind, Path: path, Text: part, Start: start, End: end})
		}
		if end == len(runes) {
			break
		}
		start = end - 150
	}
	return items
}

// buildResumeVectorChunks 建立一份职业概况与完整工作、项目、教育、证书和原文字段片段，返回可用于索引和证据核对的固定合同。
func buildResumeVectorChunks(candidate PositionCandidate) []resumeVectorChunk {
	items := []resumeVectorChunk{}
	summary := []string{}
	for _, value := range []string{candidate.ExpectedPosition, candidate.EducationLevel, candidate.WorkYears, candidateString(candidate.LibraryFields, "current_position")} {
		if value != "" {
			runes := []rune(value)
			if len(runes) > 1000 {
				runes = runes[:1000]
			}
			summary = append(summary, string(runes))
		}
	}
	// 概况只放短的明确职业字段；全部长内容始终在下面的原文字段保留。
	if len(summary) == 0 {
		summary = append(summary, "职业经历资料")
	}
	text := redactResumeVectorText(strings.Join(summary, "\n"), candidate)
	digest := sha256.Sum256([]byte("summary:" + text))
	items = append(items, resumeVectorChunk{ID: hex.EncodeToString(digest[:12]), Kind: "summary", Path: "career_summary", Text: text, End: utf8.RuneCountInString(text)})
	for i, entry := range candidate.WorkExperiences {
		prefix := fmt.Sprintf("work_experiences[%d]", i)
		items = appendResumeVectorChunks(items, "experience", prefix+".content", entry.Content, candidate)
		items = appendResumeVectorChunks(items, "experience", prefix+".position_name", entry.PositionName, candidate)
		items = appendResumeVectorChunks(items, "experience", prefix+".company_name", entry.CompanyName, candidate)
		if entry.StartYM != "" || entry.EndYM != "" {
			items = appendResumeVectorChunks(items, "experience", prefix+".period", entry.StartYM+" 至 "+entry.EndYM, candidate)
		}
	}
	for i, entry := range candidate.ProjectExperiences {
		prefix := fmt.Sprintf("project_experiences[%d]", i)
		items = appendResumeVectorChunks(items, "experience", prefix+".content", entry.Content, candidate)
		items = appendResumeVectorChunks(items, "experience", prefix+".project_name", entry.ProjectName, candidate)
		items = appendResumeVectorChunks(items, "experience", prefix+".role_name", entry.RoleName, candidate)
		if entry.StartYM != "" || entry.EndYM != "" {
			items = appendResumeVectorChunks(items, "experience", prefix+".period", entry.StartYM+" 至 "+entry.EndYM, candidate)
		}
	}
	for i, entry := range candidate.Educations {
		items = appendResumeVectorChunks(items, "experience", fmt.Sprintf("educations[%d]", i), entry.SchoolName+"\n"+entry.MajorName+"\n"+entry.EducationLevel, candidate)
	}
	for i, entry := range candidate.Certificates {
		items = appendResumeVectorChunks(items, "experience", fmt.Sprintf("certificates[%d]", i), entry.CertificateName+"\n"+entry.IssuedBy+"\n"+entry.IssuedYM, candidate)
	}
	for _, field := range []struct{ path, text string }{{"personal_description", candidate.PersonalDescription}, {"basic_info", candidate.BasicInfo}, {"raw_text", candidate.RawText}} {
		items = appendResumeVectorChunks(items, "experience", field.path, field.text, candidate)
	}
	return items
}
