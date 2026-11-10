// Package lifecycle 本文件验证旧日志一次性清理的回滚、计费精度、任务边界及原业务字段的去除。
package lifecycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLegacyReplyLogCleanup 核对非自动回复、不明账单和其它岗位不动，清理中断可恢复并只执行一次。
func TestLegacyReplyLogCleanup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "logs.db")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, task := range []storage.TaskRun{
		{TaskID: "reply", PositionID: "p", PlatformID: "liepin", TaskType: "auto_reply", Status: "failed", CurrentStep: "候选人姓名", Summary: "候选人原文", ErrorCode: "KNOWN_ERR", ErrorMessage: "旧消息", StartedAt: time.Now()},
		{TaskID: "regular", PositionID: "p", PlatformID: "boss", TaskType: "greeting", Status: "failed", Summary: "普通日志", ErrorMessage: "普通错误", StartedAt: time.Now()},
	} {
		if err := store.SaveTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	seed := []storage.TaskLog{
		{TaskID: "reply", Flow: "候选人姓名", Step: "候选人姓名", Status: "failed", Level: "error", Category: "候选人姓名", Message: "私密正文", ErrorMessage: "候选人姓名", DurationMS: 43, Input: json.RawMessage(`{"link":"私密内容"}`), Output: json.RawMessage(`{"error_code":"ACTION_TIMEOUT","error_message":"私密内容"}`)},
		{TaskID: "reply", Flow: "ai", Step: "ai_billing", Output: json.RawMessage(`{"status":"unavailable","cost_units":0,"model":"候选人姓名","extra":"私密信息"}`)},
		{PositionID: "p", Flow: "ai", Step: "ai_billing", Message: "AI 推荐报告，候选人“测试候选人”，真实扣费", Output: json.RawMessage(`{"status":"charged","cost_units":23,"model":"test-model","hidden":"私密信息"}`)},
		{TaskID: "regular", Flow: "auto_reply", Step: "analysis.auto_reply.tool", Message: "普通任务内容"},
		{PositionID: "p", Flow: "ai", Step: "ai_billing", Message: "别的流程，候选人“测试候选人”", Output: json.RawMessage(`{"status":"charged","cost_units":23}`)},
		{PositionID: "p", Flow: "ai", Step: "ai_billing", Message: "AI 推荐报告，候选人“测试候选人”，缺少回执", Output: json.RawMessage(`{"status":"charged"}`)},
		{PositionID: "p", Flow: "analysis", Step: "analysis.auto_reply.tool", Message: "旧工具原文"},
		{PositionID: "other", Flow: "ai", Step: "ai_billing", Message: "AI 推荐报告，候选人“测试候选人”，无岗位凭据", Output: json.RawMessage(`{"status":"unavailable"}`)},
	}
	var ids []int64
	for _, l := range seed {
		got, e := store.SaveTaskLog(ctx, l)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, got.ID)
	}
	_, err = db.Exec("CREATE TRIGGER stop_cleanup BEFORE UPDATE ON task_logs WHEN OLD.id = " + fmt.Sprint(ids[1]) + " BEGIN SELECT RAISE(ABORT,'test rollback'); END")
	if err != nil {
		t.Fatal(err)
	}
	if e := store.RedactLegacyReplyDiagnostics(ctx, RedactLegacyReplyLog); e == nil {
		t.Fatal("注入的失败应回滚")
	}
	var v string
	if e := db.QueryRow("SELECT message FROM task_logs WHERE id=?", ids[0]).Scan(&v); e != nil || v != "私密正文" {
		t.Fatal("部分改动未回滚", e)
	}
	if _, e := db.Exec("DROP TRIGGER stop_cleanup"); e != nil {
		t.Fatal(e)
	}
	if e := store.RedactLegacyReplyDiagnostics(ctx, RedactLegacyReplyLog); e != nil {
		t.Fatal(e)
	}
	for i, id := range ids {
		var flow, step, status, level, cat, msg, input, output, errMsg string
		var dur int
		if e := db.QueryRow("SELECT flow,step,status,level,category,message,input_json,output_json,error_message,duration_ms FROM task_logs WHERE id=?", id).Scan(&flow, &step, &status, &level, &cat, &msg, &input, &output, &errMsg, &dur); e != nil {
			t.Fatal(e)
		}
		switch i {
		case 0:
			if dur != 43 || status != "failed" || input != "{}" || output != `{"error_code":"ACTION_TIMEOUT"}` || strings.Contains(flow+step+level+cat+msg+errMsg, "候选人") || strings.Contains(msg, "私密") {
				t.Fatal("错误步骤的统计、错误码或隐私处理不正确")
			}
		case 1:
			if strings.Contains(output, "cost_units") || !strings.Contains(output, "unavailable") || strings.Contains(output, "候选人") {
				t.Fatal("未知账单不能变成零元或保留私密信息")
			}
		case 2:
			if !strings.Contains(msg, "0.0023") || strings.Contains(msg, "候选人") || strings.Contains(output, "hidden") {
				t.Fatal("空任务号的已核实推荐账单未保留")
			}
		case 6:
			if strings.Contains(msg, "旧工具原文") {
				t.Fatal("分析步骤旧原文遗留")
			}
		default:
			if msg != seed[i].Message {
				t.Fatal("改变了无证据或其它任务的记录", i)
			}
		}
	}
	task, e := store.Task(ctx, "reply")
	if e != nil || task.CurrentStep != "legacy_step" || task.ErrorCode != "KNOWN_ERR" || task.ErrorMessage != "" || strings.Contains(task.Summary, "候选人") {
		t.Fatal("任务自由文本和技术字段处理错误")
	}
	other, e := store.Task(ctx, "regular")
	if e != nil || other.Summary != "普通日志" || other.ErrorMessage != "普通错误" {
		t.Fatal("改变了普通任务")
	}
	if e := store.RedactLegacyReplyDiagnostics(ctx, func(x storage.TaskLog) (storage.TaskLog, bool) { t.Fatal("版本清理重复执行"); return x, false }); e != nil {
		t.Fatal(e)
	}
}
