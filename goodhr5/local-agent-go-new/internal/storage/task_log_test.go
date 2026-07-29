// Package storage 文件作用：验证任务启动前日志补充岗位、读取顺序和清空能力。
package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTaskLogsAttachToSavedTask 验证启动前日志会在任务保存后补充岗位编号。
func TestTaskLogsAttachToSavedTask(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err = store.SaveTaskLog(ctx, TaskLog{TaskID: "task-1", Step: "preflight", Status: "start"}); err != nil {
		t.Fatalf("保存启动前日志失败：%v", err)
	}
	if err = store.SaveTask(ctx, TaskRun{
		TaskID: "task-1", PositionID: "position-1", PlatformID: "boss",
		TaskType: "greeting", Status: "failed", ErrorCode: "TASK_FLOW_FAILED",
		ErrorMessage: "旧任务错误",
	}); err != nil {
		t.Fatalf("保存任务失败：%v", err)
	}
	logs, err := store.ListPositionLogs(ctx, "position-1", 100)
	if err != nil || len(logs) != 1 || logs[0].PositionID != "position-1" {
		t.Fatalf("岗位日志不符合预期：logs=%+v err=%v", logs, err)
	}
	if err = store.ClearPositionLogs(ctx, "position-1"); err != nil {
		t.Fatalf("清空岗位日志失败：%v", err)
	}
	logs, err = store.ListPositionLogs(ctx, "position-1", 100)
	if err != nil || len(logs) != 0 {
		t.Fatalf("清空后仍有岗位日志：logs=%+v err=%v", logs, err)
	}
	task, err := store.Task(ctx, "task-1")
	if err != nil {
		t.Fatalf("读取清理后的任务失败：%v", err)
	}
	if task.Status != "failed" || task.ErrorCode != "" || task.ErrorMessage != "" {
		t.Fatalf("清空日志后任务状态或历史错误不符合预期：%+v", task)
	}
}

// TestOpenRecoversInterruptedTasks 验证程序重启会把残留 running 任务改成明确失败状态。
func TestOpenRecoversInterruptedTasks(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "agent.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	if err = store.SaveTask(context.Background(), TaskRun{
		TaskID: "task-interrupted", PositionID: "position-1", PlatformID: "boss",
		TaskType: "greeting", Status: "running",
	}); err != nil {
		t.Fatalf("保存运行中任务失败：%v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatalf("关闭测试数据库失败：%v", err)
	}
	store, err = Open(databasePath)
	if err != nil {
		t.Fatalf("重新打开测试数据库失败：%v", err)
	}
	defer store.Close()
	task, err := store.Task(context.Background(), "task-interrupted")
	if err != nil {
		t.Fatalf("读取恢复任务失败：%v", err)
	}
	if task.Status != "failed" || task.ErrorCode != "AGENT_RESTARTED" || task.FinishedAt.IsZero() {
		t.Fatalf("异常任务没有正确收尾：%+v", task)
	}
}

// TestCleanupExpired 验证五类本地摘要会按统一保留时间清理。
func TestCleanupExpired(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer store.Close()
	old := "2025-01-01T00:00:00Z"
	statements := []string{
		`INSERT INTO task_runs(task_id, position_id, platform_id, task_type, status, current_step, started_at, updated_at) VALUES ('old-task', 'p', 'boss', 'greeting', 'completed', '', ?, ?)`,
		`INSERT INTO candidate_records(task_id, fingerprint, platform_id, action, result, created_at) VALUES ('old-task', 'f', 'boss', 'greet', 'success', ?)`,
		`INSERT INTO conversation_records(task_id, conversation_key, platform_id, reply_hash, result, created_at) VALUES ('old-task', 'c', 'boss', 'h', 'success', ?)`,
		`INSERT INTO download_records(id, status, created_at, updated_at) VALUES ('old-download', 'saved', ?, ?)`,
		`INSERT INTO task_logs(created_at) VALUES (?)`,
	}
	for _, statement := range statements {
		arguments := []any{old}
		if strings.Count(statement, "?") == 2 {
			arguments = append(arguments, old)
		}
		if _, err = store.db.Exec(statement, arguments...); err != nil {
			t.Fatalf("准备过期数据失败：%v", err)
		}
	}
	deleted, err := store.CleanupExpired(context.Background(), time.Now().UTC().Add(-24*time.Hour))
	if err != nil || deleted != int64(len(statements)) {
		t.Fatalf("清理过期数据失败：deleted=%d err=%v", deleted, err)
	}
}

// TestTaskLogsKeepLatestThousand 验证每个岗位只保留最近 1000 条统一日志。
func TestTaskLogsKeepLatestThousand(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if err = store.SaveTask(ctx, TaskRun{
		TaskID: "task-logs", PositionID: "position-logs", PlatformID: "boss",
		TaskType: "greeting", Status: "running",
	}); err != nil {
		t.Fatalf("保存任务失败：%v", err)
	}
	for index := 0; index < maxPositionTaskLogs; index++ {
		if _, err = store.db.Exec(
			`INSERT INTO task_logs(task_id, position_id, message, created_at) VALUES (?, ?, ?, ?)`,
			"task-logs", "position-logs", fmt.Sprintf("旧日志-%d", index), time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			t.Fatalf("准备岗位日志失败：%v", err)
		}
	}
	if _, err = store.SaveTaskLog(ctx, TaskLog{
		TaskID: "task-logs", PositionID: "position-logs", Message: "最新日志",
	}); err != nil {
		t.Fatalf("保存最新岗位日志失败：%v", err)
	}
	logs, err := store.ListPositionLogs(ctx, "position-logs", maxPositionTaskLogs+100)
	if err != nil {
		t.Fatalf("读取岗位日志失败：%v", err)
	}
	if len(logs) != maxPositionTaskLogs || logs[0].Message == "旧日志-0" || logs[len(logs)-1].Message != "最新日志" {
		t.Fatalf("岗位日志没有保留最近 1000 条：count=%d first=%s last=%s", len(logs), logs[0].Message, logs[len(logs)-1].Message)
	}
}
