// Package httpapi 本文件验证云端简历附件正文提取和严格结构化字段规则。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestParseCloudStructuredResumeRejectsLegacyAliases 验证旧字段别名不会再静默保存为空经历。
func TestParseCloudStructuredResumeRejectsLegacyAliases(t *testing.T) {
	_, err := parseCloudStructuredResume(`{
		"candidate_name":"邓云川","gender":"男","birth_ym":"1998","birth_ym_precision":"year_estimated",
		"phone":"17607080935","email":"","wechat":"","work_region":"成都","work_years":7,
		"expected_salary_min":14,"expected_salary_max":15,"education_level":"大专","expected_position":"全栈",
		"online_status":"在线","personal_description":"","work_status":"在职",
		"work_experiences":[{"company_name":"测试公司","position":"全栈","start_date":"2021-01","end_date":"","description":"开发"}],
		"educations":[],"certificates":[],"honors":[],"project_experiences":[],"colleague_communications":[]
	}`, "页面匿名姓名", "女")
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("旧字段别名没有被拒绝：%v", err)
	}
}

// TestParseCloudStructuredResumeKeepsExactExperienceFields 验证约定字段能完整保留工作和教育经历。
func TestParseCloudStructuredResumeKeepsExactExperienceFields(t *testing.T) {
	result, err := parseCloudStructuredResume(`{
		"candidate_name":"邓云川","gender":"男","birth_ym":"1998","birth_ym_precision":"year_estimated",
		"phone":"176 0708 0935","email":"A1224299352@GMAIL.COM","wechat":"","work_region":"成都","work_years":7,
		"expected_salary_min":14,"expected_salary_max":15,"education_level":"大专","expected_position":"全栈",
		"online_status":"在线","personal_description":"开发工程师","work_status":"在职",
		"work_experiences":[{"company_name":"四川数安值科技有限公司","position_name":"Java开发","content":"数据处理","start_ym":"2018-11","end_ym":"至今"}],
		"educations":[{"school_name":"江西信息学院","major_name":"软件工程","education_level":"大专","start_ym":"2014.09","end_ym":"2017.06"}],
		"certificates":[],"honors":[],"project_experiences":[],"colleague_communications":[]
	}`, "页面匿名姓名", "女")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.WorkExperiences) != 1 || result.WorkExperiences[0].PositionName != "Java开发" || result.WorkExperiences[0].StartYM != "2018-11" || result.WorkExperiences[0].EndYM != "" {
		t.Fatalf("工作经历字段没有正确保留：%+v", result.WorkExperiences)
	}
	if len(result.Educations) != 1 || result.Educations[0].MajorName != "软件工程" || result.Educations[0].StartYM != "2014-09" {
		t.Fatalf("教育经历字段没有正确保留：%+v", result.Educations)
	}
	if result.Phone != "17607080935" || result.Email != "a1224299352@gmail.com" {
		t.Fatalf("联系方式没有正确清洗：phone=%s email=%s", result.Phone, result.Email)
	}
	if result.CandidateName != "邓云川" || result.Gender != "男" {
		t.Fatalf("附件姓名和性别没有优先保留：name=%s gender=%s", result.CandidateName, result.Gender)
	}
}

// TestRequestCloudResumeStructureRetriesLegacyAliases 验证云端会拒绝旧字段别名并要求模型按正确字段重试。
func TestRequestCloudResumeStructureRetriesLegacyAliases(t *testing.T) {
	valid := `{
		"candidate_name":"邓云川","gender":"男","birth_ym":"1998","birth_ym_precision":"year_estimated",
		"phone":"17607080935","email":"1224299352@qq.com","wechat":"","work_region":"成都","work_years":"7",
		"expected_salary_min":14,"expected_salary_max":15,"education_level":"大专","expected_position":"全栈",
		"online_status":"在线","personal_description":"开发工程师","work_status":"在职",
		"work_experiences":[{"company_name":"附件公司","position_name":"全栈开发","content":"开发","start_ym":"2021-01","end_ym":""}],
		"educations":[],"certificates":[],"honors":[],"project_experiences":[],"colleague_communications":[]
	}`
	legacy := strings.Replace(valid, `"position_name"`, `"position"`, 1)
	attempts := 0
	service := &AutoReplyService{httpClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		attempts++
		content := legacy
		if attempts > 1 {
			content = valid
		}
		body, err := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": content}}},
			"usage":   map[string]any{"total_tokens": 10},
		})
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}}
	result, _, usedAttempts, tokens, err := service.requestCloudResumeStructure(
		context.Background(), AIConfig{BaseURL: "https://example.com/v1", Model: "test", APIKey: "test"},
		[]AIMsg{{Role: "system", Content: cloudResumeStructureSystemPrompt}},
		cloudResumeStructureRequest{CandidateName: "页面匿名姓名", Gender: "女"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || usedAttempts != 2 || tokens != 20 {
		t.Fatalf("重试统计不正确：requests=%d attempts=%d tokens=%d", attempts, usedAttempts, tokens)
	}
	if len(result.WorkExperiences) != 1 || result.WorkExperiences[0].PositionName != "全栈开发" {
		t.Fatalf("重试后经历字段仍不正确：%+v", result.WorkExperiences)
	}
}

// TestCallCloudResumeAIUsesDedicatedTimeout 验证真实附件结构化不会被通用 AI 超时提前取消。
func TestCallCloudResumeAIUsesDedicatedTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}],"usage":{"total_tokens":1}}`))
	}))
	defer server.Close()

	sharedClient := &http.Client{Timeout: time.Millisecond}
	service := &AutoReplyService{httpClient: sharedClient}
	content, tokens, err := service.callCloudResumeAI(context.Background(), AIConfig{
		BaseURL: server.URL, Model: "test", APIKey: "test",
	}, []AIMsg{{Role: "system", Content: cloudResumeStructureSystemPrompt}})
	if err != nil {
		t.Fatalf("专用超时没有覆盖通用短超时：%v", err)
	}
	if content != "{}" || tokens != 1 {
		t.Fatalf("AI 返回不完整：content=%q tokens=%d", content, tokens)
	}
	if sharedClient.Timeout != time.Millisecond {
		t.Fatalf("专用超时不应修改共享客户端，实际为 %s", sharedClient.Timeout)
	}
}

// TestExtractRealResumePDF 在提供真实附件路径时验证云端能直接读取 PDF 正文。
func TestExtractRealResumePDF(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("GOODHR_TEST_RESUME_PDF"))
	if path == "" {
		t.Skip("GOODHR_TEST_RESUME_PDF is not configured")
	}
	text, err := extractPDFResumeText(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"邓云川", "成都青与澜健康科技有限公司", "联系电话", "17607080935"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("PDF 正文没有读到 %q，正文开头：%s", expected, truncateAutoReplyText(text, 500))
		}
	}
}
