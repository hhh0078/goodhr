// Package ai 文件作用：定义结构化简历提示词，并解析 AI 完整响应中的评分和简历字段。
package ai

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

const structuredResumeExample = `{
  "analysis": {"score": 80, "reason": "匹配核心要求"},
  "candidate_name": "候选人姓名",
  "birth_ym": "1990-05",
  "phone": "",
  "email": "",
  "wechat": "",
  "work_region": "上海",
  "work_years": "10年以上",
  "expected_salary_min": 20,
  "expected_salary_max": 30,
  "education_level": "本科",
  "expected_position": "目标岗位",
  "online_status": "刚刚活跃",
  "personal_description": "个人简介",
  "work_status": "在职-月内到岗",
  "raw_text": "完整简历文字",
  "work_experiences": [{"company_name":"","position_name":"","content":"","start_ym":"","end_ym":""}],
  "educations": [{"school_name":"","major_name":"","education_level":"","start_ym":"","end_ym":""}],
  "certificates": [{"certificate_name":"","issued_by":"","issued_ym":""}],
  "honors": [{"honor_name":"","issued_by":"","issued_ym":"","description":""}],
  "project_experiences": [{"project_name":"","role_name":"","content":"","start_ym":"","end_ym":""}],
  "colleague_communications": [{"communicator_name":"","communicated_at":"","content":""}]
}`

// structuredCandidatePrompt 根据岗位开关返回评分或结构化简历输出格式。
func structuredCandidatePrompt(enabled bool) string {
	if !enabled {
		return `{"score":80,"reason":"匹配核心要求"}`
	}
	return structuredResumeExample
}

// applyStructuredCandidatePrompt 替换旧提示词占位符，并在开启时补齐结构化输出要求。
func applyStructuredCandidatePrompt(prompt string, enabled bool) string {
	format := structuredCandidatePrompt(enabled)
	prompt = strings.ReplaceAll(prompt, "${结构化简历}", format)
	if enabled && !strings.Contains(prompt, "candidate_name") {
		prompt += "\n请严格按照下面的 JSON 结构返回。analysis 必须放在最前面，后续简历字段可以继续流式输出：\n" + format
	}
	if enabled {
		prompt += "\n结构化简历字段类型：expected_salary_min、expected_salary_max只能为有限JSON数字，未提供薪资时返回null，不能用空字符串、面议、带单位文字或0代替未知；其它文字字段使用字符串，日期保留原文明确的年月，不推算缺失月份；经历、教育、证书、荣誉、项目和沟通记录必须使用数组，没有记录时返回[]。"
	}
	return prompt
}

// parseDecisionWithStructured 从完整 AI 响应中解析评分和可选结构化简历。
func parseDecisionWithStructured(content string, threshold float64, includeResume bool) (Decision, error) {
	object := extractJSONObject(content)
	fields, err := candidateObject([]byte(object), 0)
	if err != nil {
		return Decision{}, newParsedResponseError(fmt.Errorf("AI 判断格式不正确：%w", err), content, object, "model_content")
	}
	decisionFields, prefix := fields, ""
	if analysis, exists := fields["analysis"]; exists && string(analysis.Raw) != "null" {
		decisionFields, err = candidateObject(analysis.Raw, analysis.Offset)
		if err != nil {
			return Decision{}, newParsedResponseError(fmt.Errorf("AI 判断格式不正确：%w", candidateFieldError(analysis, "analysis", reflect.TypeOf(map[string]json.RawMessage{}))), content, object, "model_content")
		}
		prefix = "analysis."
	}
	scoreField, exists := decisionFields["score"]
	if !exists || string(scoreField.Raw) == "null" {
		responseErr := newResponseError(fmt.Errorf("AI 判断没有返回 score，当前岗位提示词格式可能需要兼容"), content, "model_content", true)
		responseErr.Diagnostic.ParseField = prefix + "score"
		return Decision{}, responseErr
	}
	score, err := candidateNumber(scoreField, prefix+"score")
	if err != nil {
		return Decision{}, newParsedResponseError(fmt.Errorf("AI 判断格式不正确：%w", err), content, object, "model_content")
	}
	var reason string
	if field, exists := decisionFields["reason"]; exists && json.Unmarshal(field.Raw, &reason) != nil {
		return Decision{}, newParsedResponseError(fmt.Errorf("AI 判断格式不正确：%w", candidateFieldError(field, prefix+"reason", reflect.TypeOf(""))), content, object, "model_content")
	}
	decision := normalizedDecision(score, reason, threshold)
	if !includeResume {
		return decision, nil
	}
	resumeField := candidateJSONField{Raw: json.RawMessage(object)}
	path := ""
	if nested, exists := fields["resume"]; exists && string(nested.Raw) != "null" {
		resumeField, path = nested, "resume"
	}
	normalized, err := normalizeCandidateField(resumeField, reflect.TypeOf(cloud.StructuredCandidate{}), path)
	if err != nil {
		return Decision{}, newParsedResponseError(fmt.Errorf("AI 简历字段格式不正确：%w", err), content, object, "model_content")
	}
	var resume cloud.StructuredCandidate
	if err := json.Unmarshal(normalized, &resume); err != nil {
		return Decision{}, newResponseError(fmt.Errorf("AI 简历字段格式不正确：%w", err), content, "model_content", true)
	}
	if hasStructuredCandidate(resume) {
		decision.Resume = &resume
	}
	return decision, nil
}

// hasStructuredCandidate 判断完整 AI 响应是否真正包含可保存的简历字段。
func hasStructuredCandidate(candidate cloud.StructuredCandidate) bool {
	return strings.TrimSpace(candidate.CandidateName) != "" ||
		strings.TrimSpace(candidate.Phone) != "" ||
		strings.TrimSpace(candidate.Email) != "" ||
		strings.TrimSpace(candidate.Wechat) != "" ||
		strings.TrimSpace(candidate.RawText) != "" ||
		strings.TrimSpace(candidate.PersonalDescription) != "" ||
		len(candidate.WorkExperiences) > 0 ||
		len(candidate.Educations) > 0 ||
		len(candidate.ProjectExperiences) > 0
}
