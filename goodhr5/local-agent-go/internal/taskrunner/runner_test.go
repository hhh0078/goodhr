// Package taskrunner 负责测试 Go 本地任务运行器。
package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/config"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/ocr"
	"goodhr5/local-agent-go/internal/platforms"
)

// TestRunnerStartStop 验证任务启动会校验会员、读取平台配置、扫描候选人并更新状态。
func TestRunnerStartStop(t *testing.T) {
	speedUpPageEntryCheck(t)
	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected ai path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"score":82,"reason":"符合要求"}`}},
			},
			"usage": map[string]any{"total_tokens": 12},
		})
	}))
	defer aiServer.Close()
	var task localdb.Task
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscription/status":
			if r.Header.Get("Authorization") != "Bearer token-1" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":           true,
				"subscription": map[string]any{"active": true},
			})
		case "/api/platforms/config/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"configs": []map[string]any{
					{"config_key": "platform.boss", "config_value": `{"id":"boss","name":"Boss直聘","auth":{"pages":[{"url":"https://www.zhipin.com/web/chat/other"},{"url":"https://www.zhipin.com/web/chat/recommend","entry":true}]},"position":{"current":{"target_classes":[["current-position"]]},"switchBtn":{"target_classes":[["switch-position"]]},"list":{"target_classes":[["position-list"]]},"item":{"target_classes":[["position-item"]]},"itemText":{"target_classes":[["position-name"]]}}}`},
				},
			})
		case "/api/config/user-preferences":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "config": map[string]any{}})
		case "/api/config/effective-ai":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "config": map[string]any{"base_url": aiServer.URL, "api_key": "test-key", "model": "test-model", "temperature": 0.2}})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/tasks/") && strings.HasSuffix(r.URL.Path, "/candidates") {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/tasks/") {
				requestedID := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
				taskName := "本地任务"
				if requestedID != task.ID {
					taskName = "本地任务2"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "task": map[string]any{"id": requestedID, "name": taskName, "platform_id": "boss", "mode": "ai", "match_limit": 1, "position": map[string]any{"name": taskName}}})
				return
			}
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	task, err := db.CreateTask(map[string]any{"name": "本地任务", "platform_id": "boss", "position_snapshot": map[string]any{"name": "本地任务"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveAIConfig(map[string]any{
		"base_url": aiServer.URL,
		"api_key":  "test-key",
		"model":    "test-model",
	}); err != nil {
		t.Fatal(err)
	}
	worker := &fakeWorker{}
	runner := newTestRunner(t, db, worker)
	result, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", PageReadyDelay: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result["running"] != true {
		t.Fatalf("result = %+v", result)
	}
	updated := waitForTaskStatus(t, db, task.ID, "completed")
	if updated.ScannedCount != 1 {
		t.Fatalf("scanned count = %d", updated.ScannedCount)
	}
	status, err := runner.Status(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status["running"] != false {
		t.Fatalf("status = %+v", status)
	}
	if status["progress"] == nil || status["logs"] == nil {
		t.Fatalf("status missing progress/logs: %+v", status)
	}
	candidates, err := db.ListCandidates(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0]["candidate_name"] != "候选人A" {
		t.Fatalf("candidates = %+v", candidates)
	}
	if candidates[0]["status"] != "ai_passed" || candidates[0]["ai_greet_score"] == nil {
		t.Fatalf("candidate ai fields = %+v", candidates[0])
	}
	stopResult, err := runner.Stop(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopResult["running"] != false || runner.IsRunning(task.ID) {
		t.Fatalf("stopResult = %+v", stopResult)
	}
	stopped, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != "stopped" {
		t.Fatalf("stopped status = %s", stopped.Status)
	}
	for _, call := range worker.calls {
		if call == "/api/v1/browser/stop" {
			t.Fatal("停止任务不应该关闭浏览器")
		}
	}

	task2, err := db.CreateTask(map[string]any{"name": "本地任务2", "platform_id": "boss", "match_limit": 1, "position_snapshot": map[string]any{"name": "本地任务2"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Start(t.Context(), task2.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", EnableGreet: true, PageReadyDelay: 1}); err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, db, task2.ID, "completed")
	candidates2, err := db.ListCandidates(task2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates2) != 1 || candidates2[0]["status"] != "greeted" {
		t.Fatalf("candidates2 = %+v", candidates2)
	}
}

// TestPlatformEntryURL 验证平台入口页读取规则与云端运行时一致。
func TestPlatformEntryURL(t *testing.T) {
	config := cloudapi.PlatformConfig{
		"auth": map[string]any{
			"pages": []any{
				map[string]any{"url": "https://example.com/first"},
				map[string]any{"url": "https://example.com/entry", "entry": true},
			},
		},
	}
	if url := platformEntryURL(config); url != "https://example.com/entry" {
		t.Fatalf("entry url = %s", url)
	}
	fallbackConfig := cloudapi.PlatformConfig{
		"auth": map[string]any{
			"pages": []any{
				map[string]any{"url": "https://example.com/first"},
			},
		},
	}
	if url := platformEntryURL(fallbackConfig); url != "https://example.com/first" {
		t.Fatalf("fallback url = %s", url)
	}
	legacyConfig := cloudapi.PlatformConfig{
		"pages": []any{
			map[string]any{"url": "https://example.com/legacy"},
		},
	}
	if url := platformEntryURL(legacyConfig); url != "https://example.com/legacy" {
		t.Fatalf("legacy url = %s", url)
	}
}

// TestRunnerStartRequiresToken 验证空 token 会在启动前被拦截。
func TestRunnerStartRequiresToken(t *testing.T) {
	db := openRunnerTestDB(t)
	task, err := db.CreateTask(map[string]any{"name": "本地任务", "platform_id": "boss", "position_snapshot": map[string]any{"name": "本地任务"}})
	if err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, db, &fakeWorker{})
	if _, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: "https://goodhr5.58it.cn"}); err == nil || err.Error() != "请先登录后再校验会员" {
		t.Fatalf("err = %v", err)
	}
	updated, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status == "running" {
		t.Fatalf("空 token 不应启动任务，当前状态=%s", updated.Status)
	}
}

// TestRunnerMissingEntryURLDoesNotStartBrowser 验证缺少入口页时不会启动浏览器。
func TestRunnerMissingEntryURLDoesNotStartBrowser(t *testing.T) {
	db := openRunnerTestDB(t)
	task := localdb.Task{ID: "task-1", PlatformID: "boss"}
	worker := &fakeWorker{}
	runner := newTestRunner(t, db, worker)
	if _, err := runner.scanOnce(t.Context(), task, cloudapi.PlatformConfig{"auth": map[string]any{"pages": []any{}}}, StartOptions{}); err == nil || err.Error() != "云端平台配置缺少入口页面地址" {
		t.Fatalf("err = %v", err)
	}
	if len(worker.calls) != 0 {
		t.Fatalf("缺少入口页时不应启动浏览器，calls=%v", worker.calls)
	}
}

// TestEnsureTaskPageReadyRetries 验证页面刚打开时会等待多次检查。
func TestEnsureTaskPageReadyRetries(t *testing.T) {
	speedUpPageEntryCheck(t)

	db := openRunnerTestDB(t)
	task := localdb.Task{ID: "task-1", PlatformID: "boss", PositionSnapshot: map[string]any{"name": "本地任务"}}
	worker := &fakeWorker{pageListEmptyBefore: 5}
	runner := newTestRunner(t, db, worker)
	platformConfig := cloudapi.PlatformConfig{
		"auth": map[string]any{
			"pages": []any{map[string]any{"url": "https://www.zhipin.com/web/chat/recommend", "entry": true}},
		},
		"position": map[string]any{
			"current": map[string]any{"target_classes": []any{[]any{"current-position"}}},
		},
	}
	platformRuntime, err := platforms.RuntimeFor("boss")
	if err != nil {
		t.Fatal(err)
	}
	exec := platformExecutor{runner: runner, taskID: task.ID}
	if err := runner.ensureTaskPageReady(t.Context(), task, platformRuntime, exec, platformConfig); err != nil {
		t.Fatal(err)
	}
	if worker.pageListCalls != 6 {
		t.Fatalf("页面检查次数 = %d", worker.pageListCalls)
	}
}

// TestApplyKeywordFilter 验证关键词和排除词过滤。
func TestApplyKeywordFilter(t *testing.T) {
	task := localdb.Task{
		PositionSnapshot: map[string]any{
			"keywords":         []any{"本科", "销售"},
			"exclude_keywords": []any{"外包"},
			"is_and_mode":      true,
		},
	}
	candidates := []map[string]any{
		{"id": "1", "raw_text": "本科 三年 销售经验"},
		{"id": "2", "raw_text": "本科 外包 项目"},
		{"id": "3", "raw_text": "本科 客服"},
	}
	filtered, skipped := applyKeywordFilter(task, candidates)
	if skipped != 2 || len(filtered) != 1 || filtered[0]["id"] != "1" {
		t.Fatalf("filtered = %+v, skipped = %d", filtered, skipped)
	}
}

// TestRunOptionBounds 验证任务运行参数默认值和上限。
func TestRunOptionBounds(t *testing.T) {
	if scanRounds(StartOptions{}) != defaultScanRounds {
		t.Fatal("scanRounds 默认值不正确")
	}
	if maxItemsPerRound(StartOptions{}) != 15 {
		t.Fatal("maxItems 默认值不正确")
	}
	if scanRounds(StartOptions{ScanRounds: 99}) != 20 {
		t.Fatal("scanRounds 上限不正确")
	}
	if maxItemsPerRound(StartOptions{MaxItems: 999}) != 100 {
		t.Fatal("maxItems 上限不正确")
	}
	if scrollDistance(StartOptions{ScrollDistance: 9999}) != 3000 {
		t.Fatal("scrollDistance 上限不正确")
	}
	for i := 0; i < 20; i++ {
		distance := randomScrollDistance(StartOptions{})
		if distance < 560 || distance > 880 {
			t.Fatalf("随机滚动距离超出范围：%d", distance)
		}
	}
	if candidatePipelineConcurrency(2) != 2 || candidatePipelineConcurrency(15) != defaultCandidatePipelineConcurrency {
		t.Fatal("候选人流水线并发数不正确")
	}
}

// TestRunnerStopCancelsRunningTask 验证停止任务会取消正在执行的 Worker 调用。
func TestRunnerStopCancelsRunningTask(t *testing.T) {
	speedUpPageEntryCheck(t)
	var task localdb.Task
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscription/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":           true,
				"subscription": map[string]any{"active": true},
			})
		case "/api/platforms/config/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"configs": []map[string]any{
					{"config_key": "platform.boss", "config_value": `{"id":"boss","pages":[{"url":"https://www.zhipin.com/web/chat/recommend"}],"position":{"current":{"target_classes":[["current-position"]]},"switchBtn":{"target_classes":[["switch-position"]]},"list":{"target_classes":[["position-list"]]},"item":{"target_classes":[["position-item"]]},"itemText":{"target_classes":[["position-name"]]}}}`},
				},
			})
		case "/api/config/user-preferences":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "config": map[string]any{}})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/tasks/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "task": map[string]any{"id": task.ID, "name": "可停止任务", "platform_id": "boss", "mode": "keyword", "position": map[string]any{"name": "可停止任务"}}})
				return
			}
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	task, err := db.CreateTask(map[string]any{"name": "可停止任务", "platform_id": "boss", "mode": "keyword", "position_snapshot": map[string]any{"name": "可停止任务"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := &blockingWorker{extractStarted: make(chan struct{}), released: make(chan struct{})}
	runner := newTestRunner(t, db, worker)
	if _, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", PageReadyDelay: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.extractStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("等待 Worker 提取开始超时")
	}
	status, err := runner.Status(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status["running"] != true {
		t.Fatalf("running status = %+v", status)
	}
	if _, err := runner.Stop(task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.released:
	case <-time.After(2 * time.Second):
		t.Fatal("停止任务后 Worker 未释放")
	}
	waitForTaskStatus(t, db, task.ID, "stopped")
}

// TestRunnerBrowserClosedStopsTask 验证用户关闭浏览器后任务会结束。
func TestRunnerBrowserClosedStopsTask(t *testing.T) {
	speedUpPageEntryCheck(t)
	var task localdb.Task
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscription/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "subscription": map[string]any{"active": true}})
		case "/api/platforms/config/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"configs": []map[string]any{
					{"config_key": "platform.boss", "config_value": `{"id":"boss","pages":[{"url":"https://www.zhipin.com/web/chat/recommend"}],"position":{"current":{"target_classes":[["current-position"]]}}}`},
				},
			})
		case "/api/config/user-preferences":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "config": map[string]any{}})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/tasks/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "task": map[string]any{"id": task.ID, "name": "浏览器关闭任务", "platform_id": "boss", "mode": "keyword", "position": map[string]any{"name": "本地任务"}}})
				return
			}
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	task, err := db.CreateTask(map[string]any{"name": "浏览器关闭任务", "platform_id": "boss", "mode": "keyword", "position_snapshot": map[string]any{"name": "本地任务"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := &fakeWorker{extractErr: errors.New("浏览器已关闭，请重新启动浏览器")}
	runner := newTestRunner(t, db, worker)
	if _, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", PageReadyDelay: 1}); err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, db, task.ID, "stopped")
}

// fakeWorker 模拟浏览器 Worker。
type fakeWorker struct {
	calls               []string
	currentPosition     string
	pageListCalls       int
	pageListEmptyBefore int
	extractErr          error
}

// fakeOCR 模拟 OCR 识别器。
type fakeOCR struct{}

// Recognize 模拟 OCR 图片识别。
// ctx 为请求上下文，imagePath 为图片路径。
func (f fakeOCR) Recognize(ctx context.Context, imagePath string) (ocr.Result, error) {
	return ocr.Result{Text: "OCR 识别文本"}, nil
}

// Start 模拟启动 Worker。
// ctx 为请求上下文。
func (w *fakeWorker) Start(ctx context.Context) (browser.WorkerStatus, error) {
	w.calls = append(w.calls, "start")
	return browser.WorkerStatus{Running: true, BaseURL: "http://127.0.0.1:9101"}, nil
}

// Call 模拟调用 Worker API。
// ctx 为请求上下文，path 为 Worker 路径，payload 为请求体。
func (w *fakeWorker) Call(ctx context.Context, path string, payload any) (map[string]any, error) {
	w.calls = append(w.calls, path)
	if path == "/api/v1/page/list" {
		w.pageListCalls++
		if w.pageListCalls <= w.pageListEmptyBefore {
			return map[string]any{"data": map[string]any{"pages": []any{}}}, nil
		}
		return map[string]any{"data": map[string]any{"pages": []any{map[string]any{
			"page_id":    "0",
			"url":        "https://www.zhipin.com/web/chat/recommend",
			"is_default": true,
		}}}}, nil
	}
	if path == "/api/v1/page/extract-text" {
		position := strings.TrimSpace(w.currentPosition)
		if position == "" {
			position = "本地任务"
		}
		return map[string]any{"data": map[string]any{"text": position, "texts": []any{position}}}, nil
	}
	if path == "/api/v1/page/find-elements" {
		return map[string]any{"data": map[string]any{"items": []any{
			map[string]any{"index": 0, "text": "本地任务", "fields": map[string]any{"position_name": "本地任务"}},
			map[string]any{"index": 1, "text": "本地任务2", "fields": map[string]any{"position_name": "本地任务2"}},
		}}}, nil
	}
	if path == "/api/v1/page/list-click-by-index" {
		index := intFromMap(mapValue(payload), "index")
		if index == 1 {
			w.currentPosition = "本地任务2"
		} else {
			w.currentPosition = "本地任务"
		}
		return map[string]any{"data": map[string]any{"clicked": true}}, nil
	}
	if path == "/api/v1/boss/candidates/extract" {
		if w.extractErr != nil {
			return nil, w.extractErr
		}
		return map[string]any{
			"data": map[string]any{
				"candidates": []any{
					map[string]any{
						"id":             "boss_1",
						"candidate_name": "候选人A",
						"name":           "候选人A",
						"status":         "scanned",
					},
				},
			},
		}, nil
	}
	if path == "/api/v1/boss/candidates/greet" {
		return map[string]any{"data": map[string]any{"greeted": true}}, nil
	}
	if path == "/api/v1/boss/candidates/detail" {
		return map[string]any{"data": map[string]any{"detail_text": "本科 5年 销售管理经验"}}, nil
	}
	return map[string]any{"data": map[string]any{}}, nil
}

// blockingWorker 模拟会阻塞到 ctx 取消的 Worker。
type blockingWorker struct {
	extractStarted chan struct{}
	released       chan struct{}
}

// Start 模拟启动阻塞 Worker。
// ctx 为请求上下文。
func (w *blockingWorker) Start(ctx context.Context) (browser.WorkerStatus, error) {
	return browser.WorkerStatus{Running: true}, nil
}

// Call 模拟 Worker API，并在候选人提取时等待取消。
// ctx 为请求上下文，path 为 Worker 路径，payload 为请求体。
func (w *blockingWorker) Call(ctx context.Context, path string, payload any) (map[string]any, error) {
	if path == "/api/v1/page/list" {
		return map[string]any{"data": map[string]any{"pages": []any{map[string]any{
			"page_id":    "0",
			"url":        "https://www.zhipin.com/web/chat/recommend",
			"is_default": true,
		}}}}, nil
	}
	if path == "/api/v1/page/extract-text" {
		return map[string]any{"data": map[string]any{"text": "可停止任务", "texts": []any{"可停止任务"}}}, nil
	}
	if path == "/api/v1/boss/candidates/extract" {
		close(w.extractStarted)
		<-ctx.Done()
		close(w.released)
		return nil, ctx.Err()
	}
	return map[string]any{"data": map[string]any{}}, nil
}

// waitForTaskStatus 等待任务进入指定状态。
// t 为测试对象，db 为本地数据库，taskID 为任务 ID，status 为目标状态。
func waitForTaskStatus(t *testing.T, db *localdb.DB, taskID string, status string) localdb.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := db.GetTask(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == status {
			return task
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, err := db.GetTask(taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("等待任务状态超时，当前状态=%s，目标状态=%s", task.Status, status)
	return task
}

// speedUpPageEntryCheck 加快测试中的页面入口等待。
// t 为测试对象，测试结束后自动恢复默认等待配置。
func speedUpPageEntryCheck(t *testing.T) {
	t.Helper()
	oldAttempts := pageEntryCheckAttempts
	oldDelay := pageEntryCheckDelay
	oldCurrentAttempts := currentPositionCheckAttempts
	oldCurrentDelay := currentPositionCheckDelay
	pageEntryCheckAttempts = 10
	pageEntryCheckDelay = time.Millisecond
	currentPositionCheckAttempts = 10
	currentPositionCheckDelay = time.Millisecond
	t.Cleanup(func() {
		pageEntryCheckAttempts = oldAttempts
		pageEntryCheckDelay = oldDelay
		currentPositionCheckAttempts = oldCurrentAttempts
		currentPositionCheckDelay = oldCurrentDelay
	})
}

// openRunnerTestDB 创建任务运行器测试数据库。
// t 为测试对象。
func openRunnerTestDB(t *testing.T) *localdb.DB {
	t.Helper()
	db, err := localdb.Open(&config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newTestRunner 创建带临时目录的任务运行器。
// t 为测试对象，db 为测试数据库，worker 为模拟 Worker。
func newTestRunner(t *testing.T, db *localdb.DB, worker BrowserWorker) *Runner {
	t.Helper()
	root := t.TempDir()
	return New(db, worker, fakeOCR{}, root+"/profiles", root+"/downloads", root+"/screenshots", root+"/audio", "")
}
