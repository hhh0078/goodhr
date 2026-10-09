// 本文件负责验证 AI 完整失败回复经过云端接收、保存和公开分享后不被旧限额截断。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAIResponseDiagnosticStoredAndSharedWhole 验证超过旧 8 MiB HTTP 限额的 AI 原文及解析证据完整保存和分享。
func TestAIResponseDiagnosticStoredAndSharedWhole(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "ai-diagnostic-owner@example.com")
	positionID := createPositionForTest(t, routes, token)
	response := strings.Repeat("x", 9<<20) + "\n末尾证据 13800138000 candidate@example.com"
	output, _ := json.Marshal(map[string]any{"ordinary": strings.Repeat("x", 2000), "ai_response_diagnostic": map[string]any{"response": response, "model": "test-model", "parse_error": "score 字段不是数字", "parse_field": "score", "parse_offset": 123, "response_complete": true}})
	content, _ := json.Marshal(map[string]any{"logs": []positionDiagnosticEvent{{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Category: "错误", IsError: true, Message: "AI 返回格式不对", Input: json.RawMessage(`{}`), Output: output}}})
	req := httptest.NewRequest(http.MethodPut, "/api/positions/"+positionID+"/logs", bytes.NewReader(content))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("完整诊断被拒收：status=%d body=%s", resp.Code, resp.Body.String())
	}
	shareReq := httptest.NewRequest(http.MethodPost, "/api/positions/"+positionID+"/logs/share", nil)
	shareReq.Header.Set("Authorization", "Bearer "+token)
	shareResp := httptest.NewRecorder()
	routes.ServeHTTP(shareResp, shareReq)
	var share struct {
		Token string `json:"token"`
	}
	if shareResp.Code != http.StatusOK || json.Unmarshal(shareResp.Body.Bytes(), &share) != nil {
		t.Fatalf("创建分享失败：%s", shareResp.Body.String())
	}
	publicResp := httptest.NewRecorder()
	routes.ServeHTTP(publicResp, httptest.NewRequest(http.MethodGet, "/api/public/position-log-shares/"+share.Token, nil))
	var public struct {
		Logs []struct {
			Event positionDiagnosticEvent `json:"event"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(publicResp.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if len(public.Logs) != 1 {
		t.Fatalf("分享记录数量错误：%d", len(public.Logs))
	}
	var received struct {
		Ordinary string `json:"ordinary"`
		AI       struct {
			Response    string `json:"response"`
			Model       string `json:"model"`
			ParseError  string `json:"parse_error"`
			ParseOffset int    `json:"parse_offset"`
		} `json:"ai_response_diagnostic"`
	}
	if err := json.Unmarshal(public.Logs[0].Event.Output, &received); err != nil {
		t.Fatal(err)
	}
	if received.AI.Response != response || received.AI.Model != "test-model" || received.AI.ParseOffset != 123 || received.AI.ParseError != "score 字段不是数字" {
		t.Fatal("云端接收或分享阶段截断、隐藏了完整 AI 诊断")
	}
	if len(received.Ordinary) > 1010 {
		t.Fatal("普通出参字符串没有维持限额")
	}
}

// TestAIResponseDiagnosticKeepsCredentialRedaction 验证完整 AI 原文不截断，但凭据仍被隐藏。
func TestAIResponseDiagnosticKeepsCredentialRedaction(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"ai_response_diagnostic": map[string]any{"response": strings.Repeat("原文", 2000) + ` {"token":"private-token"} password=private-password Bearer private-bearer`, "api_key": "private-key"}})
	safe, err := sanitizeDiagnosticObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-token", "private-password", "private-bearer", "private-key"} {
		if bytes.Contains(safe, []byte(secret)) {
			t.Fatalf("凭据未隐藏：%s", secret)
		}
	}
	if len(safe) < 10000 {
		t.Fatal("完整回复被普通字符串限额截断")
	}
	ordinary, _ := json.Marshal(map[string]any{"ordinary": strings.Repeat("x", 17000)})
	if _, err := sanitizeDiagnosticObject(ordinary); err == nil {
		t.Fatal("普通日志不应绕过对象大小限制")
	}
}
