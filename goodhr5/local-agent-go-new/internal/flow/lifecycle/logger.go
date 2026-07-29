// Package lifecycle 把流程步骤日志同步到标准日志和 SQLite 当前任务状态。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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
		if updateErr := l.store.UpdateTaskStep(context.Background(), taskID, flow+"."+step); updateErr != nil {
			log.Printf("task_id=%s flow=lifecycle step=update_task_step status=warning error=%q", taskID, updateErr)
		}
	}
	if l.store != nil {
		message := flow + "." + step + " " + status
		if err != nil {
			message = fmt.Sprintf("%s：%v", message, err)
		}
		if _, saveErr := l.store.SaveTaskLog(context.Background(), storage.TaskLog{
			TaskID: taskID, Flow: flow, Step: step, Status: status,
			Message: message, DurationMS: time.Since(startedAt).Milliseconds(),
		}); saveErr != nil {
			log.Printf("task_id=%s flow=lifecycle step=save_task_log status=warning error=%q", taskID, saveErr)
		}
	}
}

// workerLogLine 表示 Worker 输出到 stdout 的统一 JSON 行字段。
type workerLogLine struct {
	Timestamp         string `json:"timestamp"`
	Level             string `json:"level"`
	TraceID           string `json:"trace_id"`
	Action            string `json:"action"`
	Step              string `json:"step"`
	Status            string `json:"status"`
	DurationMS        int64  `json:"duration_ms"`
	PageURL           string `json:"page_url"`
	TargetDescription string `json:"target_description"`
	ErrorCode         string `json:"error_code"`
}

// WorkerLine 解析 Worker JSON 行，并按 trace_id 汇入对应任务的岗位日志。
func (l *TaskLogger) WorkerLine(line []byte) {
	if l == nil || l.store == nil {
		return
	}
	var item workerLogLine
	if err := json.Unmarshal(line, &item); err != nil {
		log.Printf("flow=browser_worker step=parse_log status=warning error=%q", err)
		return
	}
	item.TraceID = strings.TrimSpace(item.TraceID)
	if item.TraceID == "" {
		return
	}
	exists, err := l.store.TaskExists(context.Background(), item.TraceID)
	if err != nil {
		log.Printf("task_id=%s flow=browser_worker step=check_task status=warning error=%q", item.TraceID, err)
		return
	}
	if !exists {
		return
	}
	message := strings.TrimSpace("worker." + item.Action + "." + item.Step + " " + item.Status)
	if item.ErrorCode != "" {
		message += "：" + item.ErrorCode
	}
	if _, err := l.store.SaveTaskLog(context.Background(), storage.TaskLog{
		TaskID: item.TraceID, Flow: "browser_worker", Step: item.Step,
		Status: item.Status, Level: item.Level, Message: message,
		DurationMS: item.DurationMS, CreatedAt: item.Timestamp,
	}); err != nil {
		log.Printf("task_id=%s flow=browser_worker step=save_log status=warning error=%q", item.TraceID, err)
	}
}
