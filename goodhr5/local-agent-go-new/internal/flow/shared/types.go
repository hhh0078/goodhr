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

// Logger 定义流程步骤的统一结构化日志能力。
type Logger interface {
	Step(taskID string, flow string, step string, status string, startedAt time.Time, err error)
}

// ProgressLogger 定义可向悬浮窗和用户日志写入实时中文进度的日志能力。
type ProgressLogger interface {
	Progress(taskID string, message string)
}

// ReportProgress 在当前日志器支持实时进度时更新任务状态，不影响普通日志器。
func ReportProgress(logger Logger, taskID string, message string) {
	if progress, ok := logger.(ProgressLogger); ok {
		progress.Progress(taskID, message)
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
