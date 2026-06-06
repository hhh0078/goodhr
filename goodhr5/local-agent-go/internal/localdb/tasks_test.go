// Package localdb 负责测试本地任务数据库能力。
package localdb

import (
	"encoding/json"
	"testing"

	"goodhr5/local-agent-go/internal/config"
)

// TestTaskLogCandidateFlow 验证任务、日志和候选人的基本读写流程。
func TestTaskLogCandidateFlow(t *testing.T) {
	db := openTestDB(t)
	task, err := db.CreateTask(map[string]any{
		"name":        "测试任务",
		"platform_id": "boss",
		"match_limit": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == "" || task.Status != "pending" {
		t.Fatalf("unexpected task: %+v", task)
	}
	updated, err := db.UpdateTaskStatus(task.ID, "running")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "running" {
		t.Fatalf("status = %s", updated.Status)
	}
	if _, err := db.AddTaskLog(task.ID, "info", "开始任务"); err != nil {
		t.Fatal(err)
	}
	logs, err := db.ListTaskLogs(task.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Message != "开始任务" {
		t.Fatalf("logs = %+v", logs)
	}
	candidate, err := db.SaveCandidate(task.ID, map[string]any{"name": "候选人A", "status": "scanned"})
	if err != nil {
		t.Fatal(err)
	}
	if candidate["id"] == "" {
		t.Fatalf("candidate missing id: %+v", candidate)
	}
	candidates, err := db.ListCandidates(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates len = %d", len(candidates))
	}
}

// TestPositionAISettingsRecordsFlow 验证岗位模板、AI 配置、设置和本地记录读写流程。
func TestPositionAISettingsRecordsFlow(t *testing.T) {
	db := openTestDB(t)
	position, err := db.SavePosition(map[string]any{
		"platform_id":      "boss",
		"name":             "销售顾问",
		"keywords":         []any{"本科", "3年"},
		"exclude_keywords": []string{"外包"},
		"description":      "负责客户跟进",
		"greet_message":    "你好，方便聊聊吗",
		"is_and_mode":      true,
		"common_config":    map[string]any{"match_score": json.Number("70")},
		"ai_config":        map[string]any{"enabled": true},
		"keyword_config":   map[string]any{"strict": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if position.ID == "" || position.Name != "销售顾问" || len(position.Keywords) != 2 {
		t.Fatalf("position = %+v", position)
	}
	positions, err := db.ListPositions()
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 1 {
		t.Fatalf("positions len = %d", len(positions))
	}

	aiConfig, err := db.SaveAIConfig(map[string]any{
		"provider":    "openai",
		"base_url":    "http://127.0.0.1:8000",
		"api_key":     "test-key",
		"model":       "gpt-test",
		"temperature": json.Number("0.3"),
		"timeout":     json.Number("60"),
		"extra":       map[string]any{"reasoning": "low"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if aiConfig.BaseURL == "" || aiConfig.Timeout != 60 {
		t.Fatalf("aiConfig = %+v", aiConfig)
	}
	loadedAIConfig, err := db.GetAIConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loadedAIConfig.APIKey != "test-key" {
		t.Fatalf("loadedAIConfig = %+v", loadedAIConfig)
	}

	settings, err := db.SaveSettings(map[string]any{"browser_download_dir": "/tmp/goodhr-downloads"})
	if err != nil {
		t.Fatal(err)
	}
	if settings["browser_download_dir"] != "/tmp/goodhr-downloads" {
		t.Fatalf("settings = %+v", settings)
	}

	download, err := db.SaveDownload(map[string]any{
		"task_id":   "task-1",
		"url":       "https://example.com/a.pdf",
		"file_path": "/tmp/a.pdf",
		"file_name": "a.pdf",
		"mime_type": "application/pdf",
		"size":      json.Number("12"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if download.FileName != "a.pdf" || download.Size != 12 {
		t.Fatalf("download = %+v", download)
	}
	downloads, err := db.ListDownloads("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("downloads len = %d", len(downloads))
	}

	screenshot, err := db.SaveScreenshot(map[string]any{
		"task_id":   "task-1",
		"file_path": "/tmp/a.png",
		"label":     "详情页",
		"width":     json.Number("100"),
		"height":    json.Number("200"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if screenshot.Width != 100 || screenshot.Height != 200 {
		t.Fatalf("screenshot = %+v", screenshot)
	}
	screenshots, err := db.ListScreenshots("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(screenshots) != 1 {
		t.Fatalf("screenshots len = %d", len(screenshots))
	}

	if err := db.DeletePosition(position.ID); err != nil {
		t.Fatal(err)
	}
	positions, err = db.ListPositions()
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 0 {
		t.Fatalf("positions after delete len = %d", len(positions))
	}
}

// openTestDB 创建测试数据库。
// t 为测试对象。
func openTestDB(t *testing.T) *DB {
	t.Helper()
	cfg := &config.Config{DataDir: t.TempDir()}
	db, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
