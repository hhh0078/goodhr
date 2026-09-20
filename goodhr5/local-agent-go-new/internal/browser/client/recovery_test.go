// 文件作用说明：验证 Worker 卡死错误会先回收进程且绝不重放请求，逐字输入和长截图得到足够预算。
package client

import (
	"context"
	"errors"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRecoveryWithoutReplay 验证进程回收只执行一次，发送类请求不自动重试。
func TestRecoveryWithoutReplay(t *testing.T) {
	calls, recoveries := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Worker-Timeout-Ms") == "" {
			t.Error("未传递请求预算")
		}
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"WORKER_UNRESPONSIVE","message":"卡住","action":"element.scroll","step":"queue"}}`))
	}))
	defer server.Close()
	client := New(server.URL)
	client.SetRecovery(func() error { recoveries++; return nil })
	_, err := client.Scroll(context.Background(), contract.ScrollRequest{})
	var workerErr *contract.WorkerError
	if !errors.As(err, &workerErr) || workerErr.Body.Code != "WORKER_UNRESPONSIVE" {
		t.Fatalf("错误未保留：%v", err)
	}
	if calls != 1 || recoveries != 1 {
		t.Fatalf("calls=%d recoveries=%d", calls, recoveries)
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

// TestAlreadyCancelledDoesNotKillWorker 验证未发送的取消请求不会误杀健康 Worker。
func TestAlreadyCancelledDoesNotKillWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := New("http://127.0.0.1:1")
	recovered := false
	client.SetRecovery(func() error { recovered = true; return nil })
	_, err := client.Scroll(ctx, contract.ScrollRequest{})
	if !errors.Is(err, context.Canceled) || recovered {
		t.Fatalf("取消请求错误恢复进程：%v %v", err, recovered)
	}
}
