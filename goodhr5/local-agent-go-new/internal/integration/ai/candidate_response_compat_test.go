// Package ai 文件作用：用最小脱敏JSON回归候选评分与简历兼容解码、精度和原文错误位置，不使用实际诊断中的简历内容。
package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// candidateCompatPayload 为扁平与resume嵌套两种格式生成同形的脱敏响应。
func candidateCompatPayload(fields string, nested bool) string {
	if nested {
		return `{"analysis":{"score":80,"reason":"测试匹配"},"resume":{` + fields + `}}`
	}
	return `{"analysis":{"score":80,"reason":"测试匹配"},` + fields + `}`
}

// requireCandidateCompatFieldError 核对字段路径、原文完整保留与按原始UTF-8字节计算的错误位置。
func requireCandidateCompatFieldError(t *testing.T, content, field, badLiteral string) {
	t.Helper()
	_, err := parseDecisionWithStructured(content, 70, true)
	if err == nil {
		t.Fatalf("字段 %s 的错误形状被静默接受", field)
	}
	diagnostic, ok := ResponseDiagnosticFromError(err)
	if !ok || diagnostic.Response != content || !diagnostic.ResponseComplete || diagnostic.ResponseKind != "model_content" {
		t.Fatal("字段错误未保留完整原文与实际响应类型")
	}
	wantOffset := int64(strings.LastIndex(content, badLiteral) + len(badLiteral))
	if wantOffset < int64(len(badLiteral)) || diagnostic.ParseField != field || diagnostic.ParseOffset != wantOffset {
		t.Fatalf("字段或原文偏移不符：field=%q offset=%d，期望field=%q offset=%d", diagnostic.ParseField, diagnostic.ParseOffset, field, wantOffset)
	}
	var typeError *json.UnmarshalTypeError
	if !errors.As(err, &typeError) || typeError.Field != field {
		t.Fatalf("原JSON字段错误分类丢失：%v", err)
	}
}

// TestCandidateResponseCompatSalary 验证两种简历格式的薪资空值、零与有限数字表示不会互相混淆。
func TestCandidateResponseCompatSalary(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, tc := range []struct {
			name, raw string
			known     bool
			want      float64
		}{
			{"null", "null", false, 0}, {"空串", `""`, false, 0}, {"空白串", `" \t\n "`, false, 0},
			{"数字零", "0", true, 0}, {"数值串零", `"0"`, true, 0}, {"数字", "23.5", true, 23.5},
			{"有限数值串", `" 23.5 "`, true, 23.5}, {"指数数值串", `"1.25e2"`, true, 125},
		} {
			t.Run(fmt.Sprintf("nested=%t/%s", nested, tc.name), func(t *testing.T) {
				content := candidateCompatPayload(`"candidate_name":"合成样例","expected_salary_min":`+tc.raw+`,"expected_salary_max":`+tc.raw, nested)
				decision, err := parseDecisionWithStructured(content, 70, true)
				if err != nil || decision.Resume == nil {
					t.Fatalf("兼容薪资读取失败：%v", err)
				}
				for _, value := range []*float64{decision.Resume.ExpectedSalaryMin, decision.Resume.ExpectedSalaryMax} {
					if tc.known && (value == nil || *value != tc.want) || !tc.known && value != nil {
						t.Fatalf("已知数字与未知薪资混淆：known=%t value=%v", tc.known, value)
					}
				}
			})
		}
	}
}

