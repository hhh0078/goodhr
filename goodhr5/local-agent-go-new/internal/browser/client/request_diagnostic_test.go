// Package client 验证请求取消、响应中断和 Worker 超时的证据能关联到同一条操作，且不串入另一请求。
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// TestRequestEvidence 验证断在响应头前、正文中或 Worker 内时的原因、预算与字段进度。
func TestRequestEvidence(t *testing.T) {
	for _, phase := range []string{"wait_response_headers", "read_response_body", "worker_response", "prior_request"} {
		t.Run(phase, func(t *testing.T) {
			var probes, actions atomic.Int32
			var id atomic.Value
			entered := make(chan struct{})
			done := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					probes.Add(1)
					if r.Header.Get("X-Inspect-Request-ID") != id.Load().(string) {
						t.Error("健康探测没有关联原请求")
					}
					_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"status":"ok","request_progress":{"request_id":%q,"worker_phase":"locate_field","item_index":0,"item_count":5,"field_key":"name","worker_version":"0.1.0","value":"private-value","pending_reads":[{"worker_phase":"read_field","item_index":12,"field_key":"name","elapsed_ms":3},{"worker_phase":"read_field","item_index":13,"field_key":"private-key","elapsed_ms":2}]}}}`, id.Load())
					return
				}
				actions.Add(1)
				callID := r.Header.Get("X-Request-ID")
				if !requestIDPattern.MatchString(callID) {
					t.Errorf("请求编号无效 %q", callID)
				}
				id.Store(callID)
				if phase == "worker_response" || phase == "prior_request" {
					sourceID := callID
					if phase == "prior_request" {
						sourceID = "previous-worker-request"
					}
					_, _ = fmt.Fprintf(w, `{"ok":false,"error":{"code":"WORKER_UNRESPONSIVE","step":"queue","details":{"request_id":%q,"reason":"deadline_exceeded","request_progress":{"request_id":%q,"worker_phase":"queued","item_index":0,"field_key":"unsafe-field-value"}}}}`, sourceID, callID)
					return
				}
				if phase == "read_response_body" {
					w.WriteHeader(200)
					_, _ = w.Write([]byte(`{"ok":`))
					w.(http.Flusher).Flush()
				}
				close(entered)
				<-done
			}))
			defer server.Close()
			defer close(done)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if phase == "wait_response_headers" {
				go func() { <-entered; cancel() }()
			}
			if phase == "read_response_body" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(context.Background(), 200*time.Millisecond)
				defer stop()
			}
			_, err := New(server.URL).FindAll(ctx, contract.ElementFindAllRequest{})
			var workerErr *contract.WorkerError
			var d contract.RequestDiagnostic
			if phase == "read_response_body" {
				var transport *transportDiagnosticError
				if !errors.As(err, &transport) || errors.As(err, &workerErr) || !errors.Is(err, context.DeadlineExceeded) || !strings.HasPrefix(err.Error(), "解析 Worker 响应失败：") {
					t.Fatalf("丢失普通错误语义 %v", err)
				}
				d = transport.BrowserRequestDiagnostic()
			} else {
				if !errors.As(err, &workerErr) {
					t.Fatalf("丢失 Worker 结构化错误 %v", err)
				}
				var details struct {
					Diagnostic contract.RequestDiagnostic `json:"request_diagnostic"`
				}
				if err := json.Unmarshal(workerErr.Body.Details, &details); err != nil {
					t.Fatal(err)
				}
				d = details.Diagnostic
			}
			sanitized, _ := json.Marshal(d)
			wantPhase := phase
			if phase == "prior_request" {
				wantPhase = "worker_response"
			}
			if d.RequestID != id.Load() || d.Phase != wantPhase || !d.ConnectionAcquired || !d.RequestWritten ||
				d.GoTimeoutMS != 45_000 || d.WorkerTimeoutMS <= 0 || d.ParentRemainingMS == nil || d.WorkerReachable == nil || !*d.WorkerReachable ||
				d.WorkerPhase == "" || d.ItemIndex == nil || *d.ItemIndex != 0 || actions.Load() != 1 || strings.Contains(string(sanitized), "private-value") {
				t.Fatalf("证据不完整或发生重复操作 %+v", d)
			}
			reason := map[string]string{"wait_response_headers": "parent_cancelled", "read_response_body": "parent_deadline", "worker_response": "worker_deadline", "prior_request": "queue_blocked_by_prior_request"}[phase]
			if d.Reason != reason || d.ResponseReceived != (phase != "wait_response_headers") {
				t.Fatalf("错误归因 %+v", d)
			}
			if phase == "worker_response" || phase == "prior_request" {
				if d.FieldKey != "" || probes.Load() != 0 {
					t.Fatalf("不应传播未知字段或重复探测 %+v", d)
				}
			} else if len(d.PendingReads) != 2 || d.PendingReads[0].FieldKey != "name" || d.PendingReads[1].FieldKey != "" || d.FieldKey != "name" || probes.Load() != 1 || d.ItemCount == nil || *d.ItemCount != 5 {
				t.Fatalf("遗漏探测 %+v", d)
			}
		})
	}
}

// TestUnmatchedProgress 验证旧 Worker 或其他请求的现场不能归在本次读取下面。
func TestUnmatchedProgress(t *testing.T) {
	o := requestObservation{diagnostic: contract.RequestDiagnostic{RequestID: "current"}}
	o.mergeProgress(contract.RequestDiagnostic{RequestID: "other", WorkerPhase: "read_field", FieldKey: "name"})
	if o.diagnostic.WorkerPhase != "" || o.diagnostic.FieldKey != "" {
		t.Fatal("归入了其他请求的字段进度")
	}
}

// TestNotSentEvidence 验证已取消的查找不会发起 HTTP 或健康探测，同时保留原取消语义。
func TestNotSentEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("取消后不应连接 Worker") }))
	defer server.Close()
	_, err := New(server.URL).FindAll(ctx, contract.ElementFindAllRequest{})
	var workerErr *contract.WorkerError
	var transport *transportDiagnosticError
	if !errors.Is(err, context.Canceled) || errors.As(err, &workerErr) || !errors.As(err, &transport) || err.Error() != context.Canceled.Error() {
		t.Fatalf("原取消语义丢失 %v", err)
	}
	d := transport.BrowserRequestDiagnostic()
	if d.Phase != "before_request" || d.Reason != "parent_cancelled" || d.RequestWritten || d.WorkerReachable != nil || d.WorkerTimeoutMS != 0 {
		t.Fatalf("预取消记录有误 %+v", d)
	}
}

// TestRefusedConnectionErrorType 确认本地 Worker 无法连接时仍保留原错误文字与系统错误码。
func TestRefusedConnectionErrorType(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	addr := server.URL
	server.Close()
	_, err := New(addr).FindAll(context.Background(), contract.ElementFindAllRequest{})
	var workerErr *contract.WorkerError
	var transport *transportDiagnosticError
	refused := errors.Is(err, syscall.ECONNREFUSED) || (runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(10061)))
	if !refused || errors.As(err, &workerErr) || !errors.As(err, &transport) || !strings.HasPrefix(err.Error(), "连接 Browser Worker 失败：") {
		t.Fatalf("连接错误语义改变 %v", err)
	}
	d := transport.BrowserRequestDiagnostic()
	if d.WorkerReachable == nil || *d.WorkerReachable || d.Phase != "connect_worker" || d.Reason != "connection_refused" {
		t.Fatalf("连接拒绝未记录 %+v", d)
	}
}

// TestReadTimeoutRequiresMatchedNativeEvidence 验证定位与取值进度只能在请求编号一致时使用，原生时限不由错误文本猜测。
func TestReadTimeoutRequiresMatchedNativeEvidence(t *testing.T) {
	for _, tc := range []struct {
		phase  string
		limit  int64
		expect string
	}{{"locate_read_target", 0, "worker_error"}, {"read_value", 0, "worker_error"}, {"read_value", 1750, "read_value_timeout"}} {
		obs := newRequestObservation(context.Background(), "/api/v1/element/read", time.Now(), time.Second)
		raw, _ := json.Marshal(map[string]any{"request_progress": map[string]any{"request_id": obs.diagnostic.RequestID, "worker_phase": tc.phase}, "read_value_timeout_ms": tc.limit})
		body := contract.WorkerErrorBody{Code: "INTERNAL_ERROR", Action: "element.read", Step: "read", Details: raw}
		obs.recordError(nil, &body, "worker_response", "worker_error", "", time.Now(), 500)
		if obs.diagnostic.WorkerPhase != tc.phase || obs.diagnostic.Reason != tc.expect || obs.diagnostic.ReadValueTimeoutMS != tc.limit {
			t.Fatalf("未知原因或实际时限被误写：%+v", obs.diagnostic)
		}
	}
}
