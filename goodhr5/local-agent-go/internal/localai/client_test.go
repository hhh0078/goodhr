// Package localai 负责测试本地 AI 调用和评分解析。
package localai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go/internal/localdb"
)

// TestScoreForGreet 验证 OpenAI 兼容接口评分流程。
func TestScoreForGreet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer key-1" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"score": 76, "reason": "匹配销售经验"}`}},
			},
		})
	}))
	defer server.Close()

	client := New(localdb.AIConfig{BaseURL: server.URL, APIKey: "key-1", Model: "model-1", Timeout: 5})
	decision, err := client.ScoreForGreet(
		t.Context(),
		map[string]any{"name": "销售", "description": "需要销售经验"},
		map[string]any{"raw_text": "三年销售经验，本科"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ShouldGreet || decision.Score != 76 || decision.Reason != "匹配销售经验" {
		t.Fatalf("decision = %+v", decision)
	}
}

// TestParseScoreJSON 验证 Markdown 包裹的 JSON 也能解析。
func TestParseScoreJSON(t *testing.T) {
	score, reason, err := parseScoreJSON("```json\n{\"score\":65,\"reason\":\"可沟通\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if score != 65 || reason != "可沟通" {
		t.Fatalf("score=%v reason=%s", score, reason)
	}
}

// TestParseScoreJSONAnalysis 验证 analysis 包裹的评分结果可以解析。
func TestParseScoreJSONAnalysis(t *testing.T) {
	score, reason, err := parseScoreJSON(`{"analysis":{"score":72,"reason":"学历经验达标"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if score != 72 || reason != "学历经验达标" {
		t.Fatalf("score=%v reason=%s", score, reason)
	}
}

// TestParseVisionScoreJSONWithResumeAnalysis 验证结构化简历只从 analysis 取本次分数。
func TestParseVisionScoreJSONWithResumeAnalysis(t *testing.T) {
	score, reason, _, resumeData, err := parseVisionScoreJSONWithResume(`{
		"analysis":{"score":75,"reason":"经验匹配"},
		"candidate_name":"徐英",
		"ai":{"greet":{"score":10,"reason":"不该读取"}}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if score != 75 || reason != "经验匹配" {
		t.Fatalf("score=%v reason=%s", score, reason)
	}
	if _, ok := resumeData["ai_greet_score"]; ok {
		t.Fatalf("resumeData should not include ai_greet_score: %+v", resumeData)
	}
}

// TestParseVisionScoreJSONWithoutResume 验证关闭结构化简历时不会生成空简历数据。
func TestParseVisionScoreJSONWithoutResume(t *testing.T) {
	score, reason, _, resumeData, err := parseVisionScoreJSONWithResume(`{
		"analysis":{"score":80,"reason":"原因"}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if score != 80 || reason != "原因" {
		t.Fatalf("score=%v reason=%s", score, reason)
	}
	if resumeData != nil {
		t.Fatalf("resumeData should be nil: %+v", resumeData)
	}
}

// TestBuildResumeJSONExampleSwitch 验证岗位开关会控制 AI 是否输出简历字段。
func TestBuildResumeJSONExampleSwitch(t *testing.T) {
	closed := buildVisionSystemPrompt(map[string]any{
		"common_config": map[string]any{"output_structured_resume": false},
	})
	if strings.Contains(closed, "candidate_name") {
		t.Fatalf("closed prompt should not include resume fields: %s", closed)
	}
	opened := buildVisionSystemPrompt(map[string]any{
		"common_config": map[string]any{"output_structured_resume": true},
	})
	if !strings.Contains(opened, "candidate_name") {
		t.Fatalf("opened prompt should include resume fields: %s", opened)
	}
}

// TestChatStreamProgress 验证流式 AI 响应会实时回调完整文本。
func TestChatStreamProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"score\\\":\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" 88, \\\"reason\\\": \\\"合适\\\"}\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	updates := []string{}
	earlyDecisions := []Decision{}
	client := New(localdb.AIConfig{BaseURL: server.URL, APIKey: "key-1", Model: "model-1", Timeout: 5}).WithProgress(func(text string) {
		updates = append(updates, text)
	}).WithEarlyDecision(func(decision Decision) {
		earlyDecisions = append(earlyDecisions, decision)
	})
	decision, err := client.ScoreForGreet(
		t.Context(),
		map[string]any{"description": "需要销售经验"},
		map[string]any{"raw_text": "销售经验丰富"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ShouldGreet || decision.Score != 88 || len(updates) < 2 || len(earlyDecisions) != 1 {
		t.Fatalf("decision=%+v updates=%v early=%+v", decision, updates, earlyDecisions)
	}
	if !earlyDecisions[0].ShouldGreet || earlyDecisions[0].Threshold != 70 {
		t.Fatalf("early decision = %+v", earlyDecisions[0])
	}
}

// TestTryExtractScoreDecisionFromStream 验证能从复杂流式累计文本中提取评分 JSON。
func TestTryExtractScoreDecisionFromStream(t *testing.T) {
	content := strings.Join([]string{
		"AI分析如下：",
		"```json",
		`{"candidate_name":"张三","ai":{"greet":{"score":"82.5","reason":"经验匹配"}},"raw_text":"这里有 { 字符 }"}`,
		"```",
	}, "\n")
	decision, ok := TryExtractScoreDecisionFromStream(content)
	if !ok {
		t.Fatal("未提前提取到评分 JSON")
	}
	if decision.Score != 82.5 || decision.Reason != "经验匹配" {
		t.Fatalf("decision = %+v", decision)
	}
}

// TestTryExtractScoreDecisionFromBrokenOuterStream 验证外层 JSON 未完整时也能提取内部评分对象。
func TestTryExtractScoreDecisionFromBrokenOuterStream(t *testing.T) {
	content := `["ai":{"greet":{"score":95.0,"reason":"具备服装及男装直播带货经验，有百万GMV业绩，完全符合岗位要求。"}},"candidate_name":"邓英杰"`
	decision, ok := TryExtractScoreDecisionFromStream(content)
	if !ok {
		t.Fatal("未从不完整外层文本中提前提取评分 JSON")
	}
	if decision.Score != 95 || !strings.Contains(decision.Reason, "服装") {
		t.Fatalf("decision = %+v", decision)
	}
}