// TestCandidateResponseCompatScore 验证评分沿用有限数值规则，缺失或错误类型不得被当作零分。
func TestCandidateResponseCompatScore(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, tc := range []struct {
			name, raw string
			valid     bool
			want      float64
		}{
			{"零", "0", true, 0}, {"零数值串", `" 0 "`, true, 0}, {"有限数值串", `"82.5"`, true, 82.5},
			{"下限", "-12", true, 0}, {"上限", "120", true, 100},
			{"缺失", "", false, 0}, {"null", "null", false, 0}, {"布尔", "true", false, 0},
			{"空串", `""`, false, 0}, {"空白串", `"  "`, false, 0}, {"NaN", `"NaN"`, false, 0},
			{"正无穷", `"+Inf"`, false, 0}, {"负无穷", `"-Infinity"`, false, 0}, {"溢出数值", "1e999", false, 0},
		} {
			t.Run(fmt.Sprintf("nested=%t/%s", nested, tc.name), func(t *testing.T) {
				fields := `"reason":"  测试原因  "`
				if tc.raw != "" {
					fields += `,"score":` + tc.raw
				}
				content, path := "{"+fields+"}", "score"
				if nested {
					content, path = `{"analysis":{`+fields+`},"score":false}`, "analysis.score"
				}
				decision, err := parseDecisionWithStructured(content, 70, false)
				if !tc.valid {
					if err == nil {
						t.Fatal("错误评分被当作有效分数")
					}
					diagnostic, ok := ResponseDiagnosticFromError(err)
					if !ok || diagnostic.ParseField != path || diagnostic.Response != content {
						t.Fatal("错误评分的字段或原文丢失")
					}
					return
				}
				if err != nil || decision.Score != tc.want || decision.Accepted != (tc.want >= 70) || decision.Reason != "测试原因" {
					t.Fatalf("评分旧规则改变：decision=%+v error=%v", decision, err)
				}
			})
		}
	}
}

// TestCandidateResponseCompatScoreOnlyIgnoresResume 验证关闭简历输出时不会解析任何附带的简历错误类型。
func TestCandidateResponseCompatScoreOnlyIgnoresResume(t *testing.T) {
	for _, badResume := range []string{"false", "3", `"wrong-shape"`, "[]", `{"phone":true,"work_experiences":false}`} {
		content := `{"analysis":{"score":"80","reason":"保持评分"},"resume":` + badResume + `,"candidate_name":true,"expected_salary_min":false,"educations":7,"certificates":{}}`
		decision, err := parseDecisionWithStructured(content, 70, false)
		if err != nil || !decision.Accepted || decision.Score != 80 || decision.Resume != nil || decision.Reason != "保持评分" {
			t.Fatalf("只评分被无关简历字段阻断：%v", err)
		}
	}
}

// TestCandidateResponseCompatSelectedBranch 验证只解析被选中的resume，非null分支优先，null才回退到根字段。
func TestCandidateResponseCompatSelectedBranch(t *testing.T) {
	content := `{"analysis":{"score":80},"score":false,"phone":false,"expected_salary_min":false,"educations":false,"resume":{"candidate_name":"合成样例","phone":900719925474099312345,"expected_salary_min":"20"}}`
	decision, err := parseDecisionWithStructured(content, 70, true)
	if err != nil || decision.Resume == nil || decision.Resume.Phone != "900719925474099312345" || decision.Resume.ExpectedSalaryMin == nil || *decision.Resume.ExpectedSalaryMin != 20 {
		t.Fatalf("未选根分支错误污染了有效resume：%v", err)
	}
	decision, err = parseDecisionWithStructured(`{"score":80,"resume":null,"candidate_name":"根样例"}`, 70, true)
	if err != nil || decision.Resume == nil || decision.Resume.CandidateName != "根样例" {
		t.Fatalf("null resume未回退有效根字段：%v", err)
	}
	for _, raw := range []string{"false", "3", `""`, "[]"} {
		requireCandidateCompatFieldError(t, `{"score":80,"candidate_name":"根样例","resume":`+raw+`}`, "resume", raw)
	}
}

// TestCandidateResponseCompatTextNumbers 验证文本字段中的长号码、末尾零和指数保持原始字面串，不经过浮点数。
func TestCandidateResponseCompatTextNumbers(t *testing.T) {
	for _, raw := range []string{"900719925474099312345", "1.2300", "1e+20", "-0"} {
		decision, err := parseDecisionWithStructured(candidateCompatPayload(`"candidate_name":"合成样例","phone":`+raw, true), 70, true)
		if err != nil || decision.Resume == nil || decision.Resume.Phone != raw {
			t.Fatalf("文本数字精度或字面表示改变：want=%s error=%v", raw, err)
		}
	}
}

