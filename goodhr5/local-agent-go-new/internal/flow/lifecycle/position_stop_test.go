// Package lifecycle 文件作用：验证岗位停止的云端兜底、账号归属、安全收尾和并发启动保护。
package lifecycle

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/preflight"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/profile"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/power"
)

// TestStopPositionWithoutActiveTask 验证无本地运行任务时直接停止云端，且不改写本地历史。
func TestStopPositionWithoutActiveTask(t *testing.T) {
	for _, test := range []struct {
		name       string
		history    bool
		cloudError bool
		repeats    int
	}{
		{name: "no_history", repeats: 2},
		{name: "old_running_history", history: true, repeats: 2},
		{name: "no_history_cloud_failure", cloudError: true, repeats: 1},
		{name: "old_running_cloud_failure", history: true, cloudError: true, repeats: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stopCalls atomic.Int32
			runner, store := newPositionStopRunner(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodPost || request.URL.Path != "/api/positions/position-stop/stop" || request.Header.Get("Authorization") != "Bearer current-token" {
					t.Errorf("停止请求错误：%s %s", request.Method, request.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				stopCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if test.cloudError {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"error":{"code":"CLOUD_BUSY","message":"请稍后再试"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"position":{"id":"position-stop","status":"stopped"},"status":"stopped"}`))
			}))
			var history storage.TaskRun
			if test.history {
				seed := storage.TaskRun{
					TaskID: "old-task", PositionID: "position-stop", PlatformID: "boss",
					TaskType: "greeting", Status: "running", CurrentStep: "read_candidate",
					Summary: "原来的任务记录", StartedAt: time.Now().UTC().Add(-time.Hour),
				}
				if err := store.SaveTask(context.Background(), seed); err != nil {
					t.Fatal(err)
				}
				if err := store.SaveCandidate(context.Background(), storage.CandidateRecord{
					TaskID: seed.TaskID, Fingerprint: "candidate-1", Action: "greet", Result: "success",
				}); err != nil {
					t.Fatal(err)
				}
				var err error
				history, err = store.Task(context.Background(), seed.TaskID)
				if err != nil {
					t.Fatal(err)
				}
			}
			for repeat := 0; repeat < test.repeats; repeat++ {
				task, err := runner.StopPosition(context.Background(), " position-stop ", " current-token ")
				if test.cloudError {
					assertPositionStopAPIError(t, err, 503, "CLOUD_BUSY")
					if task.Status == "stopped" {
						t.Fatal("云端失败不应返回停止成功")
					}
				} else {
					if err != nil || task.Status != "stopped" || task.PositionID != "position-stop" {
						t.Fatalf("岗位停止结果错误：%+v, %v", task, err)
					}
					if test.history {
						want := history
						want.Status = "stopped"
						if !reflect.DeepEqual(task, want) {
							t.Fatalf("响应更改了历史统计或摘要：%+v", task)
						}
					} else if task.TaskID != "" || !task.StartedAt.IsZero() || !task.FinishedAt.IsZero() {
						t.Fatalf("无历史时不应虚构任务：%+v", task)
					}
				}
			}
			if int(stopCalls.Load()) != test.repeats {
				t.Fatalf("重复停止没有逐次核对云端：%d", stopCalls.Load())
			}
			if test.history {
				after, err := store.Task(context.Background(), history.TaskID)
				if err != nil || !reflect.DeepEqual(after, history) {
					t.Fatalf("停止兜底改写了历史记录：%+v, %v", after, err)
				}
			} else if _, err := store.LatestTaskForPosition(context.Background(), "position-stop"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("停止兜底不应创建本地历史：%v", err)
			}
		})
	}
}

// TestStopPositionRejectsUnauthorizedActiveTask 验证当前凭证无权限时不会向本地任务发出停止信号。
func TestStopPositionRejectsUnauthorizedActiveTask(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			runner, store := newPositionStopRunner(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				if request.Method != http.MethodGet || request.URL.Path != "/api/positions/position-stop" || request.Header.Get("Authorization") != "Bearer current-token" {
					t.Errorf("未使用当前账号校验岗位归属：%s %s", request.Method, request.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"code":"POSITION_DENIED","message":"这个账号不能访问岗位"}}`))
			}))
			active, runCtx := addPositionStopTask(t, runner, store, "task-stop", "position-stop")
			_, err := runner.StopPosition(context.Background(), "position-stop", "current-token")
			assertPositionStopAPIError(t, err, status, "POSITION_DENIED")
			if calls.Load() != 1 || active.stopped {
				t.Fatal("归属校验失败后仍然尝试停止")
			}
			select {
			case <-active.stop:
				t.Fatal("归属校验失败不应发送安全停止信号")
			case <-runCtx.Done():
				t.Fatal("归属校验失败不应取消当前任务")
			default:
			}
		})
	}
}

