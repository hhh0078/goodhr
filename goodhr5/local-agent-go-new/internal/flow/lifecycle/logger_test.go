// Package lifecycle 文件作用：验证用户可见任务日志统一使用中文且不会泄露 Worker 内部步骤。
package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestTaskLogMessageUsesChinese 验证流程错误日志使用中文步骤名称。
func TestTaskLogMessageUsesChinese(t *testing.T) {
	message := taskLogMessage("select_position", "failed", errors.New("没有找到岗位"))
	if message != "选择岗位：没处理成功，没有找到岗位" {
		t.Fatalf("中文流程日志不正确：%s", message)
	}
}

// TestCandidateTimeoutIsPersistedAsError 验证候选人总超时以错误级别保存姓名、明确原因和完整处理耗时。
func TestCandidateTimeoutIsPersistedAsError(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTask(ctx, storage.TaskRun{TaskID: "candidate-timeout", PositionID: "timeout-position", Status: "running", TaskType: "greeting"}); err != nil {
		t.Fatal(err)
	}
	logger := NewTaskLogger(store, nil)
	logger.CandidateActivity("candidate-timeout", "合成候选人", "正在判断是否适合打招呼", "running", nil)
	logger.Step("candidate-timeout", "greeting", "candidate_timeout", "failed", time.Now().Add(-5*time.Minute), errors.New("候选人处理超时（已达到5分钟上限）"))
	logs, err := store.ListPositionLogs(ctx, "timeout-position", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 {
		t.Fatal("候选人超时日志没有落库")
	}
	last := logs[len(logs)-1]
	if !last.IsError || last.Level != "error" || last.Step != "candidate_timeout" || last.DurationMS < 300_000 ||
		!strings.Contains(last.Message, "合成候选人") || !strings.Contains(last.ErrorMessage, "候选人处理超时") {
		t.Fatalf("候选人超时未保存为明确错误：%+v", last)
	}
}

// TestUploadPositionLogsKeepsCandidateName 验证分享日志仍能定位到发生动作的候选人。
func TestUploadPositionLogsKeepsCandidateName(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.SaveTaskLog(ctx, storage.TaskLog{
		PositionID: "position-readable", Flow: "candidate", Step: "正在点击打招呼按钮",
		Category: "运行", Status: "running", Message: "当前王女士，正在点击打招呼按钮",
	}); err != nil {
		t.Fatal(err)
	}
	var captured struct {
		Logs []cloud.PositionDiagnosticLog `json:"logs"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.Path != "/api/positions/position-readable/logs" {
			t.Errorf("上传接口不正确：%s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("解析上传日志失败：%v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	runner := &Runner{store: store, cloud: cloud.New(server.URL)}
	if err := runner.UploadPositionLogs(ctx, "test-token", "position-readable"); err != nil {
		t.Fatal(err)
	}
	if len(captured.Logs) != 1 || !strings.Contains(captured.Logs[0].Message, "王女士") {
		t.Fatalf("上传时丢失候选人称呼：%+v", captured.Logs)
	}
}

// TestTaskLoggerCandidateTimeline 验证岗位日志保留候选人动作和详细错误，且不被原子操作成功记录淹没。
func TestTaskLoggerCandidateTimeline(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTask(ctx, storage.TaskRun{
		TaskID: "task-readable", PositionID: "position-readable", Status: "running", TaskType: "greeting",
	}); err != nil {
		t.Fatal(err)
	}
	logger := NewTaskLogger(store, nil)
	logger.Step("task-readable", "lifecycle", "agent_start", "start", time.Now(), nil)
	logger.Step("task-readable", "platform", "find_candidates", "success", time.Now(), nil)
	logger.Progress("task-readable", "OCR 已识别截图 1000 字")
	logger.CandidateActivity("task-readable", "王女士", "正在点击打招呼按钮", "running", nil)
	logger.WorkerLine([]byte(`{"trace_id":"task-readable","action":"element.click","step":"click","status":"success","level":"info","target_description":"打招呼按钮"}`))
	logger.WorkerLine([]byte(`{"trace_id":"task-readable","action":"element.click","step":"click","status":"failed","level":"error","target_description":"打招呼按钮","error_code":"ELEMENT_NOT_FOUND","error_message":"按钮没有找到","input":{"candidates":["打招呼"]},"output":{"poll_attempts":4}}`))
	logs, err := store.ListPositionLogs(ctx, "position-readable", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 {
		t.Fatalf("应只保留启动、候选人动作和原子操作错误，实际 %d 条：%+v", len(logs), logs)
	}
	if !strings.Contains(logs[1].Message, "王女士") || !strings.Contains(logs[1].Message, "打招呼按钮") {
		t.Fatalf("候选人动作不清楚：%s", logs[1].Message)
	}
	if !logs[2].IsError || !strings.Contains(logs[2].Message, "王女士") {
		t.Fatalf("原子操作错误没有归属候选人：%+v", logs[2])
	}
	var input, output map[string]any
	if err := json.Unmarshal(logs[2].Input, &input); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(logs[2].Output, &output); err != nil {
		t.Fatal(err)
	}
	if _, ok := input["candidates"]; !ok || output["error_code"] != "ELEMENT_NOT_FOUND" || output["poll_attempts"] != float64(4) {
		t.Fatalf("详细排查字段丢失，入参=%v，出参=%v", input, output)
	}
	logger.ClearCandidate("task-readable")
	if got := logger.withCandidate("task-readable", "下一步失败"); got != "下一步失败" {
		t.Fatalf("候选人上下文未清空：%s", got)
	}
	logger.Step("task-readable", "lifecycle", "agent_shutdown", "stopped", time.Now(), errors.New("收到系统退出信号"))
	logs, err = store.ListPositionLogs(ctx, "position-readable", 1000)
	if err != nil || len(logs) != 4 || !strings.Contains(logs[3].Message, "收到系统退出信号") || logs[3].IsError || logs[3].ErrorMessage != "" {
		t.Fatalf("正常退出原因没有正确记录：logs=%+v err=%v", logs, err)
	}
}

// TestWorkerLogMessageUsesChinese 验证 Worker 错误日志不展示英文 action 和稳定错误码。
func TestWorkerLogMessageUsesChinese(t *testing.T) {
	message := workerLogMessage(workerLogLine{
		Action: "element.click", Status: "failed", Level: "error",
		TargetDescription: "候选人详情", ErrorCode: "ELEMENT_NOT_FOUND",
		ErrorMessage: "候选人详情暂时没找到", PollAttempts: 20,
	})
	if strings.Contains(message, "element.click") || strings.Contains(message, "ELEMENT_NOT_FOUND") {
		t.Fatalf("用户日志仍包含内部英文：%s", message)
	}
	if !strings.Contains(message, "每 300 毫秒找了 20 次") {
		t.Fatalf("错误日志缺少查找次数：%s", message)
	}
}

// TestTaskLoggerKeepsStructuredAnalysis 验证普通步骤日志不会覆盖悬浮窗结构化分析结果。
func TestTaskLoggerKeepsStructuredAnalysis(t *testing.T) {
	logger := NewTaskLogger(nil, nil)
	score := 86.5
	accepted := true
	logger.ReportAnalysis("task-analysis", shared.AnalysisStatus{
		Kind: "ai", Phase: "result", CandidateName: "张三",
		Score: &score, Accepted: &accepted, Reason: "经历比较匹配",
	})
	logger.Step("task-analysis", "browser_worker", "click", "start", time.Now(), nil)
	status := logger.AnalysisStatus("task-analysis")
	if status == nil || status.Score == nil || *status.Score != score || status.Reason != "经历比较匹配" {
		t.Fatalf("结构化分析结果被普通步骤覆盖：%+v", status)
	}
}

// TestTaskLoggerHoldsTerminalResult 验证低分候选人的最终原因不会被下一位候选人马上覆盖。
func TestTaskLoggerHoldsTerminalResult(t *testing.T) {
	logger := NewTaskLogger(nil, nil)
	score := 52.0
	accepted := false
	logger.ReportAnalysis("task-analysis", shared.AnalysisStatus{
		Kind: "ai", Phase: "result", Stage: "final", Terminal: true,
		CandidateName: "张三", Score: &score, Accepted: &accepted, Reason: "经验暂时不匹配",
	})
	logger.ReportAnalysis("task-analysis", shared.AnalysisStatus{
		Kind: "ai", Phase: "loading", Stage: "preview", CandidateName: "李四",
		Reason: "正在分析下一位候选人",
	})
	status := logger.AnalysisStatus("task-analysis")
	if status == nil || status.CandidateName != "张三" || status.Reason != "经验暂时不匹配" {
		t.Fatalf("最终判断被下一位候选人提前覆盖：%+v", status)
	}
	logger.analysisMu.Lock()
	logger.analysisVisibleAt = time.Now().Add(-analysisResultHoldDuration)
	logger.analysisMu.Unlock()
	status = logger.AnalysisStatus("task-analysis")
	if status == nil || status.CandidateName != "李四" {
		t.Fatalf("保留时间结束后没有展示最新状态：%+v", status)
	}
}
