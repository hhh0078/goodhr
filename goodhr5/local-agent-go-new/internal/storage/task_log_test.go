// Package storage 文件作用：验证任务启动前日志补充岗位、读取顺序和清空能力。
package storage

import (
	"context"
	"encoding/json"
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
	store, err = Open(databasePath, "Browser Worker 意外退出，退出码 2")
	if err != nil {
		t.Fatalf("重新打开测试数据库失败：%v", err)
	}
	defer store.Close()
	task, err := store.Task(context.Background(), "task-interrupted")
	if err != nil {
		t.Fatalf("读取恢复任务失败：%v", err)
	}
	if task.Status != "failed" || task.ErrorCode != "AGENT_RESTARTED" || task.FinishedAt.IsZero() || !strings.Contains(task.ErrorMessage, "退出码 2") {
		t.Fatalf("异常任务没有正确收尾：%+v", task)
	}
	logs, err := store.ListPositionLogs(context.Background(), "position-1", 100)
	if err != nil || len(logs) == 0 || !strings.Contains(logs[len(logs)-1].ErrorMessage, "退出码 2") {
		t.Fatalf("异常退出原因没有保存到岗位：logs=%+v err=%v", logs, err)
	}
}

// TestLegacyTaskLogResetRunsOnce 验证升级时删除旧岗位流水，随后生成的新日志不会在重启时再次丢失。
func TestLegacyTaskLogResetRunsOnce(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.SaveTaskLog(ctx, TaskLog{PositionID: "position-1", Message: "旧技术流水"}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPositionLogUploadPending(ctx, "position-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = '005_reset_legacy_task_logs.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	logs, err := store.ListPositionLogs(ctx, "position-1", 100)
	if err != nil || len(logs) != 0 {
		t.Fatalf("旧岗位日志未清除：logs=%+v err=%v", logs, err)
	}
	pending, err := store.PendingPositionLogUploads(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("旧补传队列未清除：pending=%+v err=%v", pending, err)
	}
	if _, err := store.SaveTaskLog(ctx, TaskLog{PositionID: "position-1", Message: "新运行记录"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	logs, err = store.ListPositionLogs(ctx, "position-1", 100)
	if err != nil || len(logs) != 1 || logs[0].Message != "新运行记录" {
		t.Fatalf("新岗位日志被重复清除：logs=%+v err=%v", logs, err)
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

// TestLatestTaskIncludesCandidateCounts 验证最近任务会从现有候选人摘要返回本次统计。
func TestLatestTaskIncludesCandidateCounts(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if err = store.SaveTask(ctx, TaskRun{
		TaskID: "task-counts", PositionID: "position-counts", PlatformID: "zhaopin",
		TaskType: "greeting", Status: "completed",
	}); err != nil {
		t.Fatalf("保存任务失败：%v", err)
	}
	records := []CandidateRecord{
		{TaskID: "task-counts", Fingerprint: "candidate-1", PlatformID: "zhaopin", Action: "detail", Result: "success"},
		{TaskID: "task-counts", Fingerprint: "candidate-1", PlatformID: "zhaopin", Action: "greet", Result: "success"},
		{TaskID: "task-counts", Fingerprint: "candidate-2", PlatformID: "zhaopin", Action: "decision", Result: "skipped"},
		{TaskID: "task-counts", Fingerprint: "candidate-3", PlatformID: "zhaopin", Action: "detail", Result: "failed"},
	}
	for _, record := range records {
		if err = store.SaveCandidate(ctx, record); err != nil {
			t.Fatalf("保存候选人摘要失败：%v", err)
		}
	}
	task, err := store.LatestTaskForPosition(ctx, "position-counts")
	if err != nil {
		t.Fatalf("读取最近任务失败：%v", err)
	}
	if task.ScannedCount != 3 || task.GreetedCount != 1 || task.SkippedCount != 1 {
		t.Fatalf("本次任务统计不正确：%+v", task)
	}
}

// TestHistoricalMouseLogsFilterBeforeLimit 验证历史鼠标流水不挤掉关键记录，读取筛选不会删除数据库原始行。
func TestHistoricalMouseLogsFilterBeforeLimit(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	const position = "historical-mouse-position"
	for _, important := range []TaskLog{
		{Flow: "lifecycle", Step: "agent_start", Status: "start", Message: "岗位已开始"},
		{Flow: "candidate", Step: "open_detail", Status: "running", Message: "开始读取详情"},
		{Flow: "browser_worker", Step: "mouse_wheel", Status: "failed", Level: "error", IsError: true, ErrorMessage: "输入等待超时", Message: "浏览器输入失败"},
		{Flow: "browser_worker", Step: "mouse_move", Status: "recovered", Level: "info", Message: "窗口恢复完成"},
	} {
		important.PositionID = position
		if _, err := store.SaveTaskLog(ctx, important); err != nil {
			t.Fatal(err)
		}
		for _, step := range []string{"mouse_move", "mouse_down", "mouse_up", "mouse_wheel"} {
			for _, status := range []string{"start", "success"} {
				for _, level := range []string{"info", "debug"} {
					if _, err := store.SaveTaskLog(ctx, TaskLog{
						PositionID: position, Flow: "browser_worker", Step: step, Status: status, Level: level,
						Message: "正常鼠标流水", Output: json.RawMessage(fmt.Sprintf(`{"status":%q,"duration_ms":20,"page_state_after":{"page_closed":false,"recent_page_failures":[{"type":"page_error"}],"recent_page_events":[{"status":500}]}}`, status)),
					}); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	// 直接模拟旧数据库中的坏 JSON，避免保存接口先将它修正成空对象。
	if _, err := store.db.ExecContext(ctx, `INSERT INTO task_logs(position_id, flow, step, status, level, message, output_json, created_at) VALUES (?, 'browser_worker', 'mouse_move', 'success', 'info', '历史输出无法解析', '{broken', ?)`, position, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_logs WHERE position_id = ?`, position).Scan(&before); err != nil {
		t.Fatal(err)
	}
	logs, err := store.ListPositionLogs(ctx, position, 3)
	if err != nil || len(logs) != 3 || logs[0].Message != "浏览器输入失败" || logs[1].Message != "窗口恢复完成" || logs[2].Message != "历史输出无法解析" {
		t.Fatalf("关键或异常记录被鼠标流水挤掉，或顺序不正确：%+v err=%v", logs, err)
	}
	logs, err = store.ListPositionLogs(ctx, position, 1000)
	if err != nil || len(logs) != 5 || logs[0].Message != "岗位已开始" || logs[1].Message != "开始读取详情" {
		t.Fatalf("历史混合日志筛选不正确：%+v err=%v", logs, err)
	}
	var after int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_logs WHERE position_id = ?`, position).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != 69 || after != before {
		t.Fatalf("读取筛选删除了原始日志：before=%d after=%d", before, after)
	}
}

// TestRoutineMouseDiagnosticKeepsAbnormalEvidence 验证历史正常记录可以精简，异常、恢复、无效输出和未来新增的错误字段始终保留。
func TestRoutineMouseDiagnosticKeepsAbnormalEvidence(t *testing.T) {
	for _, sample := range []struct {
		name         string
		output       string
		flow         string
		step         string
		status       string
		level        string
		category     string
		isError      bool
		errorMessage string
		message      string
		hidden       bool
	}{
		{name: "normal_empty_object", output: `{}`, hidden: true},
		{name: "normal_future_sampling", output: `{"status":"success","new_sampling":{"point":{"x":20}}}`, hidden: true},
		{name: "normal_without_level", output: `{}`, level: " ", hidden: true},
		{name: "normal_start_pending", output: `{"status":"start","input_wait":{"status":"pending"}}`, status: "start", hidden: true},
		{name: "historical_page_failures", output: `{"input_wait":{"status":"completed"},"page_state_after":{"page_closed":false,"recent_page_failures":[{"error_message":"过去页面错误","is_error":true}],"recent_page_events":[{"status":500}]} }`, hidden: true},
		{name: "empty_error_fields", output: `{"error_code":"","original_error":"","is_error":false,"native_mouse":{"native_error_code":"","recovery_required":false}}`, hidden: true},
		{name: "wrong_flow", flow: "candidate", output: `{}`},
		{name: "unknown_step", step: "selector_probe", output: `{}`},
		{name: "failed_status", status: "failed", output: `{}`},
		{name: "recovered_status", status: "recovered", output: `{}`},
		{name: "warning_level", level: "warn", output: `{}`},
		{name: "error_level", level: "error", output: `{}`},
		{name: "unknown_level", level: "trace", output: `{}`},
		{name: "error_category", category: "错误", output: `{}`},
		{name: "error_flag", isError: true, output: `{}`},
		{name: "error_message", errorMessage: "结果不确定", output: `{}`},
		{name: "recovery_message", message: "移动鼠标恢复完成", output: `{}`},
		{name: "invalid_json", output: `{broken`},
		{name: "absent_output", output: ""},
		{name: "null_output", output: `null`},
		{name: "array_output", output: `[]`},
		{name: "scalar_output", output: `true`},
		{name: "nested_error_code", output: `{"new_sampling":{"error_code":"WORKER_UNRESPONSIVE"}}`},
		{name: "nested_original_error", output: `{"new_sampling":[{"original_error":"原生调用失败"}]}`},
		{name: "nested_error_message", output: `{"new_sampling":{"error_message":"结果未知"}}`},
		{name: "nested_error_flag", output: `{"new_sampling":{"is_error":true}}`},
		{name: "unknown_output_status", output: `{"status":"unknown"}`},
		{name: "nested_warning_level", output: `{"new_sampling":{"level":"warning"}}`},
		{name: "failed_input_wait", output: `{"input_wait":{"status":"failed"}}`},
		{name: "unknown_input_wait", output: `{"input_wait":{"status":"unknown"}}`},
		{name: "pending_success_input_wait", output: `{"input_wait":{"status":"pending"}}`},
		{name: "missing_input_wait_status", output: `{"input_wait":{}}`},
		{name: "invalid_input_wait", output: `{"input_wait":null}`},
		{name: "native_mouse_error", output: `{"native_mouse":{"native_error_code":"GOODHR_MOUSE_ACK_TIMEOUT"}}`},
		{name: "native_mouse_recovery", output: `{"native_mouse":{"recovery_required":true}}`},
		{name: "invalid_native_mouse", output: `{"native_mouse":null}`},
		{name: "closed_page", output: `{"page_state_after":{"page_closed":true,"recent_page_failures":[]}}`},
		{name: "invalid_closed_flag", output: `{"page_state_after":{"page_closed":[]}}`},
	} {
		t.Run(sample.name, func(t *testing.T) {
			item := TaskLog{Flow: "browser_worker", Step: "mouse_wheel", Status: "success", Level: "info", Output: json.RawMessage(sample.output), Category: sample.category, IsError: sample.isError, ErrorMessage: sample.errorMessage, Message: sample.message}
			if sample.flow != "" {
				item.Flow = sample.flow
			}
			if sample.step != "" {
				item.Step = sample.step
			}
			if sample.status != "" {
				item.Status = sample.status
			}
			if sample.level != "" {
				item.Level = sample.level
			}
			if hidden := IsRoutineMouseDiagnostic(item); hidden != sample.hidden {
				t.Fatalf("正常鼠标可见性判断错误：hidden=%v expected=%v item=%+v", hidden, sample.hidden, item)
			}
		})
	}
}
