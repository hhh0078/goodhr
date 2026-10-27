// Package api 文件作用：用本机 HTTP 和可控依赖验证绑定后浏览器同步、失败重试、任务保护、退出取消及凭证不落盘。
package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/flow/lifecycle"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/runtime"
	"goodhr5/local-agent-go-new/internal/storage"
)

// startupRuntimeFixture 模拟组件合同的门闩，不启动浏览器或下载组件。
type startupRuntimeFixture struct {
	mu       sync.Mutex
	pending  bool
	calls    int
	begins   int
	failures []string
	stop     func() error
}

// StartupCamoufoxSyncPending 返回受锁保护的首次检查状态。
func (f *startupRuntimeFixture) StartupCamoufoxSyncPending() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pending
}

// BeginStartupCamoufoxSyncCheck 记录开始检查的次数，以验证重复绑定不重入。
func (f *startupRuntimeFixture) BeginStartupCamoufoxSyncCheck() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begins++
}

// FailStartupCamoufoxSync 记录安全错误并保留待重试门闩。
func (f *startupRuntimeFixture) FailStartupCamoufoxSync(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, err.Error())
}

// SyncStartupCamoufox 模拟同步成功前的安全回调与取消检查。
func (f *startupRuntimeFixture) SyncStartupCamoufox(ctx context.Context, _ runtime.Manifest, before func(context.Context) error) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if err := before(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	f.pending = false
	f.mu.Unlock()
	return nil
}

// StopWorker 记录已有 Worker 的正常停止，不创建任何系统进程。
func (f *startupRuntimeFixture) StopWorker() error {
	if f.stop != nil {
		return f.stop()
	}
	return nil
}

// startupCloudFixture 用可控读取回调模拟云端延迟、失败与取消。
type startupCloudFixture struct {
	calls atomic.Int32
	read  func(context.Context, string) (runtime.Manifest, error)
}

// RuntimeManifest 统计请求并执行本次测试的清单响应。
func (f *startupCloudFixture) RuntimeManifest(ctx context.Context, token string) (runtime.Manifest, error) {
	f.calls.Add(1)
	if f.read != nil {
		return f.read(ctx, token)
	}
	return runtime.Manifest{}, nil
}

// startupBrowserFixture 记录健康检查与正常关闭顺序，不启动浏览器。
type startupBrowserFixture struct {
	healthy bool
	stopErr error
	events  *[]string
}

// Health 模拟已运行或不存在的 Worker。
func (f *startupBrowserFixture) Health(context.Context) error {
	*f.events = append(*f.events, "health")
	if !f.healthy {
		return errors.New("没有运行中的 Worker")
	}
	return nil
}

// StopBrowser 模拟已有浏览器正常退出或返回明确失败。
func (f *startupBrowserFixture) StopBrowser(context.Context) (contract.BrowserStatus, error) {
	*f.events = append(*f.events, "browser")
	return contract.BrowserStatus{}, f.stopErr
}

// startupTasksFixture 模拟任务占用状态，验证组件检查不会停止任务。
type startupTasksFixture bool

// HasActive 返回测试设定的任务占用状态。
func (f startupTasksFixture) HasActive() bool { return bool(f) }

// waitStartupFixture 等待已创建的检查退出，超时立即失败，防止后台协程泄露到下一个测试。
func waitStartupFixture(t *testing.T, syncer *startupCamoufoxSync) {
	t.Helper()
	syncer.mu.Lock()
	done := syncer.done
	syncer.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("后台组件检查没有按时退出")
	}
}

// startupBindFixture 通过正式绑定路由提交测试凭证，响应完成代表没有等待组件下载。
func startupBindFixture(t *testing.T, server *Server, token string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/bind", strings.NewReader(fmt.Sprintf(`{"token":%q}`, token)))
	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() { server.http.Handler.ServeHTTP(response, request); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.CancelStartupSync(ctx)
		t.Fatal("绑定响应等待了后台组件检查")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("正常绑定被后台检查阻断：status=%d", response.Code)
	}
}

