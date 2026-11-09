// Package ai 本文件验证本地推荐AI的资料边界和可信身份不被模型返回覆盖。
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAutoReplyRecommendationKeepsTrustedSnapshot 验证动态资料只进入user消息，模型伪造身份不能进入报告。
func TestAutoReplyRecommendationKeepsTrustedSnapshot(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Messages) != 2 || strings.Contains(request.Messages[0].Content, "动态候选人") || !strings.Contains(request.Messages[1].Content, "动态候选人") {
			t.Errorf("推荐提示词边界失效：%+v", request)
		}
		if len(request.Messages) == 2 {
			system := request.Messages[0].Content
			for _, rule := range []string{"现有资料未展示", "岗位薪酬范围待核实", "不得提出兼课、替岗、转岗", "评价范围仅限当前岗位", "match_score"} {
				if !strings.Contains(system, rule) {
					t.Errorf("模型实际请求缺少固定规则 %s", rule)
				}
			}
			if calls == 2 && !strings.Contains(system, "团队按岗位条件阅读报告") {
				t.Error("岗位事实约束覆盖了管理员业务提示词")
			}
		}
		content, _ := json.Marshal(`{"match_score":82,"recommendation_level":"建议推进","executive_summary":"满足已确认条件","candidate":{"id":"forged","name":"伪造身份"}}`)
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, content)
	}))
	defer server.Close()
	source := cloud.AutoReplyRecommendationReport{Candidate: cloud.AutoReplyReportCandidate{ID: "trusted", Name: "动态候选人"}, Position: cloud.AutoReplyReportPosition{ID: "position"}}
	for index, prompt := range []string{"", "团队按岗位条件阅读报告"} {
		report, err := New().GenerateAutoReplyRecommendation(context.Background(), cloud.AIConfig{BaseURL: server.URL, APIKey: "test", Model: "test"}, source, prompt)
		if err != nil || calls != index+1 || report.Candidate.ID != "trusted" || report.Candidate.Name != "动态候选人" || report.GeneratedAt.IsZero() {
			t.Fatalf("推荐快照不可信：%+v %v calls=%d", report, err, calls)
		}
	}
}

// TestRecommendationFieldShapes 验证现场面试重点文本无损保留；空要点、错误证据或缺失分数给出安全的字段提示。
func TestRecommendationFieldShapes(t *testing.T) {
	const base = `"match_score":90,"recommendation_level":"建议推进","executive_summary":"报告摘要"`
	content := "{" + base + `,"interview_focus":["核验具体负责的模块",{"title":"任职阶段","detail":"了解两段经历","evidence":["原文"]}],"suggested_questions":["如何衡量结果？"],"generated_at":"模型无权填写的时间"}`
	result, err := parseRecommendationAnalysis(content, cloud.AutoReplyRecommendationReport{})
	if err != nil || len(result.InterviewFocus) != 2 || result.InterviewFocus[0].Title != "核验具体负责的模块" ||
		len(result.InterviewFocus[0].Evidence) != 0 || result.InterviewFocus[1].Evidence[0] != "原文" || len(result.SuggestedQuestions) != 1 {
		t.Fatalf("面试重点解析失败：%+v %v", result.InterviewFocus, err)
	}
	for _, sample := range []struct{ field, value string }{
		{"unconfirmed_items[0]", "{" + base + `,"unconfirmed_items":[{"item":"敏感模型原文","label":"敏感模型原文"}]}`},
		{"risks[0]", "{" + base + `,"risks":[{"title":"敏感模型原文","evidence":[{}]}]}`},
		{"match_score", `{"executive_summary":"敏感模型原文"}`},
		{"suggested_questions", "{" + base + `,"suggested_questions":[{"title":"敏感模型原文"}]}`},
		{"JSON", "敏感模型原文"},
	} {
		_, err := parseRecommendationAnalysis(sample.value, cloud.AutoReplyRecommendationReport{})
		var invalid *RecommendationFormatError
		if !errors.As(err, &invalid) || invalid.field != sample.field || strings.Contains(err.Error(), "敏感模型原文") {
			t.Fatalf("未保留安全可定位的字段错误，预期%s，得到%v", sample.field, err)
		}
	}
}
