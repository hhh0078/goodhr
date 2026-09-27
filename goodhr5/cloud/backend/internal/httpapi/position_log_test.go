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
	// 岗位日志按最新在前返回，停止记录应排在原有日志前面。
	if len(logPayload.Logs) != 2 || logPayload.Logs[0].Message != "岗位运行已停止" || logPayload.Logs[1].Message != "原有日志" {
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

// TestPositionDiagnosticReplaceAndShare 验证批量覆盖、千条上限、脱敏和分享链接替换。
func TestPositionDiagnosticReplaceAndShare(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "diagnostic-owner@example.com")
	positionID := createPositionForTest(t, routes, token)
	entries := make([]positionDiagnosticEvent, 1005)
	for index := range entries {
		entries[index] = positionDiagnosticEvent{
			Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano),
			Category: "browser", Message: "步骤" + intString(index), Input: json.RawMessage(`{}`), Output: json.RawMessage(`{}`),
		}
	}
	entries[1004].IsError = true
	entries[1004].ErrorMessage = "点击失败 password=private-secret"
	entries[1004].Input = json.RawMessage(`{"token":"private-token","target":"按钮"}`)
	content, err := json.Marshal(map[string]any{"logs": entries})
	if err != nil {
		t.Fatal(err)
	}
	replaceRequest := httptest.NewRequest(http.MethodPut, "/api/positions/"+positionID+"/logs", bytes.NewReader(content))
	replaceRequest.Header.Set("Authorization", "Bearer "+token)
	replaceResponse := httptest.NewRecorder()
	routes.ServeHTTP(replaceResponse, replaceRequest)
	if replaceResponse.Code != http.StatusOK {
		t.Fatalf("replace status=%d body=%s", replaceResponse.Code, replaceResponse.Body.String())
	}
	var replaceResult struct{ Count int `json:"count"` }
	if err := json.Unmarshal(replaceResponse.Body.Bytes(), &replaceResult); err != nil || replaceResult.Count != 1000 {
		t.Fatalf("replace count=%d err=%v", replaceResult.Count, err)
	}
	shareRequest := httptest.NewRequest(http.MethodPost, "/api/positions/"+positionID+"/logs/share", nil)
	shareRequest.Header.Set("Authorization", "Bearer "+token)
	shareResponse := httptest.NewRecorder()
	routes.ServeHTTP(shareResponse, shareRequest)
	if shareResponse.Code != http.StatusOK {
		t.Fatalf("share status=%d body=%s", shareResponse.Code, shareResponse.Body.String())
	}
	var shared struct{ Token string `json:"token"` }
	if err := json.Unmarshal(shareResponse.Body.Bytes(), &shared); err != nil || len(shared.Token) != 64 {
		t.Fatalf("share token invalid: %v", err)
	}
	publicResponse := httptest.NewRecorder()
	routes.ServeHTTP(publicResponse, httptest.NewRequest(http.MethodGet, "/api/public/position-log-shares/"+shared.Token, nil))
	if publicResponse.Code != http.StatusOK {
		t.Fatalf("public status=%d body=%s", publicResponse.Code, publicResponse.Body.String())
	}
	var publicResult struct {
		Logs []struct { Event positionDiagnosticEvent `json:"event"` } `json:"logs"`
	}
	if err := json.Unmarshal(publicResponse.Body.Bytes(), &publicResult); err != nil {
		t.Fatal(err)
	}
	if len(publicResult.Logs) != 1000 || !publicResult.Logs[0].Event.IsError || publicResult.Logs[len(publicResult.Logs)-1].Event.Message != "步骤5" {
		t.Fatalf("share logs count=%d first=%+v last=%+v", len(publicResult.Logs), publicResult.Logs[0], publicResult.Logs[len(publicResult.Logs)-1])
	}
	if bytes.Contains(publicResponse.Body.Bytes(), []byte("private-secret")) || bytes.Contains(publicResponse.Body.Bytes(), []byte("private-token")) {
		t.Fatal("shared logs contain credentials")
	}
	secondShare := httptest.NewRecorder()
	routes.ServeHTTP(secondShare, shareRequest)
	if secondShare.Code != http.StatusOK {
		t.Fatalf("second share status=%d", secondShare.Code)
	}
	oldResponse := httptest.NewRecorder()
	routes.ServeHTTP(oldResponse, httptest.NewRequest(http.MethodGet, "/api/public/position-log-shares/"+shared.Token, nil))
	if oldResponse.Code != http.StatusNotFound {
		t.Fatalf("old share status=%d, want 404", oldResponse.Code)
	}
}