// TestStopPositionWaitsForSafeFinish 验证停止等待当前候选人和云端收尾，且不影响其他任务。
func TestStopPositionWaitsForSafeFinish(t *testing.T) {
	finishEntered, releaseFinish := make(chan struct{}), make(chan struct{})
	stopEntered, releaseStop := make(chan struct{}), make(chan struct{})
	var stopCalls atomic.Int32
	runner, store := newPositionStopRunner(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/positions/position-stop":
			if request.Header.Get("Authorization") != "Bearer current-token" {
				t.Error("归属校验借用了旧任务凭证")
			}
			_, _ = w.Write([]byte(`{"position":{"id":"position-stop","platform_id":"boss"}}`))
		case request.URL.Path == "/api/positions/position-stop/status":
			close(finishEntered)
			<-releaseFinish
			_, _ = w.Write([]byte(`{"success":true}`))
		case request.URL.Path == "/api/positions/position-stop/stop":
			if request.Header.Get("Authorization") != "Bearer current-token" {
				t.Error("停止确认借用了旧任务凭证")
			}
			stopCalls.Add(1)
			close(stopEntered)
			<-releaseStop
			_, _ = w.Write([]byte(`{"position":{"id":"position-stop","status":"stopped"}}`))
		default:
			_, _ = w.Write([]byte(`{"success":true}`))
		}
	}))
	t.Cleanup(func() {
		select {
		case <-releaseFinish:
		default:
			close(releaseFinish)
		}
		select {
		case <-releaseStop:
		default:
			close(releaseStop)
		}
	})
	active, runCtx := addPositionStopTask(t, runner, store, "task-stop", "position-stop")
	other, otherCtx := addPositionStopTask(t, runner, store, "task-other", "position-other")
	runner.profiles = profile.New(filepath.Join(t.TempDir(), "profiles"))
	runner.power = &power.Guard{}
	if err := runner.profiles.Acquire("profile-stop", active.state.TaskID); err != nil {
		t.Fatal(err)
	}
	result := make(chan struct {
		task storage.TaskRun
		err  error
	}, 1)
	go func() {
		task, err := runner.StopPosition(context.Background(), "position-stop", "current-token")
		result <- struct {
			task storage.TaskRun
			err  error
		}{task: task, err: err}
	}()
	waitPositionStopSignal(t, active.stop)
	select {
	case <-runCtx.Done():
		t.Fatal("停止请求取消了当前候选人上下文")
	default:
	}
	if stopCalls.Load() != 0 {
		t.Fatal("当前候选人尚未收尾时提前停止了云端")
	}
	go runner.finish(active, shared.Stats{Processed: 3, Succeeded: 1}, nil)
	waitPositionStopSignal(t, finishEntered)
	if runner.ActiveTaskIDForPosition("position-stop") != "task-stop" || stopCalls.Load() != 0 {
		t.Fatal("旧任务云端收尾结束前移除了运行登记或确认了停止")
	}
	_, err := runner.StartTask(context.Background(), shared.StartRequest{PositionID: "position-stop"})
	assertPositionStopAPIError(t, err, 409, "POSITION_STOPPING")
	close(releaseFinish)
	waitPositionStopSignal(t, stopEntered)
	_, err = runner.StartTask(context.Background(), shared.StartRequest{PositionID: "position-stop"})
	assertPositionStopAPIError(t, err, 409, "POSITION_STOPPING")
	close(releaseStop)
	select {
	case response := <-result:
		if response.err != nil || response.task.TaskID != "task-stop" || response.task.Status != "stopped" {
			t.Fatalf("安全停止结果错误：%+v, %v", response.task, response.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("安全停止没有结束")
	}
	if runner.ActiveTaskIDForPosition("position-other") != other.state.TaskID || other.stopped {
		t.Fatal("停止指定岗位影响了其他任务")
	}
	select {
	case <-other.stop:
		t.Fatal("其他任务收到停止信号")
	case <-otherCtx.Done():
		t.Fatal("其他任务上下文被取消")
	default:
	}
	runner.mu.Lock()
	stillStopping := runner.stoppingPositions["position-stop"] > 0
	runner.mu.Unlock()
	if stillStopping {
		t.Fatal("确认停止之后未释放岗位启动保护")
	}
}

// TestStopPositionPreservesInfrastructureErrors 验证凭证、连接和 SQLite 异常不会被停止兜底掩盖。
func TestStopPositionPreservesInfrastructureErrors(t *testing.T) {
	var calls atomic.Int32
	runner, store := newPositionStopRunner(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_, err := runner.StopPosition(context.Background(), "position-stop", " ")
	assertPositionStopAPIError(t, err, 400, "TOKEN_REQUIRED")
	client := runner.cloud
	runner.cloud = nil
	_, err = runner.StopPosition(context.Background(), "position-stop", "current-token")
	assertPositionStopAPIError(t, err, 503, "CLOUD_UNAVAILABLE")
	runner.cloud = client
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = runner.StopPosition(context.Background(), "position-stop", "current-token")
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("真实 SQLite 故障被当成了没有历史：%v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("本地参数或数据库异常时仍然请求了云端停止")
	}
}

// TestStopPositionConcurrentRequestsKeepStartBlocked 验证一个停止请求完成后，另一个未完成请求仍保护同岗位启动。
func TestStopPositionConcurrentRequestsKeepStartBlocked(t *testing.T) {
	firstEntered, secondEntered := make(chan struct{}), make(chan struct{})
	releaseFirst, releaseSecond := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	runner, _ := newPositionStopRunner(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch calls.Add(1) {
		case 1:
			close(firstEntered)
			<-releaseFirst
		case 2:
			close(secondEntered)
			<-releaseSecond
		default:
			t.Error("并发停止发出了多余的云端请求")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"position":{"id":"position-stop","status":"stopped"}}`))
	}))
	t.Cleanup(func() {
		select {
		case <-releaseFirst:
		default:
			close(releaseFirst)
		}
		select {
		case <-releaseSecond:
		default:
			close(releaseSecond)
		}
	})
	firstResult, secondResult := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := runner.StopPosition(context.Background(), "position-stop", "current-token")
		firstResult <- err
	}()
	waitPositionStopSignal(t, firstEntered)
	go func() {
		_, err := runner.StopPosition(context.Background(), "position-stop", "current-token")
		secondResult <- err
	}()
	waitPositionStopSignal(t, secondEntered)
	close(releaseFirst)
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	_, err := runner.StartTask(context.Background(), shared.StartRequest{PositionID: "position-stop"})
	assertPositionStopAPIError(t, err, 409, "POSITION_STOPPING")
	close(releaseSecond)
	if err := <-secondResult; err != nil {
		t.Fatal(err)
	}
	runner.mu.Lock()
	remaining := runner.stoppingPositions["position-stop"]
	runner.mu.Unlock()
	if remaining != 0 || calls.Load() != 2 {
		t.Fatalf("并发停止没有正确完成或释放保护：remaining=%d calls=%d", remaining, calls.Load())
	}
}

// TestStopPositionCanceledWaitPreservesCandidateContext 验证调用方取消等待时保留当前候选人处理，并返回取消错误。
func TestStopPositionCanceledWaitPreservesCandidateContext(t *testing.T) {
	var stopCalls atomic.Int32
	runner, store := newPositionStopRunner(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet && request.URL.Path == "/api/positions/position-stop" {
			_, _ = w.Write([]byte(`{"position":{"id":"position-stop","platform_id":"boss"}}`))
			return
		}
		stopCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	active, runCtx := addPositionStopTask(t, runner, store, "task-stop", "position-stop")
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := runner.StopPosition(requestCtx, "position-stop", "current-token")
		result <- err
	}()
	waitPositionStopSignal(t, active.stop)
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("停止等待取消错误被掩盖：%v", err)
	}
	select {
	case <-runCtx.Done():
		t.Fatal("调用方取消等待不应取消当前候选人")
	default:
	}
	if stopCalls.Load() != 0 {
		t.Fatal("任务尚未安全收尾时不应确认云端停止")
	}
}

// TestStopPositionDuringSlowPreflightRejectsOldStart 验证停止先完成后，旧启动仍被拒绝，新的主动启动不受旧停止失败影响。
func TestStopPositionDuringSlowPreflightRejectsOldStart(t *testing.T) {
	for _, test := range []struct {
		name       string
		cloudError bool
	}{
		{name: "stop_confirmed"},
		{name: "stop_cloud_failure", cloudError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var startCalls atomic.Int32
			runner, store := newPositionStopRunner(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch request.URL.Path {
				case "/api/auth/me":
					_, _ = w.Write([]byte(`{"user":{"id":"user-stop"}}`))
				case "/api/agents/bind":
					_, _ = w.Write([]byte(`{"agent":{"machine_id":"machine-stop","bind_status":"active"}}`))
				case "/api/positions/position-stop":
					_, _ = w.Write([]byte(`{"position":{"id":"position-stop","platform_id":"boss","profile_id":"profile-stop","common_config":{"mode_default":"keyword","detail_mode":"text"}}}`))
				case "/api/config/user-preferences":
					_, _ = w.Write([]byte(`{"config":{}}`))
				case "/api/positions/position-stop/stop":
					if test.cloudError {
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = w.Write([]byte(`{"error":{"code":"CLOUD_BUSY","message":"请稍后再试"}}`))
						return
					}
					_, _ = w.Write([]byte(`{"position":{"id":"position-stop","status":"stopped"}}`))
				case "/api/positions/position-stop/start":
					startCalls.Add(1)
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(`{"error":{"code":"TEST_START_DENIED","message":"测试在云端启动前结束"}}`))
				default:
					_, _ = w.Write([]byte(`{"success":true}`))
				}
			}))
			checks := &positionStopPreflightRuntime{entered: make(chan struct{}), release: make(chan struct{})}
			t.Cleanup(func() {
				select {
				case <-checks.release:
				default:
					close(checks.release)
				}
			})
			runner.profiles = profile.New(filepath.Join(t.TempDir(), "profiles"))
			runner.checker = &preflight.Checker{
				Cloud: runner.cloud, Storage: store, Profiles: runner.profiles,
				Runtime: checks, Browser: checks, Power: checks,
				AgentBind: cloud.AgentBindRequest{MachineID: "machine-stop"},
			}
			request := shared.StartRequest{TaskID: "task-before-stop", PositionID: "position-stop", TaskType: "greeting", Token: "current-token"}
			startResult := make(chan error, 1)
			go func() {
				_, err := runner.StartTask(context.Background(), request)
				startResult <- err
			}()
			waitPositionStopSignal(t, checks.entered)
			_, stopErr := runner.StopPosition(context.Background(), "position-stop", "current-token")
			if test.cloudError {
				assertPositionStopAPIError(t, stopErr, 503, "CLOUD_BUSY")
			} else if stopErr != nil {
				t.Fatal(stopErr)
			}
			close(checks.release)
			assertPositionStopAPIError(t, <-startResult, 409, "POSITION_STOPPING")
			if exists, err := store.TaskExists(context.Background(), request.TaskID); err != nil || exists || runner.HasActive() || startCalls.Load() != 0 {
				t.Fatalf("停止前的慢启动仍登记了任务或请求云端启动：exists=%v err=%v calls=%d", exists, err, startCalls.Load())
			}
			request.TaskID = "task-after-stop"
			_, err := runner.StartTask(context.Background(), request)
			assertPositionStopAPIError(t, err, 409, "TEST_START_DENIED")
			if startCalls.Load() != 1 {
				t.Fatal("停止结束后发起的新启动被旧停止代数永久阻止")
			}
		})
	}
}

// positionStopPreflightRuntime 模拟浏览器检查耗时，其余检查保持可用且不启动真实浏览器。
type positionStopPreflightRuntime struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// CheckNode 模拟已安装 Node.js 运行时。
func (*positionStopPreflightRuntime) CheckNode() error { return nil }

// CheckWorkerBuild 模拟已经存在浏览器操作程序。
func (*positionStopPreflightRuntime) CheckWorkerBuild() error { return nil }

// EnsureWorker 暂停第一次启动检查，便于稳定复现启动与停止的交错顺序。
func (r *positionStopPreflightRuntime) EnsureWorker(context.Context) error {
	r.once.Do(func() {
		close(r.entered)
		<-r.release
	})
	return nil
}

// Health 模拟浏览器操作程序能够响应健康检查。
func (*positionStopPreflightRuntime) Health(context.Context) error { return nil }

// BrowserStatus 模拟浏览器会话可用，不操作真实浏览器。
func (*positionStopPreflightRuntime) BrowserStatus(context.Context) (contract.BrowserStatus, error) {
	return contract.BrowserStatus{Running: true}, nil
}

// RuntimeStatus 模拟 Camoufox 运行组件已经安装。
func (*positionStopPreflightRuntime) RuntimeStatus(context.Context) (contract.WorkerRuntimeStatus, error) {
	return contract.WorkerRuntimeStatus{Installed: true}, nil
}

// Available 模拟防休眠能力可用，避免测试修改系统电源设置。
func (*positionStopPreflightRuntime) Available() error { return nil }

// newPositionStopRunner 创建可核对真实 HTTP 请求和 SQLite 历史的岗位停止测试环境。
func newPositionStopRunner(t *testing.T, handler http.Handler) (*Runner, *storage.Store) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
		_ = store.Close()
	})
	return &Runner{active: make(map[string]*activeTask), store: store, cloud: cloud.New(server.URL)}, store
}

// addPositionStopTask 创建带独立上下文和旧凭证的运行任务，供停止安全性测试使用。
func addPositionStopTask(t *testing.T, runner *Runner, store *storage.Store, taskID, positionID string) (*activeTask, context.Context) {
	t.Helper()
	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	state := storage.TaskRun{TaskID: taskID, PositionID: positionID, PlatformID: "boss", TaskType: "greeting", Status: "running", StartedAt: time.Now().UTC()}
	if err := store.SaveTask(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	active := &activeTask{
		prepared: shared.PreparedTask{
			Request:  shared.StartRequest{TaskID: taskID, PositionID: positionID, ProfileID: "profile-stop", Token: "old-token", TaskType: "greeting"},
			Position: cloud.PositionSnapshot{ID: positionID, PlatformID: "boss"},
		},
		state: state, cancel: cancel, done: make(chan struct{}), stop: make(chan struct{}),
	}
	runner.active[taskID] = active
	return active, runCtx
}

// assertPositionStopAPIError 核对岗位停止返回的稳定 HTTP 状态与错误码。
func assertPositionStopAPIError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var apiErr *cloud.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.Code != code {
		t.Fatalf("停止错误不正确：%v，期望 %d/%s", err, status, code)
	}
}

// waitPositionStopSignal 等待停止测试的确定性阶段信号，避免依赖固定休眠。
func waitPositionStopSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("等待岗位停止阶段超时")
	}
}
