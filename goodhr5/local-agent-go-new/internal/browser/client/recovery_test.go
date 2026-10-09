// 文件作用说明：验证 Worker 故障和请求取消只返回错误、保留进程且绝不重放，输入与长截图仍有足够预算。
package client

import (
	"context"
	"errors"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestFailureRetainsWorkerWithoutReplay 验证操作失联后仍可访问同一 Worker，失败请求不自动重试。
func TestFailureRetainsWorkerWithoutReplay(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"ok":true,"data":{"status":"ok"}}`))
			return
		}
		calls++
		if r.Header.Get("X-Worker-Timeout-Ms") == "" {
			t.Error("未传递请求预算")
		}
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"WORKER_UNRESPONSIVE","message":"卡住","action":"element.scroll","step":"queue"}}`))
	}))
	defer server.Close()
	client := New(server.URL)
	_, err := client.Scroll(context.Background(), contract.ScrollRequest{})
	var workerErr *contract.WorkerError
	if !errors.As(err, &workerErr) || workerErr.Body.Code != "WORKER_UNRESPONSIVE" {
		t.Fatalf("错误未保留：%v", err)
	}
	if calls != 1 || client.Health(context.Background()) != nil {
		t.Fatalf("失败后请求被重放或 Worker 不可用：calls=%d", calls)
	}
}

// TestWindowNotReadyRetainsWorker 验证任何窗口错误只保留原错误，不调用停止接口或重放当前请求。
func TestWindowNotReadyRetainsWorker(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		details string
	}{
		{name: "zero_input", details: `{"native_mouse":{"events_sent":0,"events_acknowledged":0}}`},
		{name: "input_sent", details: `{"native_mouse":{"events_sent":1,"events_acknowledged":0}}`},
		{name: "missing_evidence", details: `{}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					_, _ = w.Write([]byte(`{"ok":true,"data":{"status":"ok"}}`))
					return
				}
				calls++
				_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"BROWSER_WINDOW_NOT_READY","message":"窗口未激活","action":"element.scroll","step":"move","details":` + testCase.details + `}}`))
			}))
			defer server.Close()
			browser := New(server.URL)
			_, err := browser.Scroll(context.Background(), contract.ScrollRequest{})
			var workerErr *contract.WorkerError
			if !errors.As(err, &workerErr) || workerErr.Body.Code != "BROWSER_WINDOW_NOT_READY" {
				t.Fatalf("错误未保留：%v", err)
			}
			if calls != 1 || browser.Health(context.Background()) != nil {
				t.Fatalf("窗口错误后请求被重放或 Worker 不可用：calls=%d", calls)
			}
		})
	}
}

// TestTypingBudget 验证中文按字符计算输入预算，避免长消息被固定 45 秒中断。
func TestTypingBudget(t *testing.T) {
	if got := requestTimeout(contract.ElementInputRequest{Text: strings.Repeat("中", 400)}); got != 90*time.Second {
		t.Fatalf("timeout=%s", got)
	}
	if got := requestTimeout(contract.ElementInputRequest{Text: strings.Repeat("中", 5000)}); got != 180*time.Second {
		t.Fatalf("timeout=%s", got)
	}
}

// TestLongScreenshotBudget 验证分段长截图不会被普通请求的 45 秒预算提前中断。
func TestLongScreenshotBudget(t *testing.T) {
	if got := requestTimeout(contract.LongScreenshotRequest{MaxParts: 20}); got != 180*time.Second {
		t.Fatalf("timeout=%s", got)
	}
}

// TestAlreadyCancelledDoesNotSendRequest 验证未发送的取消请求直接返回取消原因。
func TestAlreadyCancelledDoesNotSendRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := New("http://127.0.0.1:1")
	_, err := client.Scroll(ctx, contract.ScrollRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消请求没有保留原错误：%v", err)
	}
}

// TestInFlightCancellationRetainsWorker 验证已发出请求的超时和取消不会停止 Worker 或补发浏览器动作。
func TestInFlightCancellationRetainsWorker(t *testing.T) {
	for _, timedOut := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "timed_out"}[timedOut], func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					_, _ = w.Write([]byte(`{"ok":true,"data":{"status":"ok"}}`))
					return
				}
				calls.Add(1)
				close(started)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			browser := New(server.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := browser.Scroll(ctx, contract.ScrollRequest{}); result <- err }()
			<-started
			if !timedOut {
				cancel()
			}
			var workerErr *contract.WorkerError
			if err := <-result; !errors.As(err, &workerErr) || workerErr.Body.Code != "WORKER_UNRESPONSIVE" {
				t.Fatalf("进行中请求没有保留中断错误：%v", err)
			}
			if err := browser.Health(context.Background()); err != nil || calls.Load() != 1 {
				t.Fatalf("Worker 被停止或动作被重放：calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}
