// 本文件负责测试云端任务 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTaskCreateListDetail 验证任务可以创建、列表展示和读取详情。
func TestTaskCreateListDetail(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "task@example.com")
	positionID := createPositionForTest(t, routes, token)

	// 调用任务创建接口，保存云端任务元信息和统计摘要初始值。
	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/tasks",
		bytes.NewBufferString(`{"platform_id":"boss","platform_account_id":"platform_account_1","position_id":"`+positionID+`","mode":"keyword","match_limit":20}`),
	)
	createReq.Header.Set("Authorization", "Bearer "+token)
	createResp := httptest.NewRecorder()
	routes.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", createResp.Code, createResp.Body.String())
	}

	var createPayload struct {
		Task struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			Scanned     int    `json:"scanned_count"`
			MatchLimit  int    `json:"match_limit"`
			PlatformID  string `json:"platform_id"`
			AccountID   string `json:"platform_account_id"`
			PositionID  string `json:"position_id"`
			FilterMode  string `json:"mode"`
			LocalTaskID string `json:"local_task_id"`
		} `json:"task"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createPayload); err != nil {
		t.Fatal(err)
	}
	if createPayload.Task.ID == "" || createPayload.Task.Status != "created" {
		t.Fatalf("unexpected task payload: %+v", createPayload.Task)
	}
	if createPayload.Task.Scanned != 0 || createPayload.Task.MatchLimit != 20 {
		t.Fatalf("unexpected task stats: %+v", createPayload.Task)
	}
	if createPayload.Task.PositionID != positionID {
		t.Fatalf("position_id = %q", createPayload.Task.PositionID)
	}
	if createPayload.Task.LocalTaskID == "" {
		t.Fatal("local_task_id is empty")
	}

	// 调用任务列表接口，供云端控制台展示任务卡片。
	listReq := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body.String())
	}

	var listPayload struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Tasks) != 1 {
		t.Fatalf("tasks length = %d", len(listPayload.Tasks))
	}

	// 调用任务详情接口，供后续展开日志和候选人数据时使用。
	detailReq := httptest.NewRequest(http.MethodGet, "/api/tasks/"+createPayload.Task.ID, nil)
	detailReq.Header.Set("Authorization", "Bearer "+token)
	detailResp := httptest.NewRecorder()
	routes.ServeHTTP(detailResp, detailReq)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detailResp.Code, detailResp.Body.String())
	}
}

// createPositionForTest 调用岗位配置创建接口，并返回岗位模板 ID。
func createPositionForTest(t *testing.T, routes http.Handler, token string) string {
	t.Helper()

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/positions",
		bytes.NewBufferString(`{"name":"带货主播","keywords":["直播"]}`),
	)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("create position status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Position struct {
			ID string `json:"id"`
		} `json:"position"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Position.ID
}

// TestTaskCreateRejectsMissingAccount 验证创建任务时必须选择平台账号。
func TestTaskCreateRejectsMissingAccount(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "task-missing@example.com")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBufferString(`{"platform_id":"boss"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("create status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
}
