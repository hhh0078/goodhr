// Package localai 负责测试本地 AI 调用和评分解析。
package localai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	client := New(localdb.AIConfig{BaseURL: server.URL, APIKey: "key-1", Model: "model-1", Timeout: 5}).WithProgress(func(text string) {
		updates = append(updates, text)
	})
	decision, err := client.ScoreForGreet(
		t.Context(),
		map[string]any{"description": "需要销售经验"},
		map[string]any{"raw_text": "销售经验丰富"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ShouldGreet || decision.Score != 88 || len(updates) < 2 {
		t.Fatalf("decision=%+v updates=%v", decision, updates)
	}
}
