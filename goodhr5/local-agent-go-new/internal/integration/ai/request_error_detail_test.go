// Package ai 文件作用：验证逐次失败的明确服务说明、旧协商头兼容、UTF8限额和凭据脱敏，不改变重试与完整响应诊断。
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// TestAIRequestExternalErrorDetail 验证真实外部HTTP失败只提取错误说明，脱敏已知密钥而完整响应仍保存在原诊断。
func TestAIRequestExternalErrorDetail(t *testing.T) {
	const key = "PRIVATE_API_KEY"
	const body = `{"error":{"message":"模型容量暂不足，请稍后再试 PRIVATE_API_KEY","code":"ServiceUnavailable"},"choices":[{"message":{"content":"PRIVATE_RESUME"}}],"api_key":"PRIVATE_API_KEY"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	collector := &requestLogCollector{}
	ctx := WithRequestLogReporter(context.Background(), "fixed-task", "fixed-position", collector.report)
	_, err := New().ParseResume(ctx, cloud.AIConfig{BaseURL: server.URL, APIKey: key, Model: "test-model"}, "PRIVATE_REQUEST")
	if err == nil {
		t.Fatal("真实HTTP失败被吞掉")
	}
	lines := collector.snapshot()
	if len(lines) != 2 {
		t.Fatalf("单次请求日志=%d", len(lines))
	}
	event := lines[1].Output.Attempt
	if event.ErrorDetail != "模型容量暂不足，请稍后再试 [已隐藏]" || event.ErrorCode != "ServiceUnavailable" || event.ResponseBytes != int64(len(body)) || event.WillRetry || strings.Count(lines[1].ErrorMessage, "模型容量暂不足") != 1 {
		t.Fatalf("错误说明或单次策略丢失：%+v", event)
	}
	encoded, _ := json.Marshal(lines)
	for _, secret := range []string{key, "PRIVATE_RESUME", "PRIVATE_REQUEST"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("请求元数据包含私密字段：%s", secret)
		}
	}
	if diagnostic, ok := ResponseDiagnosticFromError(err); !ok || strings.Contains(diagnostic.Response, key) || !strings.Contains(diagnostic.Response, "PRIVATE_RESUME") || !strings.Contains(diagnostic.Response, "模型容量暂不足") || !diagnostic.ResponseComplete {
		t.Fatal("完整错误诊断没有隐藏认证回显，或丢失了其它原始响应内容")
	}
	if summary := SafeRequestErrorSummary(err); !strings.Contains(summary, event.ErrorDetail) || strings.Contains(summary, key) {
		t.Fatal("最终安全摘要没有复用本次已脱敏说明")
	}
}

// TestAIRequestErrorHeaderCompatibility 验证新字段随可信历史保留，开始清空错误字段，旧头仍可读且非法负字节不接受。
func TestAIRequestErrorHeaderCompatibility(t *testing.T) {
	now := time.Now().UTC()
	for _, mode := range []string{"detail", "legacy", "long_utf8", "success", "negative_bytes"} {
		t.Run(mode, func(t *testing.T) {
			collector := &requestLogCollector{}
			record := &requestLogAttempt{ctx: WithRequestLogReporter(context.Background(), "task", "position", collector.report), credential: "known-key", event: RequestAttempt{ClientRequestID: "req-fixed", AnalysisGroupID: "group-fixed", ClientAttempt: 1, Operation: "AI 请求"}}
			event := RequestAttempt{Layer: "server", ClientRequestID: "req-fixed", AnalysisGroupID: "group-fixed", ServerAttempt: 1, MaxAttempts: 4, Provider: "primary", StartedAt: now.Add(-time.Millisecond).Format(time.RFC3339Nano), FinishedAt: now.Format(time.RFC3339Nano), ElapsedMS: 1, HTTPStatus: 503, Result: "failed", Reason: "http_status", WillRetry: true, BackoffMS: 500, ErrorDetail: "服务繁忙 known-key api_key=unknown-key", ErrorCode: "ServiceUnavailable", ResponseBytes: 123}
			switch mode {
			case "legacy":
				event.ErrorDetail, event.ErrorCode, event.ResponseBytes = "", "", 0
			case "long_utf8":
				event.ErrorDetail = strings.Repeat("暂", 400)
			case "success":
				event.Result, event.Reason, event.HTTPStatus, event.WillRetry = "success", "success", 200, false
			case "negative_bytes":
				event.ResponseBytes = -1
			}
			encoded, _ := json.Marshal([]RequestAttempt{event})
			record.serverAttempts(string(encoded), true)
			lines := collector.snapshot()
			if mode == "negative_bytes" {
				if len(lines) != 0 {
					t.Fatal("负数已收字节混入岗位日志")
				}
				return
			}
			if len(lines) != 2 {
				t.Fatal("兼容头的开始结束丢失")
			}
			start, end := lines[0].Output.Attempt, lines[1].Output.Attempt
			if start.ErrorDetail != "" || start.ErrorCode != "" || start.ErrorDetailTruncated || start.ResponseBytes != 0 {
				t.Fatal("合成开始错误携带了后续失败内容")
			}
			if mode == "detail" && (end.ErrorDetail != "服务繁忙 [已隐藏] api_key=[已隐藏]" || end.ErrorCode != "ServiceUnavailable" || end.ResponseBytes != 123 || strings.Count(lines[1].ErrorMessage, "服务繁忙") != 1) {
				t.Fatalf("安全详情字段丢失：%+v", end)
			}
			if mode == "long_utf8" && (!end.ErrorDetailTruncated || len(end.ErrorDetail) > 1024 || !utf8.ValidString(end.ErrorDetail)) {
				t.Fatal("详情限额破坏中文或未标记截断")
			}
			if mode == "success" && (end.ErrorDetail != "" || end.ErrorCode != "" || end.ErrorDetailTruncated || end.ResponseBytes != 123) {
				t.Fatal("成功请求携带错误正文或丢失已收字节")
			}
		})
	}
}

// TestAIRequestErrorDetailPrivacy 验证错误摘要不透传JSON附加正文，未识别响应只保留原分类，数字错误码仍兼容。
func TestAIRequestErrorDetailPrivacy(t *testing.T) {
	for _, test := range []struct{ body, detail, code string }{
		{`{"error":{"message":"容量不足","code":503},"prompt":"PRIVATE_INPUT"}`, "容量不足", "503"},
		{`{"error":"服务繁忙","message":"ignored"}`, "服务繁忙", ""},
		{`{"choices":[{"message":{"content":"PRIVATE_OUTPUT"}}]}`, "", ""},
		{`{"error":{"message":"PRIVATE_TRUNCATED`, "", ""},
		{"<html>PRIVATE_HTML</html>", "", ""},
	} {
		detail, code := providerErrorDetail(test.body)
		if detail != test.detail || code != test.code {
			t.Fatalf("错误摘要边界不符：got=%q/%q", detail, code)
		}
	}
	event := NormalizeRequestAttempt(RequestAttempt{Phase: "end", Result: "failed", ErrorDetail: `服务繁忙 "api_key":"foreign-key" Bearer hidden-token https://provider.invalid/api?key=hidden`, ErrorCode: "bad code"})
	if strings.Contains(event.ErrorDetail, "foreign-key") || strings.Contains(event.ErrorDetail, "hidden-token") || strings.Contains(event.ErrorDetail, "key=hidden") || event.ErrorCode != "" {
		t.Fatalf("外部错误中的凭据未隐藏：%q", event.ErrorDetail)
	}
	// 工具请求没有ResponseError，仍须在旧500字摘要截断前取出明确错误字段。
	want := strings.Repeat("模型暂忙", 500)
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"message": want, "code": "ServiceUnavailable"}})
	detail, code := requestErrorDetail(newHTTPServiceError(503, string(body), ""))
	bounded := NormalizeRequestAttempt(RequestAttempt{Phase: "end", Result: "failed", ErrorDetail: detail, ErrorCode: code})
	if detail != want || bounded.ErrorCode != "ServiceUnavailable" || !bounded.ErrorDetailTruncated || len(bounded.ErrorDetail) > 4096 {
		t.Fatal("工具错误被旧摘要截断后无法解析，或长详情没有标记截断")
	}
}

