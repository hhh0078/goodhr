// Package taskrunner 负责测试 Go 本地任务运行器。
package taskrunner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/config"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/ocr"
)

// TestRunnerStartStop 验证任务启动会校验会员、读取平台配置、扫描候选人并更新状态。
func TestRunnerStartStop(t *testing.T) {
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
					{"config_key": "platform.boss", "config_value": `{"id":"boss","name":"Boss直聘","pages":[{"url":"https://www.zhipin.com/web/chat/recommend"}]}`},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	task, err := db.CreateTask(map[string]any{"name": "本地任务", "platform_id": "boss"})
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
	result, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1"})
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

	task2, err := db.CreateTask(map[string]any{"name": "本地任务2", "platform_id": "boss", "match_limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Start(t.Context(), task2.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", EnableGreet: true}); err != nil {
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
					{"config_key": "platform.boss", "config_value": `{"id":"boss","pages":[{"url":"https://www.zhipin.com/web/chat/recommend"}]}`},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	task, err := db.CreateTask(map[string]any{"name": "可停止任务", "platform_id": "boss", "mode": "keyword"})
	if err != nil {
		t.Fatal(err)
	}
	worker := &blockingWorker{extractStarted: make(chan struct{}), released: make(chan struct{})}
	runner := newTestRunner(t, db, worker)
	if _, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1"}); err != nil {
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

// fakeWorker 模拟浏览器 Worker。
type fakeWorker struct {
	calls []string
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
	if path == "/api/v1/boss/candidates/extract" {
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
	return New(db, worker, fakeOCR{}, root+"/profiles", root+"/downloads", root+"/screenshots")
}
