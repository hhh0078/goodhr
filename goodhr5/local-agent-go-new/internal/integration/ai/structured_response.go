// Package ai 文件作用：定义结构化简历提示词，并解析 AI 完整响应中的评分和简历字段。
package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

const structuredResumeExample = `{
  "analysis": {"score": 80, "reason": "匹配核心要求"},
  "candidate_name": "候选人姓名",
  "birth_ym": "1990-05",
  "phone": "",
  "email": "",
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
	return prompt
}

// parseDecisionWithStructured 从完整 AI 响应中解析评分和可选结构化简历。
func parseDecisionWithStructured(content string, threshold float64, includeResume bool) (Decision, error) {
	var parsed struct {
		Score    *float64 `json:"score"`
		Reason   string   `json:"reason"`
		Analysis *struct {
			Score  *float64 `json:"score"`
			Reason string   `json:"reason"`
		} `json:"analysis"`
		cloud.StructuredCandidate
		Resume *cloud.StructuredCandidate `json:"resume"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &parsed); err != nil {
		return Decision{}, fmt.Errorf("AI 判断格式不正确：%w", err)
	}
	score := parsed.Score
	reason := parsed.Reason
	if parsed.Analysis != nil {
		score = parsed.Analysis.Score
		reason = parsed.Analysis.Reason
	}
	if score == nil {
		return Decision{}, fmt.Errorf("AI 判断没有返回 score，当前岗位提示词格式可能需要兼容")
	}
	decision := normalizedDecision(*score, reason, threshold)
	if !includeResume {
		return decision, nil
	}
	resume := parsed.StructuredCandidate
	if parsed.Resume != nil {
		resume = *parsed.Resume
	}
	if hasStructuredCandidate(resume) {
		decision.Resume = &resume
	}
	return decision, nil
}

// hasStructuredCandidate 判断完整 AI 响应是否真正包含可保存的简历字段。
func hasStructuredCandidate(candidate cloud.StructuredCandidate) bool {
	return strings.TrimSpace(candidate.CandidateName) != "" ||
		strings.TrimSpace(candidate.RawText) != "" ||
		strings.TrimSpace(candidate.PersonalDescription) != "" ||
		len(candidate.WorkExperiences) > 0 ||
		len(candidate.Educations) > 0 ||
		len(candidate.ProjectExperiences) > 0
}
