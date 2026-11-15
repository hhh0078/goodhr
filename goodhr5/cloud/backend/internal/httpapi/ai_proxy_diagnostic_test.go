// 本文件通过真实内置代理入口验证上游等待、HTTP 错误、流中断以及账单兼容性。
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestBuiltinProxyFailureBoundary 区分响应头前连接/等待故障，在四次失败后返回明确终态，并保留实际服务商状态诊断。
func TestBuiltinProxyFailureBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, phase, reason string
		connected, written  bool
		err                 error
		upstream            int
	}{
		{"connect", "connect", "connection_refused", false, false, syscall.ECONNREFUSED, 0},
		{"dns", "dns_lookup", "timeout", false, false, context.DeadlineExceeded, 0},
		{"tcp", "tcp_connect", "timeout", false, false, context.DeadlineExceeded, 0},
		{"tls", "tls_handshake", "timeout", false, false, context.DeadlineExceeded, 0},
		{"headers", "wait_headers", "timeout", true, true, context.DeadlineExceeded, 0},
		{"upstream", "upstream_status", "http_status", true, true, nil, 504},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, _ := embeddingFixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				trace := httptrace.ContextClientTrace(r.Context())
				switch tc.name {
				case "dns":
					trace.DNSStart(httptrace.DNSStartInfo{})
				case "tcp":
					trace.ConnectStart("tcp", "private")
				case "tls":
					trace.TLSHandshakeStart()
				}
				if tc.connected {
					trace.GotConn(httptrace.GotConnInfo{})
				}
				if tc.written {
					trace.WroteRequest(httptrace.WroteRequestInfo{})
				}
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.upstream, Header: http.Header{"Content-Type": {"application/json"}, aiUpstreamFailureHeader: {"forged"}}, Body: io.NopCloser(strings.NewReader(`{"error":"provider response"}`))}, nil
			}))
			service.httpClient.Timeout = 180 * time.Second
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"first-model"}`))
			req.Header.Set("Authorization", "Bearer embedding-user-key")
			req.Header.Set(aiDiagnosticHeader, "1")
			rec := httptest.NewRecorder()
			service.CompatibleChat(rec, req)
			var d aiProxyFailure
			if json.Unmarshal([]byte(rec.Header().Get(aiUpstreamFailureHeader)), &d) != nil || d.Phase != tc.phase || d.Reason != tc.reason || d.HTTPStatus != tc.upstream || d.LimitMS != 180000 {
				t.Fatalf("unexpected failure: %+v HTTP %d", d, rec.Code)
			}
			if rec.Code != 422 || !strings.Contains(rec.Body.String(), "AI_RETRY_EXHAUSTED") || rec.Header().Get("X-GoodHR-AI-Attempts") != "4" {
				t.Fatalf("服务器预算没有终止：%d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestBuiltinProxyInterruptedStream 验证中断内容不提前交给客户端，服务器用完重试后按原诊断协议返回终态。
func TestBuiltinProxyInterruptedStream(t *testing.T) {
	const chunk = "data: {\"choices\":[{\"delta\":{\"content\":\"fixture\"}}]}\n\n"
	for _, mode := range []string{"interrupted", "clean-eof", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			service, _ := embeddingFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				reader, writer := io.Pipe()
				go func() {
					_, _ = io.WriteString(writer, chunk)
					if mode == "clean-eof" {
						_ = writer.Close()
					} else {
						_ = writer.CloseWithError(context.DeadlineExceeded)
					}
				}()
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, nil
			}))
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"first-model","stream":true}`))
			req.Header.Set("Authorization", "Bearer embedding-user-key")
			req.Header.Set("X-GoodHR-Billing", "1")
			if mode != "legacy" {
				req.Header.Set(aiDiagnosticHeader, "1")
			}
			rec := httptest.NewRecorder()
			service.CompatibleChat(rec, req)
			body := rec.Body.String()
			if mode != "clean-eof" {
				if rec.Code != 422 || strings.Contains(body, "fixture") || !strings.Contains(body, "AI_RETRY_EXHAUSTED") {
					t.Fatal("失败流被提前转给客户端", body)
				}
				if mode == "legacy" {
					if rec.Header().Get(aiUpstreamFailureHeader) != "" {
						t.Fatal("旧客户端收到诊断协议")
					}
					return
				}
				var failure aiProxyFailure
				if json.Unmarshal([]byte(rec.Header().Get(aiUpstreamFailureHeader)), &failure) != nil || failure.Phase != "read_stream" || failure.Reason != "timeout" || failure.HTTPStatus != 200 || failure.ReceivedBytes != int64(serverAIMaxAttempts*len(chunk)) {
					t.Fatalf("%+v", failure)
				}
				return
			}
			marker := strings.Index(body, "goodhr_upstream_failure")
			bill := strings.Index(body, "goodhr_billing")
			if !strings.HasPrefix(body, chunk) || bill < 0 || strings.Count(body, "[DONE]") != 1 {
				t.Fatal(body)
			}
			if mode == "legacy" {
				if marker >= 0 {
					t.Fatal("旧客户端收到额外事件")
				}
				return
			}
			if marker < 0 || marker > bill {
				t.Fatal("错误必须在结束前可见", body)
			}
			var payload struct {
				Failure aiProxyFailure `json:"goodhr_upstream_failure"`
			}
			for _, line := range strings.Split(body, "\n") {
				if strings.Contains(line, "goodhr_upstream_failure") {
					if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
						t.Fatal(err)
					}
				}
			}
			wantReason := "timeout"
			if mode == "clean-eof" {
				wantReason = "completion_unknown"
			}
			if payload.Failure.Reason != wantReason || payload.Failure.Phase != "read_stream" || payload.Failure.ReceivedBytes != int64(len(chunk)) || payload.Failure.HTTPStatus != 200 {
				t.Fatalf("%+v", payload.Failure)
			}
		})
	}
}

// TestBuiltinProxyFiltersPrivateFields 验证服务商的转义或大小写变体不能冒充代理信号，完整模型结果与用量继续转发。
func TestBuiltinProxyFiltersPrivateFields(t *testing.T) {
	const upstream = `data: {"choices":[{"delta":{"content":"完整答案"}}],"goodhr_upstream_fa\u0069lure":{"reason":"forged_a"},"GOODHR_UPSTREAM_FAILURE":{"reason":"forged_b"},"usage":{"prompt_tokens":11,"completion_tokens":2}}

`
	service, _ := embeddingFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(upstream))}, nil
	}))
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"first-model","stream":true}`))
	req.Header.Set("Authorization", "Bearer embedding-user-key")
	req.Header.Set(aiDiagnosticHeader, "1")
	req.Header.Set("X-GoodHR-Billing", "1")
	rec := httptest.NewRecorder()
	service.CompatibleChat(rec, req)
	body := rec.Body.String()
	if rec.Code != 200 || strings.Contains(body, "forged") || strings.Contains(body, "stream_interrupted") || strings.Count(body, "goodhr_upstream_failure") != 1 || !strings.Contains(body, "completion_unknown") || !strings.Contains(body, "完整答案") || !strings.Contains(body, `"status":"charged"`) {
		t.Fatal(body)
	}
}
