// 本文件通过真实路由验证独立备注、实际发生时间、重试冲突、权限和人工推荐历史排序。
package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestCandidateNotesAndRecommendationOccurredHTTP 验证先建简历再追加独立备注和推荐，历史按发生时间显示。
func TestCandidateNotesAndRecommendationOccurredHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "independent-notes@example.com")
	status, created := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "备注测试", "phone": "13800138000"})
	if status != http.StatusOK {
		t.Fatalf("create=%v", created)
	}
	id := created["candidate"].(map[string]any)["id"].(string)
	path := "/api/candidates/" + id + "/notes"
	occurred := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	note := map[string]any{"content": "第一行\n第二行", "occurred_at": occurred, "idempotency_key": "independent-one"}
	status, first := candidateTestRequest(t, routes, token, http.MethodPost, path, note)
	if status != http.StatusOK || first["note"].(map[string]any)["occurred_at"] != occurred {
		t.Fatalf("first=%v status=%d", first, status)
	}
	status, retry := candidateTestRequest(t, routes, token, http.MethodPost, path, note)
	if status != http.StatusOK || retry["note"].(map[string]any)["id"] != first["note"].(map[string]any)["id"] {
		t.Fatalf("retry=%v", retry)
	}
	note["occurred_at"] = time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	status, _ = candidateTestRequest(t, routes, token, http.MethodPost, path, note)
	if status != http.StatusConflict {
		t.Fatalf("changed time retry status=%d", status)
	}
	note["occurred_at"], note["content"] = occurred, "改写旧备注"
	status, _ = candidateTestRequest(t, routes, token, http.MethodPost, path, note)
	if status != http.StatusConflict {
		t.Fatalf("changed content retry status=%d", status)
	}
	for _, invalid := range []any{123, "tomorrow", time.Now().Add(72 * time.Hour).Format(time.RFC3339)} {
		status, _ = candidateTestRequest(t, routes, token, http.MethodPost, path, map[string]any{"content": "拒绝非法时间", "occurred_at": invalid})
		if status != http.StatusBadRequest {
			t.Fatalf("invalid note time=%v status=%d", invalid, status)
		}
	}
	status, _ = candidateTestRequest(t, routes, token, http.MethodPost, path, map[string]any{"content": strings.Repeat("字", 1001)})
	if status != http.StatusBadRequest {
		t.Fatalf("oversize note status=%d", status)
	}
	status, notes := candidateTestRequest(t, routes, token, http.MethodGet, path, nil)
	if status != http.StatusOK || len(notes["notes"].([]any)) != 1 {
		t.Fatalf("notes=%v", notes)
	}
	status, detail := candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates/"+id, nil)
	if status != http.StatusOK || detail["candidate"].(map[string]any)["latest_note"].(map[string]any)["content"] != "第一行\n第二行" {
		t.Fatalf("latest note=%v", detail)
	}
	for _, event := range detail["candidate"].(map[string]any)["events"].([]any) {
		if event.(map[string]any)["event_type"] == "manual_note" {
			t.Fatal("independent note duplicated in automatic events")
		}
	}
	other := loginForTest(t, routes, "notes-other-tenant@example.com")
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		status, _ = candidateTestRequest(t, routes, other, method, path, map[string]any{"content": "越权"})
		if status != http.StatusNotFound {
			t.Fatalf("unauthorized notes=%s %d", method, status)
		}
	}
	_, position := candidateTestRequest(t, routes, token, http.MethodPost, "/api/positions", map[string]any{"name": "推荐历史"})
	positionID := position["position"].(map[string]any)["id"].(string)
	recPath := "/api/candidates/" + id + "/recommendations"
	base := time.Now().UTC().Truncate(time.Second)
	var result map[string]any
	for index, days := range []int{2, 1, 3} {
		body := map[string]any{"position_id": positionID, "stage": "contacting", "content": "独立跟进", "idempotency_key": "rec-time-" + string(rune('a'+index)), "occurred_at": base.Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)}
		status, result = candidateTestRequest(t, routes, token, http.MethodPost, recPath, body)
		if status != http.StatusOK {
			t.Fatalf("rec time=%v status=%d", result, status)
		}
		status, result = candidateTestRequest(t, routes, token, http.MethodPost, recPath, body)
		if status != http.StatusOK || len(result["recommendation"].(map[string]any)["events"].([]any)) != index+1 {
			t.Fatalf("rec retry=%v", result)
		}
		body["occurred_at"] = base.Add(-6 * time.Hour).Format(time.RFC3339)
		status, _ = candidateTestRequest(t, routes, token, http.MethodPost, recPath, body)
		if status != http.StatusConflict {
			t.Fatalf("rec changed time status=%d", status)
		}
	}
	events := result["recommendation"].(map[string]any)["events"].([]any)
	if events[0].(map[string]any)["occurred_at"] != base.Add(-24*time.Hour).Format(time.RFC3339) || events[2].(map[string]any)["occurred_at"] != base.Add(-72*time.Hour).Format(time.RFC3339) {
		t.Fatalf("occurrence order=%v", events)
	}
	for _, invalid := range []any{123, "2026-14-99", time.Now().Add(72 * time.Hour).Format(time.RFC3339)} {
		status, _ = candidateTestRequest(t, routes, token, http.MethodPost, recPath, map[string]any{"position_id": positionID, "occurred_at": invalid})
		if status != http.StatusBadRequest {
			t.Fatalf("invalid recommendation time=%v status=%d", invalid, status)
		}
	}
}
