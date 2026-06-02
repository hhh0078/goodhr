// 本文件用于测试任务执行器中的图片 AI 简历结构化解析和候选人入库映射。
package httpapi

import "testing"

// TestCleanAITextOutputRemovesMarkdownJSONFence 验证 AI 返回 Markdown JSON 代码块时可被清理解析。
func TestCleanAITextOutputRemovesMarkdownJSONFence(t *testing.T) {
	raw := "```json\n{\"score\": 50, \"reason\": \"客服有销售迁移可能，但缺乏明确销售或助教经验，需核验\"}\n```"
	cleaned := cleanAITextOutput(raw)
	want := "{\"score\": 50, \"reason\": \"客服有销售迁移可能，但缺乏明确销售或助教经验，需核验\"}"
	if cleaned != want {
		t.Fatalf("AI 输出代码块清理失败\nwant=%s\ngot=%s", want, cleaned)
	}
	var decision AIScoreDecision
	if err := tryDecodeJSON(raw, &decision); err != nil {
		t.Fatalf("代码块 JSON 应该可以解析: %v", err)
	}
	if decision.Score != 50 {
		t.Fatalf("分数解析错误: %+v", decision)
	}
}

// TestVisionDetailResumePersistsToCandidateStore 验证图片 AI 返回的结构化简历可以合并并入库。
func TestVisionDetailResumePersistsToCandidateStore(t *testing.T) {
	raw := `{
		"resume": {
			"candidate_name": "张女士",
			"birth_ym": "1998-01",
			"phone": "13800000000",
			"email": "zhang@example.com",
			"work_region": "德阳·旌阳区",
			"work_years": "6年",
			"expected_salary_min": 5000,
			"expected_salary_max": 8000,
			"basic_info": "26岁丨本科丨6年丨离职-随时到岗",
			"education_level": "本科",
			"expected_position": "课程顾问",
			"online_status": "刚刚活跃",
			"personal_description": "有课程销售经验，沟通跟进能力较好。",
			"work_status": "离职-随时到岗",
			"raw_text": "张女士刚刚活跃 26岁丨本科丨6年丨离职-随时到岗。",
			"filter_text": "本科，6年工作经验，课程顾问经历，期望德阳。",
			"work_experiences": [
				{
					"company_name": "成都某教育咨询有限公司",
					"position_name": "课程顾问",
					"content": "负责课程咨询、销售转化和学员跟进。",
					"start_ym": "2024-03",
					"end_ym": "2026-05"
				}
			],
			"educations": [
				{
					"school_name": "四川某大学",
					"major_name": "市场营销",
					"education_level": "本科",
					"campus_experience": "参加校内招生推广活动。",
					"start_ym": "2018-09",
					"end_ym": "2022-06"
				}
			],
			"certificates": ["普通话二级甲等"],
			"honors": ["年度销售冠军"],
			"project_experiences": [],
			"colleague_communications": [
				{
					"content": "同事曾沟通过课程顾问岗位。",
					"time": "2026-05-14 14:16"
				}
			],
			"resume_attachment_extracted_text": "张女士完整简历文字",
			"ext": {
				"source_note": "图片中还有合作客户专享"
			}
		},
		"analysis": {
			"score": 85,
			"reason": "销售经验匹配",
			"should_greet": true
		}
	}`

	visionResult, ok := parseVisionDetailDecision(raw)
	if !ok {
		t.Fatal("图片 AI JSON 应该可以解析")
	}
	if visionResult.Score != 85 || visionResult.Reason != "销售经验匹配" || !visionResult.ShouldGreet {
		t.Fatalf("分析结果解析错误: %+v", visionResult)
	}

	candidate := Candidate{
		PlatformCandidateID: "boss_001",
		Name:                "列表姓名",
	}
	applyVisionResumeToCandidate(&candidate, visionResult.Resume)

	store := NewMemoryCandidateStore()
	executor := &TaskExecutor{
		task: TaskRun{
			ID:                "task_001",
			UserEmail:         "user@example.com",
			PlatformID:        "boss",
			PlatformAccountID: "account_001",
			PositionID:        "position_001",
		},
		candidateStore: store,
	}
	persistence, err := executor.prepareCandidatePersistence(candidate, "列表基础信息", "筛选文本", visionResult.ResumeText)
	if err != nil {
		t.Fatalf("候选人入库失败: %v", err)
	}

	profile := persistence.Profile
	if profile.CandidateName != "张女士" {
		t.Fatalf("候选人姓名未入库，got=%q", profile.CandidateName)
	}
	if profile.WorkStatus != "离职-随时到岗" {
		t.Fatalf("工作状态未入库，got=%q", profile.WorkStatus)
	}
	if profile.ResumeText != "张女士完整简历文字" {
		t.Fatalf("完整简历文本未入库，got=%q", profile.ResumeText)
	}
	if len(profile.WorkExperiences) != 1 || profile.WorkExperiences[0].CompanyName != "成都某教育咨询有限公司" {
		t.Fatalf("工作经历未入库，got=%+v", profile.WorkExperiences)
	}
	if len(profile.Educations) != 1 || profile.Educations[0].SchoolName != "四川某大学" {
		t.Fatalf("教育经历未入库，got=%+v", profile.Educations)
	}
	if len(profile.Communications) != 1 || profile.Communications[0].Time != "2026-05-14 14:16" {
		t.Fatalf("同事沟通记录未入库，got=%+v", profile.Communications)
	}
}
