// Package shared 定义启动前检查、任务生命周期和各主流程共享的强类型数据。
package shared

import (
	"log"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// StartRequest 表示本地任务统一启动参数。
type StartRequest struct {
	TaskID     string `json:"task_id"`
	PositionID string `json:"position_id"`
	TaskType   string `json:"task_type"`
	Token      string `json:"token"`
	ProfileID  string `json:"profile_id,omitempty"`
	Headless   bool   `json:"headless"`
}

// PreparedTask 表示启动前检查生成且后续流程直接复用的快照。
type PreparedTask struct {
	Request      StartRequest
	Session      cloud.UserSession
	Subscription cloud.Subscription
	Position     cloud.PositionSnapshot
	Preferences  cloud.UserPreferences
	Platform     model.Config
	ProfilePath  string
}

// Stats 表示主流程不含敏感内容的运行统计。
type Stats struct {
	Processed int `json:"processed"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

// AnalysisStatus 表示悬浮窗展示的 AI 或关键词判断状态。
type AnalysisStatus struct {
	Kind            string   `json:"kind"`
	Phase           string   `json:"phase"`
	Stage           string   `json:"stage,omitempty"`
	Terminal        bool     `json:"terminal"`
	CandidateName   string   `json:"candidate_name"`
	Score           *float64 `json:"score,omitempty"`
	Threshold       *float64 `json:"threshold,omitempty"`
	Accepted        *bool    `json:"accepted,omitempty"`
	Reason          string   `json:"reason"`
	Keywords        []string `json:"keywords,omitempty"`
	MatchedKeywords []string `json:"matched_keywords,omitempty"`
	ExcludeKeywords []string `json:"exclude_keywords,omitempty"`
	MatchedExcludes []string `json:"matched_excludes,omitempty"`
	UpdatedAt       string   `json:"updated_at"`
}

// Logger 定义流程步骤的统一结构化日志能力。
type Logger interface {
	Step(taskID string, flow string, step string, status string, startedAt time.Time, err error)
}

// ProgressLogger 定义可向悬浮窗和用户日志写入实时中文进度的日志能力。
type ProgressLogger interface {
	Progress(taskID string, message string)
}

// AnalysisLogger 定义悬浮窗结构化分析状态的内存读写能力。
type AnalysisLogger interface {
	ReportAnalysis(taskID string, status AnalysisStatus)
	AnalysisStatus(taskID string) *AnalysisStatus
	ResetAnalysis(taskID string)
}

// ReportProgress 在当前日志器支持实时进度时更新任务状态，不影响普通日志器。
func ReportProgress(logger Logger, taskID string, message string) {
	if progress, ok := logger.(ProgressLogger); ok {
		progress.Progress(taskID, message)
	}
}

// ReportAnalysis 在当前日志器支持时更新结构化分析状态。
func ReportAnalysis(logger Logger, taskID string, status AnalysisStatus) {
	if analysis, ok := logger.(AnalysisLogger); ok {
		analysis.ReportAnalysis(taskID, status)
	}
}

// ReadAnalysis 返回当前任务最近一次结构化分析状态。
func ReadAnalysis(logger Logger, taskID string) *AnalysisStatus {
	if analysis, ok := logger.(AnalysisLogger); ok {
		return analysis.AnalysisStatus(taskID)
	}
	return nil
}

// ResetAnalysis 清空旧任务分析状态并准备接收当前任务结果。
func ResetAnalysis(logger Logger, taskID string) {
	if analysis, ok := logger.(AnalysisLogger); ok {
		analysis.ResetAnalysis(taskID)
	}
}

// StandardLogger 使用 Go 标准日志输出步骤状态。
type StandardLogger struct{}

// Step 输出任务、流程、步骤、状态和耗时。
func (StandardLogger) Step(taskID string, flow string, step string, status string, startedAt time.Time, err error) {
	errorText := ""
	if err != nil {
		errorText = err.Error()
	}
	log.Printf("task_id=%s flow=%s step=%s status=%s duration_ms=%d error=%q", taskID, flow, step, status, time.Since(startedAt).Milliseconds(), errorText)
}