// TestAIRequestErrorDetailBudgets 验证普通持久详情在4KiB内完整保留，超限只按UTF8边界裁剪并显式标记。
func TestAIRequestErrorDetailBudgets(t *testing.T) {
	for _, size := range []int{1100, 1365, 1366, 3000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			text := strings.Repeat("暂", size)
			event := NormalizeRequestAttempt(RequestAttempt{Phase: "end", Result: "failed", ErrorDetail: text})
			if summary := RequestAttemptReason(event); len(summary) > 512 || !strings.Contains(summary, "详见错误详情") || !utf8.ValidString(summary) {
				t.Fatal("长错误用户摘要未裁到512 UTF8字节或没有详情入口")
			}
			if len(text) <= 4096 {
				if event.ErrorDetail != text || event.ErrorDetailTruncated {
					t.Fatal("4KiB内具体错误被静默截断")
				}
			} else if !event.ErrorDetailTruncated || len(event.ErrorDetail) > 4096 || !utf8.ValidString(event.ErrorDetail) {
				t.Fatal("超限错误详情未按UTF8边界截断并标记")
			}
		})
	}
}

// TestAIRequestPrivateHTTPHeaderEcho 验证真实自定义HTTP错误回显中的整行认证、Cookie和URL凭据不进入逐次日志或安全终态摘要。
func TestAIRequestPrivateHTTPHeaderEcho(t *testing.T) {
	const key = "PRIVATE_REQUEST_API_KEY"
	detail := "上游503容量不足\nAuthorization: Basic BASIC_PRIVATE_VALUE\nProxy-Authorization: Basic PROXY_PRIVATE_VALUE\nCookie: sid=SID_PRIVATE_VALUE; refresh=REFRESH_PRIVATE_VALUE\nSet-Cookie: login=LOGIN_PRIVATE_VALUE; Path=/\nX-Api-Key: OTHER_PRIVATE_KEY\n\"Authorization\": \"Basic JSON_PRIVATE_VALUE\"\nhttps://user:URL_PRIVATE_PASSWORD@provider.invalid/api?token=QUERY_PRIVATE_VALUE\n配置密钥 " + key + "\n具体原因仍保留"
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"message": detail, "code": "ServiceUnavailable"}})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write(body)
	}))
	defer provider.Close()
	collector := &requestLogCollector{}
	ctx := WithRequestLogReporter(context.Background(), "task", "position", collector.report)
	_, err := New().ParseResume(ctx, cloud.AIConfig{BaseURL: provider.URL, APIKey: key, Model: "test-model"}, "PRIVATE_REQUEST")
	lines := collector.snapshot()
	if err == nil || len(lines) != 2 {
		t.Fatal("真实失败或开始结束记录丢失")
	}
	encoded, _ := json.Marshal(lines)
	diagnostic, ok := ResponseDiagnosticFromError(err)
	if !ok || !json.Valid([]byte(diagnostic.Response)) {
		t.Fatal("完整错误诊断丢失或认证清理破坏JSON")
	}
	safe := string(encoded) + SafeRequestErrorSummary(err) + err.Error() + diagnostic.Response
	for _, secret := range []string{key, "BASIC_PRIVATE_VALUE", "PROXY_PRIVATE_VALUE", "SID_PRIVATE_VALUE", "REFRESH_PRIVATE_VALUE", "LOGIN_PRIVATE_VALUE", "OTHER_PRIVATE_KEY", "JSON_PRIVATE_VALUE", "URL_PRIVATE_PASSWORD", "QUERY_PRIVATE_VALUE"} {
		if strings.Contains(safe, secret) {
			t.Fatalf("错误回显凭据进入日志：%s", secret)
		}
	}
	if !strings.Contains(lines[1].ErrorMessage, "上游503容量不足") || !strings.Contains(lines[1].ErrorMessage, "具体原因仍保留") || lines[1].Output.Attempt.ErrorDetailTruncated {
		t.Fatal("脱敏误删了非认证行的明确错误说明")
	}
	if !strings.Contains(diagnostic.Response, "上游503容量不足") || !strings.Contains(diagnostic.Response, "具体原因仍保留") || lines[1].Output.Attempt.ResponseBytes != int64(len(body)) {
		t.Fatal("认证清理丢失其它原始错误内容，或改变实际读取字节证据")
	}
}

