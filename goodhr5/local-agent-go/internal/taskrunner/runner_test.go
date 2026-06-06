// Package taskrunner 负责测试 Go 本地任务运行器。
package taskrunner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"goodhr5/local-agent-go/internal/config"
	"goodhr5/local-agent-go/internal/localdb"
)

// TestRunnerStartStop 验证任务启动会校验会员、读取平台配置并更新状态。
func TestRunnerStartStop(t *testing.T) {
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
					{"config_key": "platform.boss", "config_value": `{"id":"boss","name":"Boss直聘"}`},
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
	runner := New(db)
	result, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result["running"] != true || !runner.IsRunning(task.ID) {
		t.Fatalf("result = %+v", result)
	}
	updated, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "running" {
		t.Fatalf("status = %s", updated.Status)
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
