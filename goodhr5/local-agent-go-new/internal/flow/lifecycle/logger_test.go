// Package lifecycle 文件作用：验证用户可见任务日志统一使用中文且不会泄露 Worker 内部步骤。
package lifecycle

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestTaskLogMessageUsesChinese 验证流程错误日志使用中文步骤名称。
func TestTaskLogMessageUsesChinese(t *testing.T) {
	message := taskLogMessage("select_position", "failed", errors.New("没有找到岗位"))
	if message != "选择岗位：没处理成功，没有找到岗位" {
		t.Fatalf("中文流程日志不正确：%s", message)
	}
}

// TestTaskLoggerDoesNotRepeatSameAnalysisLogOnStatsRefresh 验证轮询只刷新数量时不会重复写同一条 AI 终态日志。
func TestTaskLoggerDoesNotRepeatSameAnalysisLogOnStatsRefresh(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer store.Close()
	logger := NewTaskLogger(store, nil)
	status := shared.AnalysisStatus{
		Kind: "auto_reply", Phase: "error", Stage: "resume_ai", Terminal: true,
		CandidateName: "邓云川", Reason: "AI 简历结构化格式不正确",
	}
	logger.ReportAnalysis("task-analysis-repeat", status)
	status.ProcessedCount = 1
	logger.ReportAnalysis("task-analysis-repeat", status)
	logs, err := store.ListTaskLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("读取测试日志失败：%v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("同一 AI 终态被重复写入日志：count=%d logs=%+v", len(logs), logs)
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

// TestTaskLoggerKeepsAutoReplyTimeline 验证同一候选人的自动回复步骤会累积且连续重复不会刷屏。
func TestTaskLoggerKeepsAutoReplyTimeline(t *testing.T) {
	logger := NewTaskLogger(nil, nil)
	for _, status := range []shared.AnalysisStatus{
		{Kind: "auto_reply", Phase: "loading", Stage: "sync", CandidateName: "陈女士", Reason: "正在同步聊天"},
		{Kind: "auto_reply", Phase: "loading", Stage: "tool", CandidateName: "陈女士", Reason: "AI 调用工具：查看简历"},
		{Kind: "auto_reply", Phase: "loading", Stage: "tool", CandidateName: "陈女士", Reason: "AI 调用工具：查看简历"},
		{Kind: "auto_reply", Phase: "result", Stage: "reply", Terminal: true, CandidateName: "陈女士", Reason: "AI 准备回复：您好"},
	} {
		logger.ReportAnalysis("task-auto-reply", status)
	}
	result := logger.AnalysisStatus("task-auto-reply")
	if result == nil || len(result.Timeline) != 3 {
		t.Fatalf("analysis = %+v", result)
	}
	if result.Timeline[0].Stage != "sync" || result.Timeline[2].Stage != "reply" {
		t.Fatalf("timeline = %+v", result.Timeline)
	}
	logger.ReportAnalysis("task-auto-reply", shared.AnalysisStatus{
		Kind: "auto_reply", Phase: "loading", Stage: "sync", CandidateName: "陈女士",
		Reason: "开始下一轮", ProcessedCount: 3, SucceededCount: 1, SkippedCount: 2,
	})
	logger.ReportAnalysis("task-auto-reply", shared.AnalysisStatus{
		Kind: "auto_reply", Phase: "loading", Stage: "ai", CandidateName: "陈女士", Reason: "AI 正在分析",
	})
	result = logger.AnalysisStatus("task-auto-reply")
	if result == nil || result.ProcessedCount != 3 || result.SucceededCount != 1 || result.SkippedCount != 2 {
		t.Fatalf("自动回复累计统计被后续状态清空：%+v", result)
	}
}