// TestAIRequestHTTPErrorBodyPreservesOtherBytes 验证JSON认证值局部替换不重排其它字段，完整普通body与原状态分类保持兼容。
func TestAIRequestHTTPErrorBodyPreservesOtherBytes(t *testing.T) {
	body := "{\n  \"error\": {\"message\":\"服务不可用\"},\n  \"count\": 12345678901234567890, \"Authorization\": \"Basic PRIVATE_BASIC\", \"data\": [null, {\"Cookie\":\"sid=PRIVATE_SID;refresh=PRIVATE_REFRESH\"}], \"note\":\"普通文字Authorization不会被删除\"\n}"
	want := strings.ReplaceAll(strings.ReplaceAll(body, "Basic PRIVATE_BASIC", "[认证信息已隐藏]"), "sid=PRIVATE_SID;refresh=PRIVATE_REFRESH", "[认证信息已隐藏]")
	if safe := redactRequestHTTPErrorBody(body, "unused-request-key"); safe != want {
		t.Fatal("清理认证值改变了其它响应字节或字段顺序")
	}
	const normal = "{\n\t\"error\": {\"message\": \"服务暂忙\"}, \"other\": [1, null, \"原文保留\"]\n}"
	if redactRequestHTTPErrorBody(normal, "key") != normal {
		t.Fatal("没有认证回显的HTTP错误被改写")
	}
	const complex = `{"Cookie":["sid=PRIVATE","refresh=PRIVATE2"],"Authorization":{"scheme":"Basic","value":"PRIVATE3"},"token":123456,"secret":null,"error":{"message":"普通错误完整保留"}}`
	const complexWant = `{"Cookie":"[认证信息已隐藏]","Authorization":"[认证信息已隐藏]","token":"[认证信息已隐藏]","secret":"[认证信息已隐藏]","error":{"message":"普通错误完整保留"}}`
	if safe := redactRequestHTTPErrorBody(complex, "key"); safe != complexWant {
		t.Fatal("非字符串认证字段没有整块隐藏，或破坏了其它响应字节")
	}
	if !newHTTPServiceError(422, `{"error":{"message":"model unavailable API_PRIVATE"}}`, "", "model").Fatal {
		t.Fatal("凭据清理改变了原响应决定的模型配置停止策略")
	}
}

