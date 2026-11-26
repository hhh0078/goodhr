// Package httpapi 文件作用：验证合法AI请求详情在岗位保存及公开分享保留4KiB安全原文，其它普通字段、凭据和整体大小限制保持。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"goodhr5/local-agent-go-new/cloudruntime"
)

// aiDetailAttemptForTest 创建真实合同的逐次上游失败，不包含请求或候选人正文。
func aiDetailAttemptForTest(detail string) cloudruntime.AIRequestAttempt {
	at := time.Now().UTC().Format(time.RFC3339Nano)
	return cloudruntime.AIRequestAttempt{Layer: "server", Phase: "end", ClientRequestID: "airq_safe_detail", AnalysisGroupID: "aigr_safe_detail", ClientAttempt: 1, ServerAttempt: 1, MaxAttempts: 4, Operation: "候选人基础预分析", Model: "fixture-model", Provider: "primary", StartedAt: at, FinishedAt: at, Result: "failed", Reason: "http_status", HTTPStatus: 503, ErrorDetail: detail, ErrorCode: "capacity"}
}

// TestAIRequestDetailPreservedThroughShare 验证长于普通1000字节的合法安全错误经过真实保存、查询和公开分享仍一致。
func TestAIRequestDetailPreservedThroughShare(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "safe-detail@example.com")
	position := createPositionForTest(t, routes, token)
	detail := "上游HTTP503实际错误：" + strings.Repeat("model capacity unavailable;", 100)
	attempt := aiDetailAttemptForTest(detail)
	output, _ := json.Marshal(map[string]any{"ai_request_attempt": attempt, "ordinary": strings.Repeat("x", 1500)})
	event := positionDiagnosticEvent{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Category: "错误", IsError: true, Message: "具体503原因", ErrorMessage: "具体503原因", Input: json.RawMessage(`{}`), Output: output}
	body, _ := json.Marshal(map[string]any{"logs": []positionDiagnosticEvent{event}})
	upload := httptest.NewRequest(http.MethodPut, "/api/positions/"+position+"/logs", bytes.NewReader(body))
	upload.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, upload)
	if w.Code != 200 {
		t.Fatalf("replace status=%d", w.Code)
	}
	share := httptest.NewRequest(http.MethodPost, "/api/positions/"+position+"/logs/share", nil)
	share.Header.Set("Authorization", "Bearer "+token)
	created := httptest.NewRecorder()
	routes.ServeHTTP(created, share)
	var saved struct {
		Token string `json:"token"`
	}
	if created.Code != 200 || json.Unmarshal(created.Body.Bytes(), &saved) != nil {
		t.Fatal("share failed")
	}
	public := httptest.NewRecorder()
	routes.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/api/public/position-log-shares/"+saved.Token, nil))
	var result struct {
		Logs []struct {
			Event positionDiagnosticEvent `json:"event"`
		} `json:"logs"`
	}
	if public.Code != 200 || json.Unmarshal(public.Body.Bytes(), &result) != nil || len(result.Logs) != 1 {
		t.Fatal("public share failed")
	}
	var actual struct {
		Attempt  cloudruntime.AIRequestAttempt `json:"ai_request_attempt"`
		Ordinary string                        `json:"ordinary"`
	}
	if json.Unmarshal(result.Logs[0].Event.Output, &actual) != nil || actual.Attempt.ErrorDetail != detail || actual.Attempt.ErrorDetailTruncated || actual.Attempt.ErrorCode != "capacity" {
		t.Fatal("合法4KiB以内详情被二次截断或字段丢失")
	}
	if len(actual.Ordinary) > 1003 {
		t.Fatal("普通字段上限被扩大")
	}
}

// TestAIRequestDetailUTF8CapAndCredentialRedaction 验证认证行、URL私密部分及联系方式仍隐藏，超4KiB按UTF8截断并明确标记。
func TestAIRequestDetailUTF8CapAndCredentialRedaction(t *testing.T) {
	detail := "服务不可用\nAuthorization: Basic PRIVATE-BASIC\nCookie: sid=PRIVATE-FIRST;session=PRIVATE-SECOND\nhttps://fixture:PRIVATE-PASS@198.51.100.20/path?api_key=PRIVATE-QUERY\n13800138000 fixture@example.com\n" + strings.Repeat("容量详情", 500)
	attempt := aiDetailAttemptForTest(detail)
	raw, _ := json.Marshal(map[string]any{"ai_request_attempt": attempt})
	safe, err := sanitizeDiagnosticObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	var actual struct {
		Attempt cloudruntime.AIRequestAttempt `json:"ai_request_attempt"`
	}
	if json.Unmarshal(safe, &actual) != nil || !actual.Attempt.ErrorDetailTruncated || len(actual.Attempt.ErrorDetail) > 4096 || !utf8.ValidString(actual.Attempt.ErrorDetail) {
		t.Fatal("4KiB截断或UTF8标记错误")
	}
	for _, private := range []string{"PRIVATE-BASIC", "PRIVATE-FIRST", "PRIVATE-SECOND", "PRIVATE-PASS", "PRIVATE-QUERY", "13800138000", "fixture@example.com"} {
		if strings.Contains(string(safe), private) {
			t.Fatal("分享错误详情泄露认证或联系方式")
		}
	}
}

// TestAIRequestDetailRequiresTypedContract 验证仅同名字段或伪造layer、phase、result不能绕过普通字符串限额，普通整体16KiB保持。
func TestAIRequestDetailRequiresTypedContract(t *testing.T) {
	for _, bad := range []map[string]any{{"error_detail": strings.Repeat("x", 2000)}, {"layer": "unknown", "phase": "end", "result": "failed", "error_detail": strings.Repeat("x", 2000)}} {
		raw, _ := json.Marshal(map[string]any{"ai_request_attempt": bad})
		safe, err := sanitizeDiagnosticObject(raw)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]map[string]any
		_ = json.Unmarshal(safe, &out)
		if len(out["ai_request_attempt"]["error_detail"].(string)) > 1003 {
			t.Fatal("任意同名对象绕过普通限制")
		}
	}
	raw, _ := json.Marshal(map[string]any{"ai_request_attempt": aiDetailAttemptForTest("known"), "ordinary": strings.Repeat("x", 17000)})
	if _, err := sanitizeDiagnosticObject(raw); err == nil {
		t.Fatal("普通整体16KiB限制被扩大")
	}
}
