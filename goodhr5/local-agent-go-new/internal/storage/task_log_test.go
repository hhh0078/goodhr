// Package storage 文件作用：验证任务启动前日志补充岗位、读取顺序和清空能力。
package storage

import (
	"context"
	"path/filepath"
	"testing"
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
		TaskType: "greeting", Status: "running",
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
}
