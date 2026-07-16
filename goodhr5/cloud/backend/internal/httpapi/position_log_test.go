// 本文件负责测试云端岗位日志 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestPositionLogAddAndList 验证岗位日志可以写入和读取。
func TestPositionLogAddAndList(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position-log@example.com")
	positionID := createPositionForTest(t, routes, token)

	// 调用岗位日志写入接口，模拟岗位运行时同步一条日志摘要。
	addReq := httptest.NewRequest(
		http.MethodPost,
		"/api/positions/"+positionID+"/logs",
		bytes.NewBufferString(`{"level":"info","message":"岗位已创建"}`),
	)
	addReq.Header.Set("Authorization", "Bearer "+token)
	addResp := httptest.NewRecorder()
	routes.ServeHTTP(addResp, addReq)
	if addResp.Code != http.StatusOK {
		t.Fatalf("add log status = %d, body = %s", addResp.Code, addResp.Body.String())
	}

	// 调用岗位日志列表接口，供前端展开岗位卡片查看运行摘要。
	listReq := httptest.NewRequest(http.MethodGet, "/api/positions/"+positionID+"/logs", nil)
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
	if len(payload.Logs) != 1 || payload.Logs[0].Message != "岗位已创建" {
		t.Fatalf("unexpected logs: %+v", payload.Logs)
	}
}

// TestPositionStopUpdatesStatusAndKeepsLogs 验证岗位停止接口会更新状态并追加日志，不会清空原有日志。
func TestPositionStopUpdatesStatusAndKeepsLogs(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position-stop@example.com")
	positionID := createPositionForTest(t, routes, token)

	addReq := httptest.NewRequest(http.MethodPost, "/api/positions/"+positionID+"/logs", bytes.NewBufferString(`{"level":"info","message":"原有日志"}`))
	addReq.Header.Set("Authorization", "Bearer "+token)
	addResp := httptest.NewRecorder()
	routes.ServeHTTP(addResp, addReq)
	if addResp.Code != http.StatusOK {
		t.Fatalf("add log status = %d, body = %s", addResp.Code, addResp.Body.String())
	}

	stopReq := httptest.NewRequest(http.MethodPost, "/api/positions/"+positionID+"/stop", bytes.NewBufferString(`{}`))
	stopReq.Header.Set("Authorization", "Bearer "+token)
	stopResp := httptest.NewRecorder()
	routes.ServeHTTP(stopResp, stopReq)
	if stopResp.Code != http.StatusOK {
		t.Fatalf("stop status = %d, body = %s", stopResp.Code, stopResp.Body.String())
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/positions/"+positionID, nil)
	detailReq.Header.Set("Authorization", "Bearer "+token)
	detailResp := httptest.NewRecorder()
	routes.ServeHTTP(detailResp, detailReq)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detailResp.Code, detailResp.Body.String())
	}
	var detailPayload struct {
		Position struct {
			Status string `json:"status"`
		} `json:"position"`
	}
	if err := json.NewDecoder(detailResp.Body).Decode(&detailPayload); err != nil {
		t.Fatal(err)
	}
	if detailPayload.Position.Status != "stopped" {
		t.Fatalf("position status = %s", detailPayload.Position.Status)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/positions/"+positionID+"/logs", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list logs status = %d, body = %s", listResp.Code, listResp.Body.String())
	}
	var logPayload struct {
		Logs []struct {
			Message string `json:"message"`
		} `json:"logs"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&logPayload); err != nil {
		t.Fatal(err)
	}
	if len(logPayload.Logs) != 2 || logPayload.Logs[0].Message != "原有日志" || logPayload.Logs[1].Message != "岗位运行已停止" {
		t.Fatalf("unexpected logs: %+v", logPayload.Logs)
	}
}

func TestPositionLogListSupportsSince(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position-log-since@example.com")
	positionID := createPositionForTest(t, routes, token)

	addLog := func(message string) {
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/positions/"+positionID+"/logs",
			bytes.NewBufferString(`{"level":"info","message":"`+message+`"}`),
		)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("add log status = %d, body = %s", resp.Code, resp.Body.String())
		}
	}

	addLog("第一条")
	time.Sleep(10 * time.Millisecond)
	since := time.Now().UTC().Format(time.RFC3339Nano)
	time.Sleep(10 * time.Millisecond)
	addLog("第二条")

	req := httptest.NewRequest(http.MethodGet, "/api/positions/"+positionID+"/logs?since="+since, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("list logs status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Logs []struct {
			Message string `json:"message"`
		} `json:"logs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Logs) != 1 || payload.Logs[0].Message != "第二条" {
		t.Fatalf("unexpected logs: %+v", payload.Logs)
	}
}

// TestMemoryPositionLogStoreKeepsLatestThousand 验证内存日志最多保留每个岗位最新 1000 条。
func TestMemoryPositionLogStoreKeepsLatestThousand(t *testing.T) {
	store := NewMemoryPositionLogStore()
	for i := 0; i < maxPositionLogsPerPosition+5; i++ {
		if _, err := store.AddPositionLog(PositionLog{
			PositionID: "position_limit",
			UserEmail:  "limit@example.com",
			Level:      "info",
			Message:    "日志" + intString(i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(store.logs); got != maxPositionLogsPerPosition {
		t.Fatalf("log count = %d, want %d", got, maxPositionLogsPerPosition)
	}
	for _, item := range store.logs {
		if item.Message == "日志0" {
			t.Fatalf("oldest log was not trimmed")
		}
	}
}

// TestPositionLogClear 验证岗位日志可以被清空。
func TestPositionLogClear(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position-log-clear@example.com")
	positionID := createPositionForTest(t, routes, token)

	addReq := httptest.NewRequest(
		http.MethodPost,
		"/api/positions/"+positionID+"/logs",
		bytes.NewBufferString(`{"level":"info","message":"待清空日志"}`),
	)
	addReq.Header.Set("Authorization", "Bearer "+token)
	addResp := httptest.NewRecorder()
	routes.ServeHTTP(addResp, addReq)
	if addResp.Code != http.StatusOK {
		t.Fatalf("add log status = %d, body = %s", addResp.Code, addResp.Body.String())
	}

	clearReq := httptest.NewRequest(http.MethodDelete, "/api/positions/"+positionID+"/logs", nil)
	clearReq.Header.Set("Authorization", "Bearer "+token)
	clearResp := httptest.NewRecorder()
	routes.ServeHTTP(clearResp, clearReq)
	if clearResp.Code != http.StatusOK {
		t.Fatalf("clear logs status = %d, body = %s", clearResp.Code, clearResp.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/positions/"+positionID+"/logs", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list logs status = %d, body = %s", listResp.Code, listResp.Body.String())
	}

	var payload struct {
		Logs []any `json:"logs"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Logs) != 0 {
		t.Fatalf("unexpected logs after clear: %+v", payload.Logs)
	}
}

// TestPositionLogRejectsMissingPosition 验证不存在的岗位不能写入日志。
func TestPositionLogRejectsMissingPosition(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position-log-missing@example.com")

	req := httptest.NewRequest(http.MethodPost, "/api/positions/position_missing/logs", bytes.NewBufferString(`{"message":"x"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("add log status = %d, want %d", resp.Code, http.StatusNotFound)
	}
}