// TestCandidateResponseCompatCollections 验证六类集合的确定空值兼容，拒绝非空错误形状，数组null占位不产生空记录。
func TestCandidateResponseCompatCollections(t *testing.T) {
	for _, field := range []string{"work_experiences", "educations", "certificates", "honors", "project_experiences", "colleague_communications"} {
		for _, raw := range []string{"[]", "null", `""`, `" \t\n "`, "[null,null]"} {
			t.Run(field+"/empty="+raw, func(t *testing.T) {
				decision, err := parseDecisionWithStructured(candidateCompatPayload(`"candidate_name":"合成样例","`+field+`":`+raw, true), 70, true)
				if err != nil || decision.Resume == nil {
					t.Fatalf("确定的空集合未兼容：%v", err)
				}
				value := reflect.ValueOf(*decision.Resume)
				for i := 0; i < value.NumField(); i++ {
					if strings.Split(value.Type().Field(i).Tag.Get("json"), ",")[0] == field && value.Field(i).Len() != 0 {
						t.Fatal("空集合或null占位生成了虚假经历")
					}
				}
			})
		}
		for _, raw := range []string{`"not-empty"`, "false", "7", "{}", `{"value":"singleton"}`, "[false]", "[[1]]"} {
			path, bad := "resume."+field, raw
			if raw == "[false]" {
				path, bad = path+"[0]", "false"
			} else if raw == "[[1]]" {
				path, bad = path+"[0]", "[1]"
			}
			t.Run(field+"/invalid="+raw, func(t *testing.T) {
				requireCandidateCompatFieldError(t, candidateCompatPayload(`"candidate_name":"合成样例","`+field+`":`+raw, true), path, bad)
			})
		}
	}
}

// TestCandidateResponseCompatOriginalOffsets 验证中文前缀、Markdown、嵌套数组和已过滤null不改变原始字段下标与字节偏移。
func TestCandidateResponseCompatOriginalOffsets(t *testing.T) {
	for _, tc := range []struct{ field, textField string }{
		{"work_experiences", "company_name"}, {"educations", "school_name"}, {"certificates", "certificate_name"},
		{"honors", "honor_name"}, {"project_experiences", "project_name"}, {"colleague_communications", "communicator_name"},
	} {
		content := "中文说明：仅为合成测试\n```json\n" + candidateCompatPayload(`"candidate_name":"合成样例","`+tc.field+`":[null,{"`+tc.textField+`":"有效"},{"`+tc.textField+`" :\n false\t }]`, true) + "\n```\n原始后记"
		// 上面的换行和制表符必须是原始JSON空白，而不是字符串外的反斜杠文字。
		content = strings.ReplaceAll(strings.ReplaceAll(content, `\n false`, "\n false"), `false\t`, "false\t")
		requireCandidateCompatFieldError(t, content, "resume."+tc.field+"[2]."+tc.textField, "false")
	}
	for _, raw := range []string{"true", `"NaN"`, `"+Inf"`, "1e999"} {
		content := "合成前言\n```json\n" + candidateCompatPayload(`"candidate_name":"合成样例","expected_salary_min":`+raw, true) + "\n```"
		requireCandidateCompatFieldError(t, content, "resume.expected_salary_min", raw)
	}
}