// TestAIRequestExhaustedPolicyCompatibility 验证具体上游model/quota/balance说明不会把代理原422改成岗位致命或额外重试。
func TestAIRequestExhaustedPolicyCompatibility(t *testing.T) {
	for _, status := range []int{401, 402, 404, 422, 424, 503} {
		for _, word := range []string{"model", "quota", "balance", "模型额度"} {
			for _, shape := range []string{"flat", "nested"} {
				body := fmt.Sprintf(`{"error":{"code":"AI_RETRY_EXHAUSTED","message":%q}}`, "服务繁忙 "+word)
				if shape == "flat" {
					// 实际服务器返回flat error/code和attempts，历史模型字段不能让本机额外判为配置致命错误。
					body = fmt.Sprintf(`{"error":%q,"code":"AI_RETRY_EXHAUSTED","attempt_count":4,"attempts":[{"model":"upstream-model","error_detail":"容量暂不足"}]}`, "服务繁忙 "+word)
				}
				service := newHTTPServiceError(status, body, "")
				if status == 422 || status == 424 {
					if service.Retryable || service.Fatal || IsPositionStoppingError(service) {
						t.Fatalf("代理已重试耗尽的%s协议被扩大为重试或停止岗位", shape)
					}
				} else if !service.Fatal || (status == 503) != service.Retryable {
					t.Fatalf("原HTTP%d策略改变", status)
				}
			}
		}
	}
	if !newHTTPServiceError(422, `{"error":{"code":"INVALID_MODEL","message":"model not found"}}`, "").Fatal {
		t.Fatal("普通模型配置错误的原停止保护被放宽")
	}
	if SafeRequestErrorSummary(&ServiceError{Message: "PRIVATE_UNTRUSTED"}) != "" {
		t.Fatal("未经真实请求清理的外部错误可伪装成安全摘要")
	}
}