// startupBindingCloudFixture 提供正式绑定与会话验证响应，额外请求交给测试指定的组件清单处理器。
func startupBindingCloudFixture(t *testing.T, token string, manifest http.HandlerFunc) *httptest.Server {
	t.Helper()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("本次绑定凭证没有用于同一云端请求")
		}
		switch r.URL.Path {
		case "/api/agents/bind":
			_, _ = w.Write([]byte(`{"agent":{"machine_id":"fixture-device","bind_status":"active"}}`))
		case "/api/auth/me":
			_, _ = w.Write([]byte(`{"user":{"id":"fixture-user"}}`))
		case "/api/runtime/config":
			manifest(w, r)
		default:
			t.Error("出现计划外的云端请求")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(remote.Close)
	return remote
}

// TestStartupSyncBindingDoesNotPersistToken 验证正式绑定自动读取清单，凭证只保留内存，不进入 SQLite 或其他文件。
func TestStartupSyncBindingDoesNotPersistToken(t *testing.T) {
	const token = "fixture-startup-token-must-stay-in-memory-20261026"
	var requests atomic.Int32
	remote := startupBindingCloudFixture(t, token, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"config":{"runtime_components":{"camoufox":{"mac":{"version":"152"}}}}}`))
	})
	root := t.TempDir()
	store, err := storage.Open(filepath.Join(root, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := NewServer(config.Config{DataDir: root}, Dependencies{Store: store, Cloud: cloud.New(remote.URL), DeviceID: func() (string, error) { return "fixture-device", nil }})
	fakeRuntime := &startupRuntimeFixture{pending: true}
	server.startupSync = &startupCamoufoxSync{runtime: fakeRuntime, cloud: server.cloud}
	startupBindFixture(t, server, token)
	waitStartupFixture(t, server.startupSync)
	if requests.Load() != 1 || fakeRuntime.StartupCamoufoxSyncPending() {
		t.Fatal("绑定没有自动完成一次组件检查")
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		content, err := os.ReadFile(path)
		if err == nil && bytes.Contains(content, []byte(token)) {
			return errors.New("登录凭证被保存到了本地文件")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// TestStartupSyncDeduplicatesBindingsAndRetries 验证慢请求不阻塞绑定、并发检查只启动一次，失败脱敏且允许重试。
func TestStartupSyncDeduplicatesBindingsAndRetries(t *testing.T) {
	const token = "fixture-retry-token"
	entered, release := make(chan struct{}), make(chan struct{})
	remote := startupBindingCloudFixture(t, token, func(http.ResponseWriter, *http.Request) {})
	server := NewServer(config.Config{}, Dependencies{Cloud: cloud.New(remote.URL), DeviceID: func() (string, error) { return "fixture-device", nil }})
	fakeRuntime := &startupRuntimeFixture{pending: true}
	fakeCloud := &startupCloudFixture{}
	fakeCloud.read = func(ctx context.Context, received string) (runtime.Manifest, error) {
		if fakeCloud.calls.Load() == 1 {
			close(entered)
			select {
			case <-release:
				return runtime.Manifest{}, fmt.Errorf("模拟上游错误 %s", received)
			case <-ctx.Done():
				return runtime.Manifest{}, ctx.Err()
			}
		}
		return runtime.Manifest{}, nil
	}
	server.startupSync = &startupCamoufoxSync{runtime: fakeRuntime, cloud: fakeCloud}
	startupBindFixture(t, server, token)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("后台检查未开始")
	}
	startupBindFixture(t, server, token)
	if fakeCloud.calls.Load() != 1 {
		t.Fatal("重复绑定启动了第二个组件检查")
	}
	close(release)
	waitStartupFixture(t, server.startupSync)
	if len(fakeRuntime.failures) != 1 || strings.Contains(fakeRuntime.failures[0], token) || !fakeRuntime.StartupCamoufoxSyncPending() {
		t.Fatal("失败未保持门闩，或错误泄露了凭证")
	}
	startupBindFixture(t, server, token)
	waitStartupFixture(t, server.startupSync)
	startupBindFixture(t, server, token)
	if fakeCloud.calls.Load() != 2 || fakeRuntime.StartupCamoufoxSyncPending() {
		t.Fatal("失败后未能重试，或成功后又重复检查")
	}
	_ = server.Shutdown(context.Background())
}

// TestStartupSyncProtectsTasksAndClosesWorker 验证无任务时先关健康浏览器，再停当前管理器的 Worker，健康检查失败也不能漏停。
func TestStartupSyncProtectsTasksAndClosesWorker(t *testing.T) {
	for _, test := range []struct {
		name                      string
		active, healthy, external bool
		stopErr                   error
		want                      string
		wantError                 bool
	}{
		{"active_task", true, true, false, nil, "", true},
		{"worker_absent", false, false, false, nil, "health,worker,health", false},
		{"unhealthy_worker_recovers", false, false, true, nil, "health,worker,health", true},
		{"healthy_worker", false, true, false, nil, "health,browser,worker,health", false},
		{"external_worker_remains", false, true, true, nil, "health,browser,worker,health", true},
		{"close_failed", false, true, false, errors.New("保留原窗口"), "health,browser", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := []string{}
			browser := &startupBrowserFixture{test.healthy, test.stopErr, &events}
			fakeRuntime := &startupRuntimeFixture{stop: func() error {
				events = append(events, "worker")
				browser.healthy = test.external
				return nil
			}}
			syncer := &startupCamoufoxSync{runtime: fakeRuntime, tasks: startupTasksFixture(test.active), browser: browser}
			err := syncer.beforeInstall(context.Background())
			if (err != nil) != test.wantError || strings.Join(events, ",") != test.want {
				t.Fatalf("关闭顺序不正确：events=%v err=%v", events, err)
			}
		})
	}
}

// TestStartupSyncUnhealthyWorkerStopFailure 验证不健康但仍被管理的旧 Worker 停止失败时拒绝换包。
func TestStartupSyncUnhealthyWorkerStopFailure(t *testing.T) {
	events := []string{}
	browser := &startupBrowserFixture{healthy: false, events: &events}
	fakeRuntime := &startupRuntimeFixture{stop: func() error {
		events = append(events, "worker")
		return errors.New("旧 Worker 还没退出")
	}}
	syncer := &startupCamoufoxSync{runtime: fakeRuntime, browser: browser}
	if err := syncer.beforeInstall(context.Background()); err == nil || strings.Join(events, ",") != "health,worker" {
		t.Fatalf("未停止的旧 Worker 不能放行换包：events=%v err=%v", events, err)
	}
}

// TestStartupSyncConcurrentWorkerGatePreservesFailure 验证并发启动请求不会占用安装锁吞掉配置失败，导致页面永久显示安装中。
func TestStartupSyncConcurrentWorkerGatePreservesFailure(t *testing.T) {
	if goruntime.GOOS != "darwin" && !(goruntime.GOOS == "windows" && goruntime.GOARCH == "amd64") {
		t.Skip("当前系统不发布本地程序")
	}
	manager := runtime.New("", "", t.TempDir(), "", nil)
	manager.EnableStartupCamoufoxSync()
	stop := make(chan struct{})
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = manager.EnsureWorker(context.Background())
				}
			}
		}()
	}
	defer func() { close(stop); workers.Wait() }()
	for range 1000 {
		manager.BeginStartupCamoufoxSyncCheck()
		manager.FailStartupCamoufoxSync(errors.New("模拟配置读取失败"))
		if progress := manager.InstallProgress(); progress.Running || progress.Stage != "failed" {
			t.Fatalf("并发启动吞掉了配置失败状态：%+v", progress)
		}
	}
}

// TestStartupSyncShutdownCancelsPendingRequest 验证退出取消正在等待的请求、等待协程结束，且不允许新检查继续落地。
func TestStartupSyncShutdownCancelsPendingRequest(t *testing.T) {
	entered := make(chan struct{})
	fakeRuntime := &startupRuntimeFixture{pending: true}
	fakeCloud := &startupCloudFixture{read: func(ctx context.Context, _ string) (runtime.Manifest, error) {
		close(entered)
		<-ctx.Done()
		return runtime.Manifest{}, ctx.Err()
	}}
	syncer := &startupCamoufoxSync{runtime: fakeRuntime, cloud: fakeCloud}
	syncer.start("fixture")
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("后台请求未开始")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := syncer.stop(ctx); err != nil {
		t.Fatal(err)
	}
	syncer.start("fixture")
	if fakeCloud.calls.Load() != 1 || fakeRuntime.calls != 0 || !fakeRuntime.StartupCamoufoxSyncPending() {
		t.Fatal("取消后仍安装组件或重开后台请求")
	}
}

// TestStartupSyncManualRetryFetchesAuthoritativeConfig 验证首次取清单失败后，原有安装按钮会重取线上清单而非接受旧缓存包。
func TestStartupSyncManualRetryFetchesAuthoritativeConfig(t *testing.T) {
	if goruntime.GOOS != "darwin" && !(goruntime.GOOS == "windows" && goruntime.GOARCH == "amd64") {
		t.Skip("当前系统不发布本地程序")
	}
	systemNode, nodeLookupErr := exec.LookPath("node")
	for _, nodeMissing := range []bool{false, true} {
		t.Run(fmt.Sprintf("node_missing_%t", nodeMissing), func(t *testing.T) {
			nodeDir := t.TempDir()
			nodePath := filepath.Join(nodeDir, "missing-node")
			if !nodeMissing {
				if nodeLookupErr != nil {
					t.Fatal("此回归需要已安装的 Node 22 或更高：", nodeLookupErr)
				}
				nodePath = systemNode
			}
			t.Setenv("PATH", nodeDir)
			var requests atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			remote := startupBindingCloudFixture(t, "fixture", func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 2 {
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
					}
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			})
			manager := runtime.New(nodePath, "", t.TempDir(), "", nil)
			manager.EnableStartupCamoufoxSync()
			server := NewServer(config.Config{}, Dependencies{Runtime: manager, Runner: &lifecycle.Runner{}, Cloud: cloud.New(remote.URL), DeviceID: func() (string, error) { return "fixture-device", nil }})
			defer server.Shutdown(context.Background())
			startupBindFixture(t, server, "fixture")
			waitStartupFixture(t, server.startupSync)
			if manager.InstallProgress().Stage != "failed" {
				t.Fatal("首次取配置失败没有明确状态")
			}
			response := httptest.NewRecorder()
			server.http.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/runtime/install", strings.NewReader(`{"manifest":{}}`)))
			wantStatus := http.StatusAccepted
			if nodeMissing {
				wantStatus = http.StatusConflict
				if !strings.Contains(response.Body.String(), "CAMOUFOX_RETRY_BEFORE_RUNTIME_INSTALL") {
					t.Fatal("缺 Node 时没有提示完成浏览器更新后安装剩余组件")
				}
			}
			if response.Code != wantStatus || !manager.InstallProgress().Running || manager.InstallProgress().Stage != "queued" {
				t.Fatalf("手动重试未按现有组件状态响应：status=%d progress=%+v", response.Code, manager.InstallProgress())
			}
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("重试没有获取原始线上清单")
			}
			close(release)
			waitStartupFixture(t, server.startupSync)
			if requests.Load() != 2 || !manager.StartupCamoufoxSyncPending() {
				t.Fatal("重试失败时旧缓存不应打开门闩")
			}
		})
	}
}

// TestStartupSyncStatusCannotReuseOldWorker 验证旧 Worker 报已安装时，API 合并状态仍不能越过启动检查。
func TestStartupSyncStatusCannotReuseOldWorker(t *testing.T) {
	if goruntime.GOOS != "darwin" && !(goruntime.GOOS == "windows" && goruntime.GOARCH == "amd64") {
		t.Skip("当前系统不发布本地程序")
	}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"ok":true,"data":{"status":"ok"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"installed":true,"binary_path":"old-browser"}}`))
	}))
	defer worker.Close()
	manager := runtime.New("", "", t.TempDir(), "", nil)
	manager.EnableStartupCamoufoxSync()
	server := &Server{runtime: manager, browser: client.New(worker.URL)}
	status := server.runtimeStatus(context.Background(), false)
	if !status.WorkerReady || status.CamoufoxInstalled || status.CamoufoxReady {
		t.Fatalf("旧 Worker 越过门闩：%+v", status)
	}
}