// TestCandidateResponseCompatValidContent 验证正常完整简历逐字段保留，日期只保持原文，不推算月份。
func TestCandidateResponseCompatValidContent(t *testing.T) {
	minSalary, maxSalary := 12.5, 25.0
	expected := cloud.StructuredCandidate{CandidateName: "合成样例", BirthYM: "1990", Phone: "900719925474099312345", Email: "sample@example.invalid", Wechat: "synthetic", WorkRegion: "测试地区", WorkYears: "12", ExpectedSalaryMin: &minSalary, ExpectedSalaryMax: &maxSalary, EducationLevel: "测试学历", ExpectedPosition: "测试岗位", OnlineStatus: "测试状态", PersonalDescription: "合成简介", WorkStatus: "仅测试", RawText: "合成原文\n保留换行",
		WorkExperiences:         []cloud.CandidateWorkExperience{{CompanyName: "合成单位", PositionName: "测试岗位", Content: "合成工作", StartYM: "2020", EndYM: "至今"}},
		Educations:              []cloud.CandidateEducation{{SchoolName: "合成学校", MajorName: "测试专业", EducationLevel: "测试学历", StartYM: "2010", EndYM: "2014"}},
		Certificates:            []cloud.CandidateCertificate{{CertificateName: "合成证书", IssuedBy: "合成机构", IssuedYM: "2015"}},
		Honors:                  []cloud.CandidateHonor{{HonorName: "合成荣誉", IssuedBy: "合成单位", IssuedYM: "2016", Description: "合成说明"}},
		ProjectExperiences:      []cloud.CandidateProjectExperience{{ProjectName: "合成项目", RoleName: "合成角色", Content: "项目说明", StartYM: "2021", EndYM: "2022"}},
		ColleagueCommunications: []cloud.CandidateCommunication{{CommunicatorName: "测试角色", CommunicatedAt: "2024", Content: "合成沟通"}}}
	encoded, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	for _, nested := range []bool{false, true} {
		content := candidateCompatPayload(strings.TrimSuffix(strings.TrimPrefix(string(encoded), "{"), "}"), nested)
		decision, err := parseDecisionWithStructured(content, 70, true)
		if err != nil || decision.Resume == nil || !reflect.DeepEqual(*decision.Resume, expected) {
			t.Fatalf("完整正常简历内容被删改：nested=%t error=%v", nested, err)
		}
	}
}

// TestCandidateResponseCompatReasonAndPrompt 验证原因旧规则及结构化提示词开关，不把普通评分任务变成简历任务。
func TestCandidateResponseCompatReasonAndPrompt(t *testing.T) {
	for _, raw := range []string{"null", `"  "`} {
		decision, err := parseDecisionWithStructured(`{"score":80,"reason":`+raw+`}`, 70, false)
		if err != nil || decision.Reason != "AI 没有给出原因" {
			t.Fatalf("空原因旧规则改变：%v", err)
		}
	}
	longReason := strings.Repeat("合成说明", 5000)
	encoded, _ := json.Marshal(longReason)
	decision, err := parseDecisionWithStructured(`{"analysis":{"score":80,"reason":`+string(encoded)+`},"reason":false}`, 70, false)
	if err != nil || utf8.RuneCountInString(decision.Reason) != 16000 || decision.Reason != string([]rune(longReason)[:16000]) {
		t.Fatal("原因截断不再按字符或错误读取未选根原因")
	}
	requireCandidateCompatFieldError(t, `{"analysis":{"score":80,"reason":false}}`, "analysis.reason", "false")
	for _, base := range []string{"请按岗位评分", "输出格式：${结构化简历}"} {
		disabled := applyStructuredCandidatePrompt(base, false)
		if strings.Contains(disabled, "candidate_name") || strings.Contains(disabled, "expected_salary_min") || strings.Contains(disabled, "结构化简历字段类型") {
			t.Fatal("关闭结构化简历后仍额外要求简历字段")
		}
		enabled := applyStructuredCandidatePrompt(base, true)
		if !strings.Contains(enabled, "candidate_name") || !strings.Contains(enabled, "expected_salary_min") || !strings.Contains(enabled, "null") || !strings.Contains(enabled, "[]") {
			t.Fatal("开启结构化简历后缺少字段与空值类型约定")
		}
	}
}
