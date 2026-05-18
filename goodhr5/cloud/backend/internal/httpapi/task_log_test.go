// 本文件负责测试云端任务日志 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTaskLogAddAndList 验证任务日志可以写入和读取。
func TestTaskLogAddAndList(t *testing.T) {
	server := NewServer()
	routes := server.Routes()
	token := loginForTest(t, routes, "task-log@example.com")
	taskID := createTaskForTest(t, routes, token)

	// 调用任务日志写入接口，模拟任务运行时同步一条日志摘要。
	addReq := httptest.NewRequest(
		http.MethodPost,
		"/api/tasks/"+taskID+"/logs",
		bytes.NewBufferString(`{"level":"info","message":"任务已创建"}`),
	)
	addReq.Header.Set("Authorization", "Bearer "+token)
	addResp := httptest.NewRecorder()
	routes.ServeHTTP(addResp, addReq)
	if addResp.Code != http.StatusOK {
		t.Fatalf("add log status = %d, body = %s", addResp.Code, addResp.Body.String())
	}

	// 调用任务日志列表接口，供前端展开任务卡片查看运行摘要。
	listReq := httptest.NewRequest(http.MethodGet, "/api/tasks/"+taskID+"/logs", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list logs status = %d, body = %s", listResp.Code, listResp.Body.String())
	}

	var payload struct {
		Logs []struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		} `json:"logs"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Logs) != 1 || payload.Logs[0].Message != "任务已创建" {
		t.Fatalf("unexpected logs: %+v", payload.Logs)
	}
}

// TestTaskLogRejectsMissingTask 验证不存在的任务不能写入日志。
func TestTaskLogRejectsMissingTask(t *testing.T) {
	server := NewServer()
	routes := server.Routes()
	token := loginForTest(t, routes, "task-log-missing@example.com")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task_missing/logs", bytes.NewBufferString(`{"message":"x"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("add log status = %d, want %d", resp.Code, http.StatusNotFound)
	}
}

// createTaskForTest 调用任务创建接口，并返回任务 ID。
func createTaskForTest(t *testing.T, routes http.Handler, token string) string {
	t.Helper()

	// 调用任务创建接口，供日志测试拿到一个合法任务 ID。
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/tasks",
		bytes.NewBufferString(`{"platform_id":"boss","platform_account_id":"platform_account_1","mode":"keyword","match_limit":20}`),
	)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("create task status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Task.ID
}
