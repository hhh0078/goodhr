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
	runner := New(db, worker)
	result, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result["running"] != false || runner.IsRunning(task.ID) {
		t.Fatalf("result = %+v", result)
	}
	updated, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "completed" || updated.ScannedCount != 1 {
		t.Fatalf("status = %s", updated.Status)
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
	runner := New(db, worker)
	done := make(chan error, 1)
	go func() {
		_, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1"})
		done <- err
	}()
	select {
	case <-worker.extractStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("等待 Worker 提取开始超时")
	}
	if _, err := runner.Stop(task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("停止任务后 Start 未返回")
	}
	stopped, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != "stopped" {
		t.Fatalf("status = %s", stopped.Status)
	}
}

// fakeWorker 模拟浏览器 Worker。
type fakeWorker struct {
	calls []string
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
