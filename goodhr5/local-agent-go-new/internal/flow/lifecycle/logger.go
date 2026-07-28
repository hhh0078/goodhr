// Package lifecycle 把流程步骤日志同步到标准日志和 SQLite 当前任务状态。
package lifecycle

import (
	"context"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TaskLogger 同时输出日志并更新任务当前步骤。
type TaskLogger struct {
	store *storage.Store
	next  shared.Logger
}

// NewTaskLogger 创建任务步骤日志器。
func NewTaskLogger(store *storage.Store, next shared.Logger) *TaskLogger {
	return &TaskLogger{store: store, next: next}
}

// Step 输出步骤日志，并在步骤开始时更新 SQLite 当前状态。
func (l *TaskLogger) Step(taskID string, flow string, step string, status string, startedAt time.Time, err error) {
	if l.next != nil {
		l.next.Step(taskID, flow, step, status, startedAt, err)
	}
	if l.store != nil && status == "start" {
		_ = l.store.UpdateTaskStep(context.Background(), taskID, flow+"."+step)
	}
}
