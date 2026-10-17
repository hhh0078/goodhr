// 文件作用：验证浏览器冷启动准备、运行中复用、失败阻断和并发启动停止的取消边界。
package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// startupWorker 提供仅在本机响应的浏览器状态和启动接口，记录实际请求而不打开浏览器。
func startupWorker(t *testing.T) (*Client, *atomic.Bool, *atomic.Int32, *atomic.Int32, chan contract.BrowserStartRequest) {
	t.Helper()
	running, starts, stops := &atomic.Bool{}, &atomic.Int32{}, &atomic.Int32{}
	requests := make(chan contract.BrowserStartRequest, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/browser/status":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool                   `json:"ok"`
				Data contract.BrowserStatus `json:"data"`
			}{true, contract.BrowserStatus{Running: running.Load()}})
		case "/api/v1/browser/start":
			var request contract.BrowserStartRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			requests <- request
			starts.Add(1)
			reused := running.Swap(true)
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool                   `json:"ok"`
				Data contract.BrowserStatus `json:"data"`
			}{true, contract.BrowserStatus{Running: true, Reused: reused}})
		case "/api/v1/browser/stop":
			stops.Add(1)
			running.Store(false)
			_, _ = w.Write([]byte(`{"ok":true,"data":{"running":false}}`))
		default:
			t.Errorf("意外请求：%s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return New(server.URL), running, starts, stops, requests
}

// TestBeforeStartOnlyPreparesColdBrowser 验证首次安装路径随冷启动请求发送，运行中不再次准备或改写代码。
func TestBeforeStartOnlyPreparesColdBrowser(t *testing.T) {
	c, _, starts, _, requests := startupWorker(t)
	var preparations atomic.Int32
	c.SetBeforeStart(func(ctx context.Context, request contract.BrowserStartRequest) (contract.BrowserStartRequest, error) {
		preparations.Add(1)
		request.ExtensionPaths = append([]string{"managed"}, request.ExtensionPaths...)
		return request, nil
	})
	if _, err := c.StartBrowser(context.Background(), contract.BrowserStartRequest{ExtensionPaths: []string{"bookmark"}}); err != nil {
		t.Fatal(err)
	}
	if request := <-requests; !reflect.DeepEqual(request.ExtensionPaths, []string{"managed", "bookmark"}) {
		t.Fatal(request.ExtensionPaths)
	}
	result, err := c.StartBrowser(context.Background(), contract.BrowserStartRequest{ExtensionPaths: []string{"managed", "bookmark"}})
	if err != nil || !result.Reused || preparations.Load() != 1 || starts.Load() != 2 {
		t.Fatalf("运行中被重复准备：%+v %v", result, err)
	}
	if _, err = c.StopBrowser(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = c.StartBrowser(context.Background(), contract.BrowserStartRequest{}); err != nil {
		t.Fatal(err)
	}
	if preparations.Load() != 2 {
		t.Fatal("下次真实冷启动没有重查插件")
	}
}

// TestBeforeStartFailureDoesNotLaunch 验证准备失败阻止启动，未准备完整插件时不会悄悄打开错误版本。
func TestBeforeStartFailureDoesNotLaunch(t *testing.T) {
	c, _, starts, _, _ := startupWorker(t)
	expected := errors.New("synthetic invalid package")
	c.SetBeforeStart(func(ctx context.Context, request contract.BrowserStartRequest) (contract.BrowserStartRequest, error) {
		return request, expected
	})
	if _, err := c.StartBrowser(context.Background(), contract.BrowserStartRequest{}); !errors.Is(err, expected) {
		t.Fatal(err)
	}
	if starts.Load() != 0 {
		t.Fatal("准备失败后仍发出启动请求")
	}
}

// TestConcurrentStartsPrepareOnceAndStopWaitCancels 验证检查、准备、启动属于同一锁，等待启动或停止可以快速取消。
func TestConcurrentStartsPrepareOnceAndStopWaitCancels(t *testing.T) {
	c, _, starts, stops, _ := startupWorker(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var preparations atomic.Int32
	c.SetBeforeStart(func(ctx context.Context, request contract.BrowserStartRequest) (contract.BrowserStartRequest, error) {
		if preparations.Add(1) == 1 {
			close(entered)
			<-release
		}
		return request, nil
	})
	first := make(chan error, 1)
	go func() { _, err := c.StartBrowser(context.Background(), contract.BrowserStartRequest{}); first <- err }()
	<-entered
	for _, stop := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		var err error
		if stop {
			_, err = c.StopBrowser(ctx)
		} else {
			_, err = c.StartBrowser(ctx, contract.BrowserStartRequest{})
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("等待锁未及时取消：%v", err)
		}
	}
	if starts.Load() != 0 || stops.Load() != 0 {
		t.Fatal("准备期间并发启动/停止穿过互斥锁")
	}
	var group sync.WaitGroup
	second := make(chan error, 1)
	group.Add(1)
	go func() {
		defer group.Done()
		_, err := c.StartBrowser(context.Background(), contract.BrowserStartRequest{})
		second <- err
	}()
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	group.Wait()
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if preparations.Load() != 1 || starts.Load() != 2 {
		t.Fatalf("并发冷启动重复准备：preparations=%d starts=%d", preparations.Load(), starts.Load())
	}
}
