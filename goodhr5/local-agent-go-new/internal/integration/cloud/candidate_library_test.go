// Package cloud 文件作用：核实人工简历接口使用平铺字段、版本和幂等键，不混用旧自动任务候选人写入接口。
package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLibraryCandidateUsesFlatPayloadAndStableReceipts 验证新增、合并、备注和推荐请求合同。
func TestLibraryCandidateUsesFlatPayloadAndStableReceipts(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-web-token" {
			t.Error("丢失当前会话")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["idempotency_key"] == nil || body["candidate"] != nil {
			t.Errorf("不是平铺幂等合同：%+v", body)
		}
		switch r.URL.Path {
		case "/api/candidates":
			if r.Method != "POST" || body["phone"] != "13800138000" {
				t.Errorf("新增请求错误：%+v", body)
			}
		case "/api/candidates/candidate-1":
			if r.Method != "PATCH" || body["row_version"] != float64(3) {
				t.Errorf("合并未带版本：%+v", body)
			}
		case "/api/candidates/candidate-1/notes":
			if body["content"] != "备注" {
				t.Error("备注内容丢失")
			}
		case "/api/candidates/candidate-1/recommendations":
			if body["stage"] != "draft" || body["position_id"] != "position-1" {
				t.Error("推荐不是人工待推荐")
			}
		default:
			t.Errorf("错误API路径：%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"candidate":{"id":"candidate-1","row_version":4}}`))
	}))
	defer server.Close()
	client := New(server.URL)
	created, err := client.SaveLibraryCandidate(context.Background(), "test-web-token", "", json.RawMessage(` {"candidate_name":"测试甲","phone":"13800138000","expected_salary_min":10.5} `), "create-key")
	if err != nil || created.ID != "candidate-1" || created.RowVersion != 4 {
		t.Fatalf("候选人收据错误：%+v %v", created, err)
	}
	if _, err := client.SaveLibraryCandidate(context.Background(), "test-web-token", "candidate-1", json.RawMessage(`{"row_version":3,"candidate_name":"测试甲"}`), "patch-key"); err != nil {
		t.Fatal(err)
	}
	if err := client.AddLibraryNote(context.Background(), "test-web-token", "candidate-1", "备注", "note-key"); err != nil {
		t.Fatal(err)
	}
	if err := client.AddLibraryRecommendation(context.Background(), "test-web-token", "candidate-1", "position-1", "position-key"); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("请求数量错误：%d", calls)
	}
}
