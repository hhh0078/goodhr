// Package ai 文件作用：验证自动回复简历结构化的提示词边界、字段清洗和动态数据隔离。
package ai

import (
	"strings"
	"testing"
)

// TestResumeExtractionMessagesKeepDynamicDataOutOfSystem 验证候选人和简历原文只进入动态 user 消息。
func TestResumeExtractionMessagesKeepDynamicDataOutOfSystem(t *testing.T) {
	input := ResumeExtractionInput{CandidateName: "缓存边界候选人", Gender: "女", ResumeText: "缓存边界简历原文"}
	messages, err := ResumeExtractionMessages(input)
	if err != nil || len(messages) != 2 {
		t.Fatalf("构造简历结构化消息失败：messages=%+v err=%v", messages, err)
	}
	for _, dynamic := range []string{input.CandidateName, input.ResumeText} {
		if strings.Contains(messages[0].Content, dynamic) {
			t.Fatalf("动态简历内容进入了固定 system 消息：%q", dynamic)
		}
		if !strings.Contains(messages[1].Content, dynamic) {
			t.Fatalf("动态简历内容没有进入 user 消息：%q", dynamic)
		}
	}
	if !strings.Contains(messages[1].Content, `"gender":"女"`) {
		t.Fatalf("页面性别没有进入动态 user 消息：%s", messages[1].Content)
	}
}

// TestParseResumeExtractionSanitizesUntrustedFields 验证联系方式、性别、日期和薪资异常不会直接进入云端。
func TestParseResumeExtractionSanitizesUntrustedFields(t *testing.T) {
	content := `{"candidate_name":"模型名字","gender":"未知","birth_ym":"1992-13","birth_ym_precision":"month","phone":"+86 136-3281-3031","email":"USER@EXAMPLE.COM","wechat":" candidate_wechat ","expected_salary_min":30,"expected_salary_max":20,"work_experiences":[{"company_name":"甲公司","position_name":"工程师","start_ym":"2020-01","end_ym":"至今"}],"educations":[{"school_name":"测试大学","major_name":"计算机","education_level":"本科","start_ym":"2011-09","end_ym":"2015-06"}]}`
	result, err := parseResumeExtraction(content, ResumeExtractionInput{
		CandidateName: "页面名字", Gender: "女", ResumeText: "完整原始简历",
	})
	if err != nil {
		t.Fatalf("解析结构化简历失败：%v", err)
	}
	if result.Candidate.CandidateName != "页面名字" || result.Gender != "女" {
		t.Fatalf("页面身份没有优先保留：%+v", result)
	}
	if result.Candidate.Phone != "+8613632813031" || result.Candidate.Email != "user@example.com" || result.Candidate.Wechat != "candidate_wechat" || result.Wechat != "candidate_wechat" {
		t.Fatalf("联系方式清洗不正确：%+v", result)
	}
	if result.Candidate.BirthYM != "" || result.BirthYMPrecision != "" {
		t.Fatalf("无效出生月份没有被清空：%+v", result)
	}
	if result.Candidate.ExpectedSalaryMin != nil || result.Candidate.ExpectedSalaryMax != nil {
		t.Fatalf("倒置薪资区间没有被清空：%+v", result.Candidate)
	}
	if result.Candidate.WorkExperiences[0].EndYM != "" || result.Candidate.Educations[0].EndYM != "2015-06" {
		t.Fatalf("经历时间清洗不正确：%+v", result.Candidate)
	}
	if result.Candidate.RawText != "完整原始简历" {
		t.Fatalf("原始简历没有按页面原文保存：%q", result.Candidate.RawText)
	}
}

// TestParseResumeExtractionAcceptsNumericWorkYears 验证 AI 把工作年限返回为数字时仍会统一保存为字符串。
func TestParseResumeExtractionAcceptsNumericWorkYears(t *testing.T) {
	result, err := parseResumeExtraction(`{"candidate_name":"邓云川","work_years":7}`, ResumeExtractionInput{
		CandidateName: "邓云川", ResumeText: "7年工作经验",
	})
	if err != nil {
		t.Fatalf("数字工作年限不应该导致整份简历解析失败：%v", err)
	}
	if result.Candidate.WorkYears != "7" {
		t.Fatalf("数字工作年限没有统一为字符串：%q", result.Candidate.WorkYears)
	}
}
