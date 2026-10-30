// Package ai 本文件验证本地推荐AI的资料边界和可信身份不被模型返回覆盖。
package ai

import (
	"context"
	"encoding/json"
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
		content, _ := json.Marshal(`{"match_score":82,"recommendation_level":"建议推进","executive_summary":"满足已确认条件","candidate":{"id":"forged","name":"伪造身份"}}`)
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, content)
	}))
	defer server.Close()
	source := cloud.AutoReplyRecommendationReport{Candidate: cloud.AutoReplyReportCandidate{ID: "trusted", Name: "动态候选人"}, Position: cloud.AutoReplyReportPosition{ID: "position"}}
	report, err := New().GenerateAutoReplyRecommendation(context.Background(), cloud.AIConfig{BaseURL: server.URL, APIKey: "test", Model: "test"}, source)
	if err != nil || calls != 1 || report.Candidate.ID != "trusted" || report.Candidate.Name != "动态候选人" || report.GeneratedAt.IsZero() {
		t.Fatalf("推荐快照不可信：%+v %v calls=%d", report, err, calls)
	}
}
